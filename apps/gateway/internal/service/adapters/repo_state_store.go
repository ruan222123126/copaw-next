package adapters

import (
	"errors"

	"nextai/apps/gateway/internal/repo"
)

type RepoStateStore struct {
	Store *repo.Store
}

func NewRepoStateStore(store *repo.Store) RepoStateStore {
	return RepoStateStore{Store: store}
}

func (s RepoStateStore) Read(fn func(state *repo.State)) {
	if s.Store == nil {
		return
	}
	s.Store.Read(fn)
}

func (s RepoStateStore) Write(fn func(state *repo.State) error) error {
	if s.Store == nil {
		return errors.New("state store is unavailable")
	}
	return s.Store.Write(fn)
}
