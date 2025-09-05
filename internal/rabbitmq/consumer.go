package rabbitmq

import (
	"context"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"keeper.websocket.go/internal/config"
	"keeper.websocket.go/internal/websocket"
)

type Consumer struct {
	logger    *slog.Logger
	cfg       *config.Config
	broadcast chan<- *websocket.InternalBroadcast
}

func NewConsumer(logger *slog.Logger, cfg *config.Config, broadcast chan<- *websocket.InternalBroadcast) *Consumer {
	return &Consumer{
		logger:    logger,
		cfg:       cfg,
		broadcast: broadcast,
	}
}

func (c *Consumer) Start(ctx context.Context) {
	c.logger.Info("starting rabbitmq consumer")
	go c.listenForMessages(ctx)
}

func (c *Consumer) listenForMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("context cancelled, stopping consumer")
			return
		default:
			conn, err := amqp.Dial(c.cfg.RabbitMQURL)
			if err != nil {
				c.logger.Error("failed to connect to rabbitmq, retrying in 5s", "error", err)
				time.Sleep(5 * time.Second)
				continue
			}
			defer conn.Close()

			ch, err := conn.Channel()
			if err != nil {
				c.logger.Error("failed to open a channel, retrying", "error", err)
				continue
			}
			defer ch.Close()

			err = ch.ExchangeDeclare(
				"keeper.exchange",
				"topic",
				true,
				false,
				false,
				false,
				nil,
			)
			if err != nil {
				c.logger.Error("failed to declare an exchange", "error", err)
				continue
			}

			q, err := ch.QueueDeclare(
				"",
				false,
				true,
				true,
				false,
				nil,
			)
			if err != nil {
				c.logger.Error("failed to declare a queue", "error", err)
				continue
			}

			err = ch.QueueBind(
				q.Name,
				"#",
				"keeper.exchange",
				false,
				nil)
			if err != nil {
				c.logger.Error("failed to bind a queue", "error", err)
				continue
			}

			msgs, err := ch.Consume(
				q.Name,
				"",
				true,
				false,
				false,
				false,
				nil,
			)
			if err != nil {
				c.logger.Error("failed to register a consumer", "error", err)
				continue
			}

			c.logger.Info("rabbitmq consumer connected and waiting for messages")

			for d := range msgs {
				c.broadcast <- &websocket.InternalBroadcast{
					Message: d.Body,
					Origin:  nil,
				}
			}
		}
	}
}
