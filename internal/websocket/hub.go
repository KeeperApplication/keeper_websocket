package websocket

import (
	"encoding/json"

	"keeper.websocket.go/internal/shared"
)

type Hub struct {
	clients     map[*Client]bool
	topics      map[string]map[*Client]bool
	Broadcast   chan *shared.InternalBroadcast
	Register    chan *Client
	Unregister  chan *Client
	Subscribe   chan *Subscription
	Unsubscribe chan *Subscription
}

func NewHub() *Hub {
	return &Hub{
		clients:     make(map[*Client]bool),
		topics:      make(map[string]map[*Client]bool),
		Broadcast:   make(chan *shared.InternalBroadcast),
		Register:    make(chan *Client),
		Unregister:  make(chan *Client),
		Subscribe:   make(chan *Subscription),
		Unsubscribe: make(chan *Subscription),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.clients[client] = true

		case client := <-h.Unregister:
			if _, ok := h.clients[client]; ok {
				for topic := range client.topics {
					h.removeClientFromTopic(client, topic)
				}
				delete(h.clients, client)
				close(client.Send)
			}

		case subscription := <-h.Subscribe:
			topic := subscription.Topic
			if _, ok := h.topics[topic]; !ok {
				h.topics[topic] = make(map[*Client]bool)
			}
			h.topics[topic][subscription.Client] = true

		case subscription := <-h.Unsubscribe:
			h.removeClientFromTopic(subscription.Client, subscription.Topic)

		case broadcast := <-h.Broadcast:
			var msg shared.BroadcastMessage
			if err := json.Unmarshal(broadcast.Message, &msg); err != nil {
				continue
			}
			var originClient *Client
			if broadcast.Origin != nil {
				if c, ok := broadcast.Origin.(*Client); ok {
					originClient = c
				}
			}
			h.broadcastToTopic(broadcast.Message, msg.Topic, originClient)
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
