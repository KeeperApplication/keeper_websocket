package presence

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"keeper.websocket.go/internal/config"
	"keeper.websocket.go/internal/shared"
)

const onlineUsersKey = "presence:online_users"
const presenceChannel = "presence:events"
const disconnectQueueKey = "presence:disconnect_queue"
const gracePeriod = 5 * time.Second

type Service struct {
	redisClient   *redis.Client
	logger        *slog.Logger
	broadcastChan chan<- *shared.InternalBroadcast
}

func NewService(cfg *config.Config, logger *slog.Logger, broadcastChan chan<- *shared.InternalBroadcast) (*Service, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opts)
	return &Service{
		redisClient:   rdb,
		logger:        logger,
		broadcastChan: broadcastChan,
	}, nil
}

func (s *Service) AddUser(ctx context.Context, username string) error {
	s.logger.Info("user came online, adding to set and canceling any pending disconnect", "user", username)
	s.redisClient.ZRem(ctx, disconnectQueueKey, username)

	added, err := s.redisClient.SAdd(ctx, onlineUsersKey, username).Result()
	if err != nil {
		return err
	}

	if added > 0 {
		return s.publishPresenceUpdate(ctx)
	}

	return nil
}

func (s *Service) RemoveUser(ctx context.Context, username string) error {
	s.logger.Info("user connection lost, scheduling removal", "user", username)
	score := float64(time.Now().Add(gracePeriod).Unix())
	return s.redisClient.ZAdd(ctx, disconnectQueueKey, redis.Z{
		Score:  score,
		Member: username,
	}).Err()
}

func (s *Service) StartDisconnectWorker(ctx context.Context) {
	s.logger.Info("starting presence disconnect worker")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping presence disconnect worker")
			return
		case <-ticker.C:
			s.processDisconnectQueue(ctx)
		}
	}
}

func (s *Service) processDisconnectQueue(ctx context.Context) {
	now := float64(time.Now().Unix())
	usersToRemove, err := s.redisClient.ZRangeByScore(ctx, disconnectQueueKey, &redis.ZRangeBy{
		Min: "-inf",
		Max: strconv.FormatFloat(now, 'f', -1, 64),
	}).Result()

	if err != nil {
		s.logger.Error("failed to process disconnect queue", "error", err)
		return
	}

	if len(usersToRemove) > 0 {
		s.logger.Info("found users to mark as offline", "users", usersToRemove)

		pipe := s.redisClient.Pipeline()
		pipe.SRem(ctx, onlineUsersKey, usersToRemove)
		pipe.ZRem(ctx, disconnectQueueKey, usersToRemove)
		_, err := pipe.Exec(ctx)
		if err != nil {
			s.logger.Error("failed to finalize user removal from redis", "error", err)
			return
		}

		s.publishPresenceUpdate(ctx)
	}
}

func (s *Service) publishPresenceUpdate(ctx context.Context) error {
	return s.redisClient.Publish(ctx, presenceChannel, "update").Err()
}

func (s *Service) ListenForUpdates(ctx context.Context) {
	pubsub := s.redisClient.Subscribe(ctx, presenceChannel)
	defer pubsub.Close()
	s.logger.Info("presence service listening for updates on redis channel")
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-pubsub.Channel():
			if !ok {
				return
			}
			if msg.Payload == "update" {
				s.logger.Info("received presence update event, broadcasting to local clients")
				s.BroadcastPresenceUpdate(ctx)
			}
		}
	}
}

func (s *Service) BroadcastPresenceUpdate(ctx context.Context) {
	users, err := s.redisClient.SMembers(ctx, onlineUsersKey).Result()
	if err != nil {
		s.logger.Error("failed to get online users from redis for broadcast", "error", err)
		return
	}
	payload := map[string]interface{}{"users": users}
	broadcastMsg := shared.BroadcastMessage{
		Topic:   shared.PresenceTopic,
		Event:   "presence_update",
		Payload: payload,
	}
	jsonMsg, err := json.Marshal(broadcastMsg)
	if err != nil {
		s.logger.Error("failed to marshal presence update for hub", "error", err)
		return
	}
	select {
	case s.broadcastChan <- &shared.InternalBroadcast{Message: jsonMsg}:
	case <-time.After(1 * time.Second):
		s.logger.Warn("timed out sending presence update to hub channel")
	}
}
