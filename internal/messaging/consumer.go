package messaging

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/adopti/notification-service/internal/config"
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

	var conn *amqp091.Connection
	var err error
	if strings.HasPrefix(c.url, "amqps://") {
		var caPEM []byte
		caPEM, err = os.ReadFile("/app/certs/ca.crt")
		if err != nil {
			return err
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caPEM) {
			return fmt.Errorf("failed to append CA certificate to pool")
		}
		conn, err = amqp091.DialTLS(c.url, &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    caPool,
		})
	} else {
		conn, err = amqp091.Dial(c.url)
	}
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

	// events.md §1: x-message-ttl=86400000 (24h) y DLX obligatorios.
	args := amqp091.Table{
		"x-dead-letter-exchange": "adopti.events.dlx",
		"x-message-ttl":          int32(86_400_000),
	}

	_, err = ch.QueueDeclare("notifications.queue", true, false, false, false, args)
	if err != nil {
		ch.Close()
		conn.Close()
		return err
	}

	// events.md §1 lista las routing keys exactas que esta queue debe recibir.
	// `pet.report.updated` NO está aquí — solo lo consume matching-service.
	for _, key := range []string{
		"pet.report.created",
		"pet.report.reunited",
		"chat.message.sent",
		"match.found",
	} {
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
				c.logger.Error("Failed to connect to RabbitMQ, retrying...", zap.String("error", config.SanitizeError(err)), zap.Duration("backoff", backoff))
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
			c.logger.Error("Failed to consume, retrying...", zap.String("error", config.SanitizeError(err)))
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
				c.logger.Warn("RabbitMQ connection closed", zap.String("error", config.SanitizeError(err)))
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
