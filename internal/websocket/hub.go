package websocket

import (
	"encoding/json"
	"log/slog"
)

const PresenceTopic = "presence:lobby"

type Hub struct {
	clients     map[*Client]bool
	topics      map[string]map[*Client]bool
	Broadcast   chan *InternalBroadcast
	Register    chan *Client
	Unregister  chan *Client
	Subscribe   chan *Subscription
	Unsubscribe chan *Subscription
	logger      *slog.Logger
}

func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		clients:     make(map[*Client]bool),
		topics:      make(map[string]map[*Client]bool),
		Broadcast:   make(chan *InternalBroadcast),
		Register:    make(chan *Client),
		Unregister:  make(chan *Client),
		Subscribe:   make(chan *Subscription),
		Unsubscribe: make(chan *Subscription),
		logger:      logger,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.clients[client] = true
			h.logger.Info("client registered", "user", client.Username)
			h.broadcastPresenceUpdate()

		case client := <-h.Unregister:
			if _, ok := h.clients[client]; ok {
				for topic := range client.topics {
					h.removeClientFromTopic(client, topic)
				}
				delete(h.clients, client)
				close(client.Send)
				h.logger.Info("client unregistered", "user", client.Username)
				h.broadcastPresenceUpdate()
			}

		case subscription := <-h.Subscribe:
			topic := subscription.Topic
			if _, ok := h.topics[topic]; !ok {
				h.topics[topic] = make(map[*Client]bool)
			}
			h.topics[topic][subscription.Client] = true
			if topic == PresenceTopic {
				h.sendOnlineUserList(subscription.Client)
			}

		case subscription := <-h.Unsubscribe:
			h.removeClientFromTopic(subscription.Client, subscription.Topic)

		case broadcast := <-h.Broadcast:
			var msg BroadcastMessage
			if err := json.Unmarshal(broadcast.Message, &msg); err != nil {
				h.logger.Error("failed to unmarshal broadcast message", "error", err)
				continue
			}
			h.broadcastToTopic(broadcast.Message, msg.Topic, broadcast.Origin)
		}
	}
}

func (h *Hub) broadcastToTopic(message []byte, topic string, origin *Client) {
	if topicClients, ok := h.topics[topic]; ok {
		for client := range topicClients {
			if client == origin {
				continue
			}
			select {
			case client.Send <- message:
			default:
				h.cleanupClient(client)
			}
		}
	}
}

func (h *Hub) cleanupClient(client *Client) {
	if _, ok := h.clients[client]; ok {
		for topic := range client.topics {
			h.removeClientFromTopic(client, topic)
		}
		delete(h.clients, client)
		close(client.Send)
		h.logger.Info("cleaned up disconnected client", "user", client.Username)
	}
}

func (h *Hub) removeClientFromTopic(client *Client, topic string) {
	if topicClients, ok := h.topics[topic]; ok {
		delete(topicClients, client)
		if len(topicClients) == 0 {
			delete(h.topics, topic)
		}
	}
}

func (h *Hub) getOnlineUsernames() []string {
	onlineList := make([]string, 0, len(h.clients))
	userSet := make(map[string]struct{})
	for client := range h.clients {
		userSet[client.Username] = struct{}{}
	}
	for user := range userSet {
		onlineList = append(onlineList, user)
	}
	return onlineList
}

func (h *Hub) broadcastPresenceUpdate() {
	onlineUsers := h.getOnlineUsernames()
	payload := map[string]interface{}{"users": onlineUsers}
	broadcastMsg := BroadcastMessage{
		Topic:   PresenceTopic,
		Event:   "presence_update",
		Payload: payload,
	}
	jsonMsg, err := json.Marshal(broadcastMsg)
	if err != nil {
		h.logger.Error("failed to marshal presence update", "error", err)
		return
	}
	h.broadcastToTopic(jsonMsg, PresenceTopic, nil)
}

func (h *Hub) sendOnlineUserList(client *Client) {
	onlineUsers := h.getOnlineUsernames()
	payload := map[string]interface{}{"users": onlineUsers}
	broadcastMsg := BroadcastMessage{
		Topic:   PresenceTopic,
		Event:   "presence_update",
		Payload: payload,
	}
	jsonMsg, err := json.Marshal(broadcastMsg)
	if err != nil {
		h.logger.Error("failed to marshal online user list", "error", err)
		return
	}
	select {
	case client.Send <- jsonMsg:
	default:
		h.logger.Warn("could not send online user list, client send channel is full or closed", "user", client.Username)
	}
}
