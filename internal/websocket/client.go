package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"keeper.websocket.go/internal/auth"
	"keeper.websocket.go/internal/presence"
	"keeper.websocket.go/internal/shared"
)

type PublishFunc func(ctx context.Context, routingKey string, body interface{}) error

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

type Client struct {
	Hub             *Hub
	Conn            *websocket.Conn
	Send            chan []byte
	logger          *slog.Logger
	topics          map[string]bool
	Username        string
	token           string
	authorizer      *auth.Authorizer
	publish         PublishFunc
	presenceService *presence.Service
}

func NewClient(hub *Hub, conn *websocket.Conn, logger *slog.Logger, username, token string, authorizer *auth.Authorizer, publish PublishFunc, presenceService *presence.Service) *Client {
	return &Client{
		Hub:             hub,
		Conn:            conn,
		Send:            make(chan []byte, 256),
		logger:          logger,
		topics:          make(map[string]bool),
		Username:        username,
		token:           token,
		authorizer:      authorizer,
		publish:         publish,
		presenceService: presenceService,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()
	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error("unexpected close error", "error", err)
			}
			break
		}
		c.handleIncomingMessage(message)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)
			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleIncomingMessage(message []byte) {
	var msg ClientMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		c.logger.Warn("failed to unmarshal client message", "error", err)
		return
	}
	routingKey := "cmd.v2.message." + msg.Action
	command := shared.MessageCommand{UserToken: c.token}
	payload, ok := msg.Payload.(map[string]interface{})
	if !ok && msg.Payload != nil {
		c.logger.Warn("invalid payload format for action", "action", msg.Action)
		return
	}
	var err error
	switch msg.Action {
	case "subscribe", "unsubscribe", "typing":
		c.handleLocalAction(&msg)
		return
	case "new_message":
		roomID, err := strconv.ParseInt(strings.TrimPrefix(msg.Topic, "room:"), 10, 64)
		if err != nil {
			c.logger.Warn("invalid room ID in topic", "topic", msg.Topic)
			return
		}
		command.RoomID = roomID
		command.Content, _ = payload["content"].(string)
		if repliedToIDVal, ok := payload["repliedToId"]; ok && repliedToIDVal != nil {
			if repliedToIDFloat, ok := repliedToIDVal.(float64); ok {
				repliedToID := int64(repliedToIDFloat)
				command.RepliedToID = &repliedToID
			}
		}
	case "edit_message":
		command.MessageID, err = extractID(payload, "messageId")
		if err != nil {
			c.logger.Warn("failed to extract messageId for edit", "error", err)
			return
		}
		command.Content, _ = payload["content"].(string)
	case "delete_message":
		command.MessageID, err = extractID(payload, "messageId")
		if err != nil {
			c.logger.Warn("failed to extract messageId for delete", "error", err)
			return
		}
	case "toggle_pin":
		command.MessageID, err = extractID(payload, "messageId")
		if err != nil {
			c.logger.Warn("failed to extract messageId for toggle_pin", "error", err)
			return
		}
	case "toggle_reaction":
		command.MessageID, err = extractID(payload, "messageId")
		if err != nil {
			c.logger.Warn("failed to extract messageId for toggle_reaction", "error", err)
			return
		}
		command.Emoji, _ = payload["emoji"].(string)
	case "messages_seen":
		roomID, err := strconv.ParseInt(strings.TrimPrefix(msg.Topic, "room:"), 10, 64)
		if err != nil {
			c.logger.Warn("invalid room ID in topic", "topic", msg.Topic)
			return
		}
		command.RoomID = roomID
		command.LastMessageID, err = extractID(payload, "lastMessageId")
		if err != nil {
			c.logger.Warn("failed to extract lastMessageId for messages_seen", "error", err)
			return
		}
	default:
		c.logger.Warn("unknown client message action", "action", msg.Action)
		return
	}
	if err := c.publish(context.Background(), routingKey, command); err != nil {
		c.logger.Error("failed to publish command", "action", msg.Action, "error", err)
	}
}

func (c *Client) handleLocalAction(msg *ClientMessage) {
	switch msg.Action {
	case "subscribe":
		if !c.authorizer.CanSubscribe(c.token, msg.Topic) {
			c.logger.Warn("authorization failed for subscription", "topic", msg.Topic, "user", c.Username)
			return
		}
		c.topics[msg.Topic] = true
		c.Hub.Subscribe <- &Subscription{Client: c, Topic: msg.Topic}
		c.logger.Info("client subscribed to topic", "topic", msg.Topic, "user", c.Username)

		if msg.Topic == shared.PresenceTopic {
			payload, err := c.presenceService.GetPresenceUpdatePayload(context.Background())
			if err != nil {
				c.logger.Error("failed to get initial presence payload for new subscriber", "error", err)
				return
			}
			c.Send <- payload
		}

	case "unsubscribe":
		delete(c.topics, msg.Topic)
		c.Hub.Unsubscribe <- &Subscription{Client: c, Topic: msg.Topic}
		c.logger.Info("client unsubscribed from topic", "topic", msg.Topic, "user", c.Username)
	case "typing":
		roomID, err := strconv.ParseInt(strings.TrimPrefix(msg.Topic, "room:"), 10, 64)
		if err != nil {
			c.logger.Warn("invalid roomID in typing topic", "topic", msg.Topic, "error", err)
			return
		}
		payload := map[string]interface{}{
			"type":           "TYPING",
			"senderUsername": c.Username,
			"roomId":         roomID,
		}
		broadcastMsg := shared.BroadcastMessage{
			Topic:   msg.Topic,
			Event:   "new_event",
			Payload: payload,
		}
		jsonMsg, err := json.Marshal(broadcastMsg)
		if err != nil {
			c.logger.Error("failed to marshal typing event", "error", err)
			return
		}
		c.Hub.Broadcast <- &shared.InternalBroadcast{
			Message: jsonMsg,
			Origin:  c,
		}
	}
}

func extractID(payload map[string]interface{}, key string) (int64, error) {
	val, ok := payload[key]
	if !ok {
		return 0, fmt.Errorf("key '%s' not found in payload", key)
	}
	switch v := val.(type) {
	case float64:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("invalid type for key '%s': %T", key, v)
	}
}
