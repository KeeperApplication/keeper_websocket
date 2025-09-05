package websocket

import "context"

type CommandPublisher interface {
	Publish(ctx context.Context, routingKey string, body interface{}) error
}
