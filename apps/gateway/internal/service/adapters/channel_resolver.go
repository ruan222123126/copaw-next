package adapters

import (
	"errors"

	"nextai/apps/gateway/internal/service/ports"
)

type ChannelResolver struct {
	ResolveChannelFunc func(name string) (ports.Channel, map[string]interface{}, string, error)
}

func (r ChannelResolver) ResolveChannel(name string) (ports.Channel, map[string]interface{}, string, error) {
	if r.ResolveChannelFunc == nil {
		return nil, nil, "", errors.New("channel resolver is unavailable")
	}
	return r.ResolveChannelFunc(name)
}
