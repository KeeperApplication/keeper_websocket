package websocket

type ClientMessage struct {
	Action  string      `json:"action"`
	Topic   string      `json:"topic"`
	Payload interface{} `json:"payload"`
}
