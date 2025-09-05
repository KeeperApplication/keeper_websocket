package websocket

type BroadcastMessage struct {
	Topic   string      `json:"topic"`
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
}

type ClientMessage struct {
	Action  string      `json:"action"`
	Topic   string      `json:"topic"`
	Payload interface{} `json:"payload"`
}
