package app

import (
	workspaceservice "nextai/apps/gateway/internal/service/workspace"
)

func (s *Server) getWorkspaceService() *workspaceservice.Service {
	if s.workspaceService == nil {
		s.workspaceService = s.newWorkspaceService()
	}
	return s.workspaceService
}

func (s *Server) newWorkspaceService() *workspaceservice.Service {
	supportedChannels := map[string]struct{}{}
	for name := range s.channels {
		supportedChannels[name] = struct{}{}
	}

	return workspaceservice.NewService(workspaceservice.Dependencies{
		Store:             s.stateStore,
		DataDir:           s.cfg.DataDir,
		SupportedChannels: supportedChannels,
		IsTextFilePath: func(path string) bool {
			return isWorkspaceTextFilePath(path)
		},
		ReadTextFile: func(path string) (string, string, error) {
			return readWorkspaceTextFileRawForPath(path)
		},
		WriteTextFile: func(path, content string) error {
			return writeWorkspaceTextFileRawForPath(path, content)
		},
		CollectTextFiles: func() []workspaceservice.FileEntry {
			entries := collectWorkspaceTextFileEntries()
			out := make([]workspaceservice.FileEntry, 0, len(entries))
			for _, item := range entries {
				out = append(out, workspaceservice.FileEntry{
					Path: item.Path,
					Kind: item.Kind,
					Size: item.Size,
				})
			}
			return out
		},
	})
}
