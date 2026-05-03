package messaging

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

type EmailHandler interface {
	HandlePetReportCreated(ctx context.Context, eventID string, evt PetReportCreatedEvent, rawPayload string) error
	HandlePetReportReunited(ctx context.Context, eventID string, evt PetReportReunitedEvent, rawPayload string) error
	HandleMatchFound(ctx context.Context, eventID string, evt MatchFoundEvent, rawPayload string) error
}

type PushHandler interface {
	HandleChatMessageSent(ctx context.Context, eventID string, evt ChatMessageSentEvent, rawPayload string) error
	HandlePetReportReunited(ctx context.Context, eventID string, evt PetReportReunitedEvent, rawPayload string) error
	HandleMatchFound(ctx context.Context, eventID string, evt MatchFoundEvent, rawPayload string) error
}

type Dispatcher struct {
	logger       *zap.Logger
	emailHandler EmailHandler
	pushHandler  PushHandler
}

// events.md §5.2: 3 intentos, base 2s, max 30s. Implementado abajo en
// withRetry para envolver cada handler antes del NACK definitivo.
const (
	retryAttempts = 3
	retryBase     = 2 * time.Second
	retryMax      = 30 * time.Second
)

// NewDispatcher accepts the handlers
func NewDispatcher(logger *zap.Logger, emailHandler EmailHandler, pushHandler PushHandler) *Dispatcher {
	return &Dispatcher{logger: logger, emailHandler: emailHandler, pushHandler: pushHandler}
}

// withRetry ejecuta fn con back-off exponencial. Devuelve nil al primer
// éxito o el último error tras agotar los intentos. La idempotencia del
// handler (events.md §2.1) garantiza que repetir es seguro.
func (d *Dispatcher) withRetry(ctx context.Context, name string, fn func() error) error {
	var lastErr error
	delay := retryBase
	for attempt := 1; attempt <= retryAttempts; attempt++ {
		if err := fn(); err == nil {
			if attempt > 1 {
				d.logger.Info("handler succeeded after retry",
					zap.String("handler", name),
					zap.Int("attempt", attempt))
			}
			return nil
		} else {
			lastErr = err
			if attempt == retryAttempts {
				break
			}
			d.logger.Warn("handler failed, will retry",
				zap.String("handler", name),
				zap.Int("attempt", attempt),
				zap.Duration("nextDelay", delay),
				zap.Error(err))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			delay *= 2
			if delay > retryMax {
				delay = retryMax
			}
		}
	}
	return lastErr
}

func (d *Dispatcher) Dispatch(delivery amqp091.Delivery) {
	// events.md §7.2: el body == data del schema, sin envelope. Los metadatos
	// vienen en headers AMQP. messageId del frame es fallback si el header
	// llegara vacío.
	eventId, ok := delivery.Headers["eventId"].(string)
	if !ok || eventId == "" {
		if delivery.MessageId != "" {
			eventId = delivery.MessageId
		} else {
			eventId = "unknown"
		}
	}

	innerPayload := delivery.Body
	d.logger.Info("Received event",
		zap.String("eventId", eventId),
		zap.String("routingKey", delivery.RoutingKey))

	ctx := context.Background()
	rawPayload := string(delivery.Body)

	switch delivery.RoutingKey {
	case "pet.report.created":
		var evt PetReportCreatedEvent
		if err := json.Unmarshal(innerPayload, &evt); err != nil {
			d.logger.Error("Failed to parse pet.report.created", zap.Error(err))
			delivery.Nack(false, false)
			return
		}
		if d.emailHandler != nil {
			if err := d.withRetry(ctx, "email.pet.report.created", func() error {
				return d.emailHandler.HandlePetReportCreated(ctx, eventId, evt, rawPayload)
			}); err != nil {
				d.logger.Error("Giving up after retries; routing to DLX",
					zap.String("handler", "email.pet.report.created"),
					zap.String("eventId", eventId),
					zap.Error(err))
				delivery.Nack(false, false)
				return
			}
		}
		delivery.Ack(false)

	case "pet.report.reunited":
		var evt PetReportReunitedEvent
		if err := json.Unmarshal(innerPayload, &evt); err != nil {
			d.logger.Error("Failed to parse pet.report.reunited", zap.Error(err))
			delivery.Nack(false, false)
			return
		}
		if d.emailHandler != nil {
			if err := d.withRetry(ctx, "email.pet.report.reunited", func() error {
				return d.emailHandler.HandlePetReportReunited(ctx, eventId, evt, rawPayload)
			}); err != nil {
				d.logger.Error("Giving up after retries; routing to DLX",
					zap.String("handler", "email.pet.report.reunited"),
					zap.String("eventId", eventId),
					zap.Error(err))
				delivery.Nack(false, false)
				return
			}
		}
		if d.pushHandler != nil {
			if err := d.withRetry(ctx, "push.pet.report.reunited", func() error {
				return d.pushHandler.HandlePetReportReunited(ctx, eventId, evt, rawPayload)
			}); err != nil {
				d.logger.Error("Giving up after retries; routing to DLX",
					zap.String("handler", "push.pet.report.reunited"),
					zap.String("eventId", eventId),
					zap.Error(err))
				delivery.Nack(false, false)
				return
			}
		}
		delivery.Ack(false)

	case "chat.message.sent":
		var evt ChatMessageSentEvent
		if err := json.Unmarshal(innerPayload, &evt); err != nil {
			d.logger.Error("Failed to parse chat.message.sent", zap.Error(err))
			delivery.Nack(false, false)
			return
		}
		if d.pushHandler != nil {
			if err := d.withRetry(ctx, "push.chat.message.sent", func() error {
				return d.pushHandler.HandleChatMessageSent(ctx, eventId, evt, rawPayload)
			}); err != nil {
				d.logger.Error("Giving up after retries; routing to DLX",
					zap.String("handler", "push.chat.message.sent"),
					zap.String("eventId", eventId),
					zap.Error(err))
				delivery.Nack(false, false)
				return
			}
		}
		delivery.Ack(false)

	case "match.found":
		var evt MatchFoundEvent
		if err := json.Unmarshal(innerPayload, &evt); err != nil {
			d.logger.Error("Failed to parse match.found", zap.Error(err))
			delivery.Nack(false, false)
			return
		}
		if d.emailHandler != nil {
			if err := d.withRetry(ctx, "email.match.found", func() error {
				return d.emailHandler.HandleMatchFound(ctx, eventId, evt, rawPayload)
			}); err != nil {
				d.logger.Error("Giving up after retries; routing to DLX",
					zap.String("handler", "email.match.found"),
					zap.String("eventId", eventId),
					zap.Error(err))
				delivery.Nack(false, false)
				return
			}
		}
		if d.pushHandler != nil {
			if err := d.withRetry(ctx, "push.match.found", func() error {
				return d.pushHandler.HandleMatchFound(ctx, eventId, evt, rawPayload)
			}); err != nil {
				d.logger.Error("Giving up after retries; routing to DLX",
					zap.String("handler", "push.match.found"),
					zap.String("eventId", eventId),
					zap.Error(err))
				delivery.Nack(false, false)
				return
			}
		}
		delivery.Ack(false)

	default:
		d.logger.Warn("Unknown routing key", zap.String("routingKey", delivery.RoutingKey))
		delivery.Ack(false)
	}
}
