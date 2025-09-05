package broker

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"keeper.websocket.go/internal/config"
	"keeper.websocket.go/internal/shared"
)

const eventsChannel = "keeper-events"

type Subscriber struct {
	redisClient *redis.Client
	logger      *slog.Logger
	broadcast   chan<- *shared.InternalBroadcast
}

func NewSubscriber(cfg *config.Config, logger *slog.Logger, broadcastChan chan<- *shared.InternalBroadcast) (*Subscriber, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opts)
	return &Subscriber{
		redisClient: rdb,
		logger:      logger,
		broadcast:   broadcastChan,
	}, nil
}

func (s *Subscriber) ListenForEvents(ctx context.Context) {
	pubsub := s.redisClient.Subscribe(ctx, eventsChannel)
	defer pubsub.Close()
	s.logger.Info("Broker subscriber listening for events", "channel", eventsChannel)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Stopping broker event listener")
			return
		case msg, ok := <-pubsub.Channel():
			if !ok {
				s.logger.Warn("Broker pub/sub channel closed")
				return
			}

			s.logger.Info("Received message from broker, forwarding to hub", "channel", msg.Channel)
			s.broadcast <- &shared.InternalBroadcast{
				Message: []byte(msg.Payload),
				Origin:  nil,
			}
		}
	}
}
