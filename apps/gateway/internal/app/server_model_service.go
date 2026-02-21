package app

import (
	modelservice "nextai/apps/gateway/internal/service/model"
)

func (s *Server) getModelService() *modelservice.Service {
	if s.modelService == nil {
		s.modelService = s.newModelService()
	}
	return s.modelService
}

func (s *Server) newModelService() *modelservice.Service {
	return modelservice.NewService(modelservice.Dependencies{
		Store: s.stateStore,
	})
}
