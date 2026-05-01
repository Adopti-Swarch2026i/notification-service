package messaging

import (
	"context"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

type Consumer struct {
	url    string
	logger *zap.Logger
	conn   *amqp091.Connection
	ch     *amqp091.Channel
	done   chan struct{}
}

func NewConsumer(url string, logger *zap.Logger) (*Consumer, error) {
	c := &Consumer{
		url:    url,
		logger: logger,
		done:   make(chan struct{}),
	}
	err := c.connect()
	return c, err
}

func (c *Consumer) connect() error {
	c.Close()

	conn, err := amqp091.Dial(c.url)
	if err != nil {
		return err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}

	err = ch.Qos(5, 0, false)
	if err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	err = ch.ExchangeDeclare("adopti.events.dlx", "topic", true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	err = ch.ExchangeDeclare("adopti.events", "topic", true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	args := amqp091.Table{
		"x-dead-letter-exchange": "adopti.events.dlx",
	}

	_, err = ch.QueueDeclare("notifications.queue", true, false, false, false, args)
	if err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	for _, key := range []string{"pet.report.*", "chat.message.sent", "match.found"} {
		err = ch.QueueBind("notifications.queue", key, "adopti.events", false, nil)
		if err != nil {
			ch.Close()
			conn.Close()
			return err
		}
	}

	c.conn = conn
	c.ch = ch
	return nil
}

func (c *Consumer) Close() {
	if c.ch != nil && !c.ch.IsClosed() {
		c.ch.Close()
		c.ch = nil
	}
	if c.conn != nil && !c.conn.IsClosed() {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Consumer) Start(ctx context.Context, dispatch func(amqp091.Delivery)) error {
	backoff := 1 * time.Second

	for {
		select {
		case <-ctx.Done():
			c.Close()
			return ctx.Err()
		case <-c.done:
			return nil
		default:
		}

		if c.conn == nil || c.conn.IsClosed() {
			if err := c.connect(); err != nil {
				c.logger.Error("Failed to connect to RabbitMQ, retrying...", zap.Error(err), zap.Duration("backoff", backoff))
				select {
				case <-time.After(backoff):
					backoff *= 2
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			backoff = 1 * time.Second
		}

		notifyClose := c.conn.NotifyClose(make(chan *amqp091.Error, 1))

		msgs, err := c.ch.Consume(
			"notifications.queue",
			"",
			false,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			c.logger.Error("Failed to consume, retrying...", zap.Error(err))
			c.Close()
			time.Sleep(backoff)
			continue
		}

		c.logger.Info("Successfully connected and consuming messages from RabbitMQ")

	consumeLoop:
		for {
			select {
			case <-ctx.Done():
				c.Close()
				return ctx.Err()
			case err := <-notifyClose:
				c.logger.Warn("RabbitMQ connection closed", zap.Error(err))
				c.Close()
				break consumeLoop
			case d, ok := <-msgs:
				if !ok {
					c.logger.Warn("RabbitMQ message channel closed")
					c.Close()
					break consumeLoop
				}
				go dispatch(d)
			}
		}
	}
}
