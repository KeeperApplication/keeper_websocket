package auth

import (
	"log/slog"
	"strconv"
	"strings"

	"keeper.websocket.go/internal/config"
)

type Authorizer struct {
	cfg    *config.Config
	logger *slog.Logger
}

func NewAuthorizer(cfg *config.Config, logger *slog.Logger) *Authorizer {
	return &Authorizer{
		cfg:    cfg,
		logger: logger,
	}
}

func (a *Authorizer) CanSubscribe(username string, authorizedRooms map[int64]bool, topic string) bool {
	if topic == "presence:lobby" {
		return true
	}

	parts := strings.Split(topic, ":")
	if len(parts) != 2 {
		a.logger.Warn("invalid topic format for authorization", "topic", topic)
		return false
	}

	resourceType := parts[0]
	resourceID := parts[1]

	switch resourceType {
	case "room":
		roomID, err := strconv.ParseInt(resourceID, 10, 64)
		if err != nil {
			a.logger.Warn("invalid room ID format in topic subscription", "topic", topic)
			return false
		}

		if _, ok := authorizedRooms[roomID]; ok {
			return true
		}

		a.logger.Warn("user not authorized for room based on JWT claims", "user", username, "roomID", roomID)
		return false

	case "user":
		return username == resourceID

	default:
		return false
	}
}
