package ports

import "context"

type Channel interface {
	SendText(ctx context.Context, userID, sessionID, text string, cfg map[string]interface{}) error
}

type ChannelResolver interface {
	ResolveChannel(name string) (Channel, map[string]interface{}, string, error)
}
