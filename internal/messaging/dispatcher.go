package messaging

import (
	"github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

type Dispatcher struct {
	logger *zap.Logger
}

func NewDispatcher(logger *zap.Logger) *Dispatcher {
	return &Dispatcher{logger: logger}
}

func (d *Dispatcher) Dispatch(delivery amqp091.Delivery) {
	eventId, ok := delivery.Headers["eventId"].(string)
	if !ok {
		eventId = "unknown"
	}

	d.logger.Info("Received event", zap.String("eventId", eventId), zap.String("routingKey", delivery.RoutingKey))

	switch delivery.RoutingKey {
	case "pet.report.created", "pet.report.reunited", "match.found", "chat.message.sent":
		d.logger.Info("handler not implemented yet", zap.String("routingKey", delivery.RoutingKey))
	default:
		d.logger.Warn("Unknown routing key", zap.String("routingKey", delivery.RoutingKey))
	}

	delivery.Ack(false)
}
