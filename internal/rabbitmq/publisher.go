package rabbitmq

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"keeper.websocket.go/internal/config"
)

type Publisher struct {
	conn    *amqp.Connection
	ch      *amqp.Channel
	cfg     *config.Config
	logger  *slog.Logger
	connErr chan *amqp.Error
}

func NewPublisher(cfg *config.Config, logger *slog.Logger) (*Publisher, error) {
	p := &Publisher{
		cfg:    cfg,
		logger: logger,
	}

	if err := p.connect(); err != nil {
		return nil, err
	}

	go p.handleReconnect()

	return p, nil
}

func (p *Publisher) connect() error {
	conn, err := amqp.Dial(p.cfg.RabbitMQURL)
	if err != nil {
		return err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}

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
		ch.Close()
		conn.Close()
		return err
	}

	p.conn = conn
	p.ch = ch
	p.connErr = make(chan *amqp.Error)
	p.conn.NotifyClose(p.connErr)
	p.logger.Info("rabbitmq publisher connected")
	return nil
}

func (p *Publisher) handleReconnect() {
	for {
		err := <-p.connErr
		if err != nil {
			p.logger.Error("rabbitmq connection lost, attempting to reconnect...", "error", err)
			for {
				time.Sleep(5 * time.Second)
				if connErr := p.connect(); connErr == nil {
					p.logger.Info("rabbitmq publisher reconnected successfully")
					break
				}
				p.logger.Warn("rabbitmq reconnection failed, retrying...")
			}
		}
	}
}

func (p *Publisher) Publish(ctx context.Context, routingKey string, body interface{}) error {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	return p.ch.PublishWithContext(ctx,
		"keeper.exchange",
		routingKey,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        jsonBody,
		},
	)
}

func (p *Publisher) Close() {
	if p.ch != nil {
		p.ch.Close()
	}
	if p.conn != nil {
		p.conn.Close()
	}
}	