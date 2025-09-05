package websocket

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"keeper.websocket.go/internal/auth"
	"keeper.websocket.go/internal/coreapi"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

type Client struct {
	Hub        *Hub
	Conn       *websocket.Conn
	Send       chan []byte
	logger     *slog.Logger
	topics     map[string]bool
	Username   string
	token      string
	authorizer *auth.Authorizer
	coreClient *coreapi.Client
}

func NewClient(hub *Hub, conn *websocket.Conn, logger *slog.Logger, username, token string, authorizer *auth.Authorizer, coreClient *coreapi.Client) *Client {
	return &Client{
		Hub:        hub,
		Conn:       conn,
		Send:       make(chan []byte, 256),
		logger:     logger,
		topics:     make(map[string]bool),
		Username:   username,
		token:      token,
		authorizer: authorizer,
		coreClient: coreClient,
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
		//c.logger.Info("DEBUG: ReadPump is waiting for a message...")

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

func (c *Client) handleIncomingMessage(message []byte) {
	// c.logger.Info("DEBUG: handleIncomingMessage received", "message", string(message))

	var msg ClientMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		c.logger.Warn("failed to unmarshal client message", "error", err)
		return
	}

	switch msg.Action {
	case "subscribe":
		if !c.authorizer.CanSubscribe(c.token, msg.Topic) {
			c.logger.Warn("authorization failed for subscription", "topic", msg.Topic, "user", c.Username)
			return
		}
		c.topics[msg.Topic] = true
		c.Hub.Subscribe <- &Subscription{Client: c, Topic: msg.Topic}
		c.logger.Info("client subscribed to topic", "topic", msg.Topic, "user", c.Username)

	case "unsubscribe":
		delete(c.topics, msg.Topic)
		c.Hub.Unsubscribe <- &Subscription{Client: c, Topic: msg.Topic}
		c.logger.Info("client unsubscribed from topic", "topic", msg.Topic, "user", c.Username)

	case "typing":
		payload := map[string]interface{}{
			"type":           "TYPING",
			"senderUsername": c.Username,
			"roomId":         msg.Topic,
		}
		broadcastMsg := BroadcastMessage{
			Topic:   msg.Topic,
			Event:   "new_event",
			Payload: payload,
		}
		jsonMsg, err := json.Marshal(broadcastMsg)
		if err != nil {
			c.logger.Error("failed to marshal typing event", "error", err)
			return
		}
		c.Hub.Broadcast <- &InternalBroadcast{
			Message: jsonMsg,
			Origin:  c,
		}

	case "new_message":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			c.logger.Warn("invalid payload for new_message", "payload", msg.Payload)
			return
		}
		content, ok := payload["content"].(string)
		if !ok || content == "" {
			c.logger.Warn("content is missing or invalid in new_message payload")
			return
		}
		roomID := strings.TrimPrefix(msg.Topic, "room:")
		if err := c.coreClient.PostNewMessage(c.token, roomID, content); err != nil {
			c.logger.Error("failed to post new message", "error", err)
		}

	case "edit_message":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			c.logger.Warn("invalid payload for edit_message", "payload", msg.Payload)
			return
		}
		messageID, idOk := payload["messageId"].(string)
		content, contentOk := payload["content"].(string)
		if !idOk || !contentOk || messageID == "" {
			c.logger.Warn("invalid data for edit_message payload")
			return
		}
		if err := c.coreClient.EditMessage(c.token, messageID, content); err != nil {
			c.logger.Error("failed to edit message", "error", err)
		}

	case "delete_message":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			c.logger.Warn("invalid payload for delete_message", "payload", msg.Payload)
			return
		}
		messageID, ok := payload["messageId"].(string)
		if !ok || messageID == "" {
			c.logger.Warn("messageId is missing in delete_message payload")
			return
		}
		if err := c.coreClient.DeleteMessage(c.token, messageID); err != nil {
			c.logger.Error("failed to delete message", "error", err)
		}

	case "toggle_pin":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			c.logger.Warn("invalid payload for toggle_pin", "payload", msg.Payload)
			return
		}
		messageID, ok := payload["messageId"].(string)
		if !ok || messageID == "" {
			c.logger.Warn("messageId is missing in toggle_pin payload")
			return
		}
		if err := c.coreClient.TogglePinMessage(c.token, messageID); err != nil {
			c.logger.Error("failed to toggle pin", "error", err)
		}

	case "toggle_reaction":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			c.logger.Warn("invalid payload for toggle_reaction", "payload", msg.Payload)
			return
		}
		messageID, idOk := payload["messageId"].(string)
		emoji, emojiOk := payload["emoji"].(string)
		if !idOk || !emojiOk || messageID == "" || emoji == "" {
			c.logger.Warn("invalid data for toggle_reaction payload")
			return
		}
		if err := c.coreClient.ToggleReaction(c.token, messageID, emoji); err != nil {
			c.logger.Error("failed to toggle reaction", "error", err)
		}

	case "messages_seen":
		payload, ok := msg.Payload.(map[string]interface{})
		if !ok {
			c.logger.Warn("invalid payload for messages_seen", "payload", msg.Payload)
			return
		}
		lastMessageID, ok := payload["lastMessageId"].(float64)
		if !ok || lastMessageID <= 0 {
			c.logger.Warn("lastMessageId is missing or invalid in messages_seen payload")
			return
		}
		roomID := strings.TrimPrefix(msg.Topic, "room:")
		if err := c.coreClient.MarkMessagesAsSeen(c.token, roomID, lastMessageID); err != nil {
			c.logger.Error("failed to mark messages as seen", "error", err)
		}

	default:
		c.logger.Warn("unknown client message action", "action", msg.Action)
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
