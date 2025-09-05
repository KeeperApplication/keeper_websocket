package websocket

type InternalBroadcast struct {
	Message []byte
	Origin  *Client
}
