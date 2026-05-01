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

type Dispatcher struct {
	logger       *zap.Logger
	emailHandler EmailHandler
}

// NewDispatcher accepts the handler
func NewDispatcher(logger *zap.Logger, emailHandler EmailHandler) *Dispatcher {
	return &Dispatcher{logger: logger, emailHandler: emailHandler}
}

func (d *Dispatcher) Dispatch(delivery amqp091.Delivery) {
	eventId, ok := delivery.Headers["eventId"].(string)
	if !ok {
		eventId = "unknown"
	}

	d.logger.Info("Received event", zap.String("eventId", eventId), zap.String("routingKey", delivery.RoutingKey))

	ctx := context.Background() 
	rawPayload := string(delivery.Body)

	switch delivery.RoutingKey {
	case "pet.report.created":
		if d.emailHandler != nil {
			var evt PetReportCreatedEvent
			if err := json.Unmarshal(delivery.Body, &evt); err == nil {
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
			if err := json.Unmarshal(delivery.Body, &evt); err == nil {
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

	case "match.found", "chat.message.sent":
		d.logger.Info("handler not implemented yet", zap.String("routingKey", delivery.RoutingKey))
		delivery.Ack(false)

	default:
		d.logger.Warn("Unknown routing key", zap.String("routingKey", delivery.RoutingKey))
		delivery.Ack(false)
	}
}
