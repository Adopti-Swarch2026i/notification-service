package messaging

import (
	"context"
	"encoding/json"

	"github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

type EmailHandler interface {
	HandlePetReportCreated(ctx context.Context, eventID string, evt PetReportCreatedEvent, rawPayload string) error
	HandlePetReportReunited(ctx context.Context, eventID string, evt PetReportReunitedEvent, rawPayload string) error
}

type PushHandler interface {
	HandleChatMessageSent(ctx context.Context, eventID string, evt ChatMessageSentEvent, rawPayload string) error
}

type Dispatcher struct {
	logger       *zap.Logger
	emailHandler EmailHandler
	pushHandler  PushHandler
}

// NewDispatcher accepts the handlers
func NewDispatcher(logger *zap.Logger, emailHandler EmailHandler, pushHandler PushHandler) *Dispatcher {
	return &Dispatcher{logger: logger, emailHandler: emailHandler, pushHandler: pushHandler}
}

func (d *Dispatcher) Dispatch(delivery amqp091.Delivery) {
	eventId, ok := delivery.Headers["eventId"].(string)
	if !ok {
		eventId = "unknown"
	}

	var envelope struct {
		EventID string          `json:"eventId"`
		Data    json.RawMessage `json:"data"`
	}
	var innerPayload []byte

	if err := json.Unmarshal(delivery.Body, &envelope); err == nil && len(envelope.Data) > 0 {
		if envelope.EventID != "" {
			eventId = envelope.EventID
		}
		innerPayload = envelope.Data
	} else {
		innerPayload = delivery.Body
	}

	d.logger.Info("Received event", zap.String("eventId", eventId), zap.String("routingKey", delivery.RoutingKey))

	ctx := context.Background() 
	rawPayload := string(delivery.Body)

	switch delivery.RoutingKey {
	case "pet.report.created":
		if d.emailHandler != nil {
			var evt PetReportCreatedEvent
			if err := json.Unmarshal(innerPayload, &evt); err == nil {
				err = d.emailHandler.HandlePetReportCreated(ctx, eventId, evt, rawPayload)
				if err != nil {
					d.logger.Error("Failed to handle pet.report.created email", zap.Error(err))
					delivery.Nack(false, false) 
					return
				}
			} else {
				d.logger.Error("Failed to parse pet.report.created", zap.Error(err))
				delivery.Nack(false, false)
				return
			}
		}
		delivery.Ack(false)

	case "pet.report.reunited":
		if d.emailHandler != nil {
			var evt PetReportReunitedEvent
			if err := json.Unmarshal(innerPayload, &evt); err == nil {
				err = d.emailHandler.HandlePetReportReunited(ctx, eventId, evt, rawPayload)
				if err != nil {
					d.logger.Error("Failed to handle pet.report.reunited email", zap.Error(err))
					delivery.Nack(false, false)
					return
				}
			} else {
				d.logger.Error("Failed to parse pet.report.reunited", zap.Error(err))
				delivery.Nack(false, false)
				return
			}
		}
		delivery.Ack(false)

	case "chat.message.sent":
		if d.pushHandler != nil {
			var evt ChatMessageSentEvent
			if err := json.Unmarshal(innerPayload, &evt); err == nil {
				err = d.pushHandler.HandleChatMessageSent(ctx, eventId, evt, rawPayload)
				if err != nil {
					d.logger.Error("Failed to handle chat.message.sent push", zap.Error(err))
					delivery.Nack(false, false)
					return
				}
			} else {
				d.logger.Error("Failed to parse chat.message.sent", zap.Error(err))
				delivery.Nack(false, false)
				return
			}
		}
		delivery.Ack(false)

	case "match.found":
		d.logger.Info("handler not implemented yet", zap.String("routingKey", delivery.RoutingKey))
		delivery.Ack(false)

	default:
		d.logger.Warn("Unknown routing key", zap.String("routingKey", delivery.RoutingKey))
		delivery.Ack(false)
	}
}
