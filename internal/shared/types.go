package shared

const PresenceTopic = "presence:lobby"

type InternalBroadcast struct {
	Message []byte
	Origin  interface{}
}

type MessageCommand struct {
	UserToken     string `json:"userToken"`
	RoomID        int64  `json:"roomId,omitempty"`
	MessageID     int64  `json:"messageId,omitempty"`
	Content       string `json:"content,omitempty"`
	RepliedToID   *int64 `json:"repliedToId,omitempty"`
	Emoji         string `json:"emoji,omitempty"`
	LastMessageID int64  `json:"lastMessageId,omitempty"`
}

type BroadcastMessage struct {
	Topic   string      `json:"topic"`
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
}
