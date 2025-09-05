package auth

import (
	"fmt"
	"log/slog"
	"net/http"
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

func (a *Authorizer) CanSubscribe(token string, topic string) bool {
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
		return a.canJoinRoom(token, resourceID)
	case "user":
		username, err := ValidateToken(token, a.cfg.JWTSecret)
		if err != nil {
			return false
		}
		return username == resourceID
	default:
		return false
	}
}

func (a *Authorizer) canJoinRoom(token string, roomID string) bool {
	url := fmt.Sprintf("%s/rooms/%s/authorize-join", a.cfg.JavaApiURL, roomID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		a.logger.Error("failed to create authorization request", "error", err)
		return false
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "keeper-websocket-go/1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		a.logger.Error("failed to perform authorization request", "error", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true
	}

	a.logger.Warn("authorization failed for room", "roomID", roomID, "status", resp.StatusCode)
	return false
}
