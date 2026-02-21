package ports

import "nextai/apps/gateway/internal/repo"

type StateStore interface {
	Read(func(state *repo.State))
	Write(func(state *repo.State) error) error
}
