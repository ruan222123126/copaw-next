package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/plugin"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/runner"
	modelservice "nextai/apps/gateway/internal/service/model"
	workspaceservice "nextai/apps/gateway/internal/service/workspace"
)

func (s *Server) listProviders(w http.ResponseWriter, _ *http.Request) {
	providers, err := s.getModelService().ListProviders()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) getModelCatalog(w http.ResponseWriter, _ *http.Request) {
	out, err := s.getModelService().GetCatalog()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) configureProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey       *string            `json:"api_key"`
		BaseURL      *string            `json:"base_url"`
		DisplayName  *string            `json:"display_name"`
		Enabled      *bool              `json:"enabled"`
		Headers      *map[string]string `json:"headers"`
		TimeoutMS    *int               `json:"timeout_ms"`
		ModelAliases *map[string]string `json:"model_aliases"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	out, err := s.getModelService().ConfigureProvider(modelservice.ConfigureProviderInput{
		ProviderID:   chi.URLParam(r, "provider_id"),
		APIKey:       body.APIKey,
		BaseURL:      body.BaseURL,
		DisplayName:  body.DisplayName,
		Enabled:      body.Enabled,
		Headers:      body.Headers,
		TimeoutMS:    body.TimeoutMS,
		ModelAliases: body.ModelAliases,
	})
	if err != nil {
		if validation := (*modelservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	deleted, err := s.getModelService().DeleteProvider(chi.URLParam(r, "provider_id"))
	if err != nil {
		if validation := (*modelservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) getActiveModels(w http.ResponseWriter, _ *http.Request) {
	out, err := s.getModelService().GetActiveModels()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setActiveModels(w http.ResponseWriter, r *http.Request) {
	var body domain.ModelSlotConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	out, err := s.getModelService().SetActiveModels(body)
	if err != nil {
		if validation := (*modelservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		switch {
		case errors.Is(err, modelservice.ErrProviderNotFound):
			writeErr(w, http.StatusNotFound, "provider_not_found", "provider not found", nil)
			return
		case errors.Is(err, modelservice.ErrProviderDisabled):
			writeErr(w, http.StatusBadRequest, "provider_disabled", "provider is disabled", nil)
			return
		case errors.Is(err, modelservice.ErrModelNotFound):
			writeErr(w, http.StatusBadRequest, "model_not_found", "model not found for provider", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listEnvs(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.EnvVar, 0)
	s.store.Read(func(st *repo.State) {
		for k, v := range st.Envs {
			out = append(out, domain.EnvVar{Key: k, Value: v})
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putEnvs(w http.ResponseWriter, r *http.Request) {
	body := map[string]string{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	for k := range body {
		if strings.TrimSpace(k) == "" {
			writeErr(w, http.StatusBadRequest, "invalid_env_key", "env key cannot be empty", nil)
			return
		}
	}
	if err := s.store.Write(func(st *repo.State) error {
		st.Envs = map[string]string{}
		for k, v := range body {
			st.Envs[strings.TrimSpace(k)] = v
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	s.listEnvs(w, r)
}

func (s *Server) deleteEnv(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	exists := false
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.Envs[key]; ok {
			exists = true
			delete(st.Envs, key)
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "not_found", "env key not found", nil)
		return
	}
	s.listEnvs(w, r)
}

func (s *Server) listSkills(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.SkillSpec, 0)
	s.store.Read(func(st *repo.State) {
		for _, spec := range st.Skills {
			out = append(out, spec)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listAvailableSkills(w http.ResponseWriter, _ *http.Request) {
	out := make([]domain.SkillSpec, 0)
	s.store.Read(func(st *repo.State) {
		for _, spec := range st.Skills {
			if spec.Enabled {
				out = append(out, spec)
			}
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) batchDisableSkills(w http.ResponseWriter, r *http.Request) {
	s.batchSetSkillEnabled(w, r, false)
}

func (s *Server) batchEnableSkills(w http.ResponseWriter, r *http.Request) {
	s.batchSetSkillEnabled(w, r, true)
}

func (s *Server) batchSetSkillEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	var names []string
	if err := json.NewDecoder(r.Body).Decode(&names); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.store.Write(func(st *repo.State) error {
		for _, name := range names {
			v, ok := st.Skills[name]
			if !ok {
				continue
			}
			v.Enabled = enabled
			st.Skills[name] = v
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string                 `json:"name"`
		Content    string                 `json:"content"`
		References map[string]interface{} `json:"references"`
		Scripts    map[string]interface{} `json:"scripts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Content) == "" {
		writeErr(w, http.StatusBadRequest, "invalid_skill", "name and content are required", nil)
		return
	}
	created := false
	if err := s.store.Write(func(st *repo.State) error {
		name := strings.TrimSpace(body.Name)
		st.Skills[name] = domain.SkillSpec{
			Name:       name,
			Content:    body.Content,
			Source:     "customized",
			Path:       filepath.Join(s.cfg.DataDir, "skills", name),
			References: safeMap(body.References),
			Scripts:    safeMap(body.Scripts),
			Enabled:    true,
		}
		created = true
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"created": created})
}

func (s *Server) disableSkill(w http.ResponseWriter, r *http.Request) {
	s.setSkillEnabled(w, chi.URLParam(r, "skill_name"), false)
}

func (s *Server) enableSkill(w http.ResponseWriter, r *http.Request) {
	s.setSkillEnabled(w, chi.URLParam(r, "skill_name"), true)
}

func (s *Server) setSkillEnabled(w http.ResponseWriter, name string, enabled bool) {
	exists := false
	if err := s.store.Write(func(st *repo.State) error {
		v, ok := st.Skills[name]
		if !ok {
			return nil
		}
		exists = true
		v.Enabled = enabled
		st.Skills[name] = v
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "not_found", "skill not found", nil)
		return
	}
	key := "enabled"
	if !enabled {
		key = "disabled"
	}
	writeJSON(w, http.StatusOK, map[string]bool{key: true})
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "skill_name")
	deleted := false
	if err := s.store.Write(func(st *repo.State) error {
		if _, ok := st.Skills[name]; ok {
			delete(st.Skills, name)
			deleted = true
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) loadSkillFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "skill_name")
	filePath := chi.URLParam(r, "file_path")
	var content string
	found := false
	s.store.Read(func(st *repo.State) {
		skill, ok := st.Skills[name]
		if !ok {
			return
		}
		content, found = readSkillVirtualFile(skill, filePath)
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "skill file not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func readSkillVirtualFile(skill domain.SkillSpec, filePath string) (string, bool) {
	parts := strings.Split(strings.Trim(filePath, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	var node interface{}
	switch parts[0] {
	case "references":
		node = skill.References
	case "scripts":
		node = skill.Scripts
	default:
		return "", false
	}
	for _, p := range parts[1:] {
		m, ok := node.(map[string]interface{})
		if !ok {
			return "", false
		}
		n, ok := m[p]
		if !ok {
			return "", false
		}
		node = n
	}
	s, ok := node.(string)
	return s, ok
}

const (
	workspaceFileEnvs      = "config/envs.json"
	workspaceFileChannels  = "config/channels.json"
	workspaceFileModels    = "config/models.json"
	workspaceFileActiveLLM = "config/active-llm.json"
	workspaceFileAITools   = aiToolsGuideRelativePath
	workspaceDocsAIDir     = "docs/AI"
	workspacePromptsDir    = "prompts"
	workspacePromptDir     = "prompt"
)

type workspaceFileEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int    `json:"size"`
}

type workspaceFileListResponse struct {
	Files []workspaceFileEntry `json:"files"`
}

type workspaceExportModels struct {
	Providers map[string]repo.ProviderSetting `json:"providers"`
	ActiveLLM domain.ModelSlotConfig          `json:"active_llm"`
}

type workspaceExportConfig struct {
	Envs     map[string]string       `json:"envs"`
	Channels domain.ChannelConfigMap `json:"channels"`
	Models   workspaceExportModels   `json:"models"`
}

type workspaceExportPayload struct {
	Version string                      `json:"version"`
	Skills  map[string]domain.SkillSpec `json:"skills"`
	Config  workspaceExportConfig       `json:"config"`
}

type workspaceImportRequest struct {
	Mode    string                 `json:"mode"`
	Payload workspaceExportPayload `json:"payload"`
}

func (s *Server) listWorkspaceFiles(w http.ResponseWriter, _ *http.Request) {
	result, err := s.getWorkspaceService().ListFiles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	out := workspaceFileListResponse{Files: make([]workspaceFileEntry, 0, len(result.Files))}
	for _, item := range result.Files {
		out.Files = append(out.Files, workspaceFileEntry{
			Path: item.Path,
			Kind: item.Kind,
			Size: item.Size,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath, ok := workspaceFilePathFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	data, err := s.getWorkspaceService().GetFile(filePath)
	if err != nil {
		if errors.Is(err, workspaceservice.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "workspace file not found", nil)
			return
		}
		if fileErr := (*workspaceservice.FileError)(nil); errors.As(err, &fileErr) {
			writeErr(w, http.StatusInternalServerError, "file_error", fileErr.Error(), nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) putWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath, ok := workspaceFilePathFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.getWorkspaceService().PutFile(filePath, body); err != nil {
		if validation := (*workspaceservice.ValidationError)(nil); errors.As(err, &validation) {
			status := http.StatusBadRequest
			if validation.Code == "provider_not_found" {
				status = http.StatusNotFound
			}
			writeErr(w, status, validation.Code, validation.Message, nil)
			return
		}
		if fileErr := (*workspaceservice.FileError)(nil); errors.As(err, &fileErr) {
			writeErr(w, http.StatusInternalServerError, "file_error", fileErr.Error(), nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

func (s *Server) deleteWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	filePath, ok := workspaceFilePathFromRequest(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid_path", "invalid workspace file path", nil)
		return
	}
	deleted, err := s.getWorkspaceService().DeleteFile(filePath)
	if err != nil {
		if errors.Is(err, workspaceservice.ErrMethodNotAllowed) {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "config files cannot be deleted", nil)
			return
		}
		if validation := (*workspaceservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": deleted})
}

func (s *Server) exportWorkspace(w http.ResponseWriter, _ *http.Request) {
	result, err := s.getWorkspaceService().Export()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	out := workspaceExportPayload{
		Version: result.Version,
		Skills:  result.Skills,
		Config: workspaceExportConfig{
			Envs:     result.Config.Envs,
			Channels: result.Config.Channels,
			Models: workspaceExportModels{
				Providers: result.Config.Models.Providers,
				ActiveLLM: result.Config.Models.ActiveLLM,
			},
		},
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) importWorkspace(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.getWorkspaceService().Import(body); err != nil {
		if validation := (*workspaceservice.ValidationError)(nil); errors.As(err, &validation) {
			writeErr(w, http.StatusBadRequest, validation.Code, validation.Message, nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"imported": true})
}

func collectWorkspaceFiles(st *repo.State) []workspaceFileEntry {
	files := []workspaceFileEntry{
		{Path: workspaceFileEnvs, Kind: "config", Size: jsonSize(cloneWorkspaceEnvs(st.Envs))},
		{Path: workspaceFileChannels, Kind: "config", Size: jsonSize(cloneWorkspaceChannels(st.Channels))},
		{Path: workspaceFileModels, Kind: "config", Size: jsonSize(cloneWorkspaceProviders(st.Providers))},
		{Path: workspaceFileActiveLLM, Kind: "config", Size: jsonSize(st.ActiveLLM)},
	}
	for name, spec := range st.Skills {
		files = append(files, workspaceFileEntry{
			Path: workspaceSkillFilePath(name),
			Kind: "skill",
			Size: jsonSize(cloneWorkspaceSkill(spec)),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func collectWorkspaceTextFileEntries() []workspaceFileEntry {
	files := collectWorkspaceDocsAIFileEntries()
	files = append(files, collectWorkspacePromptFileEntries()...)
	if aiToolsFile, ok := workspaceAIToolsFileEntry(); ok {
		files = append(files, aiToolsFile)
	}
	return mergeWorkspaceFileEntries(nil, files...)
}

func collectWorkspaceDocsAIFileEntries() []workspaceFileEntry {
	return collectWorkspaceDirFileEntries(workspaceDocsAIDir, isWorkspaceDocsAIFilePath)
}

func collectWorkspacePromptFileEntries() []workspaceFileEntry {
	files := collectWorkspaceDirFileEntries(workspacePromptsDir, isWorkspacePromptFilePath)
	files = append(files, collectWorkspaceDirFileEntries(workspacePromptDir, isWorkspacePromptFilePath)...)
	return mergeWorkspaceFileEntries(nil, files...)
}

func collectWorkspaceDirFileEntries(relativeDir string, allow func(string) bool) []workspaceFileEntry {
	if allow == nil {
		return nil
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return nil
	}
	targetDir := filepath.Join(repoRoot, filepath.FromSlash(relativeDir))
	info, err := os.Stat(targetDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	files := []workspaceFileEntry{}
	_ = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil
		}
		relPath := filepath.ToSlash(rel)
		if !allow(relPath) {
			return nil
		}
		files = append(files, workspaceFileEntry{
			Path: relPath,
			Kind: "config",
			Size: int(info.Size()),
		})
		return nil
	})
	return files
}

func mergeWorkspaceFileEntries(base []workspaceFileEntry, extra ...workspaceFileEntry) []workspaceFileEntry {
	out := make([]workspaceFileEntry, 0, len(base)+len(extra))
	indexByPath := map[string]int{}
	entries := append(append([]workspaceFileEntry{}, base...), extra...)
	for _, item := range entries {
		path := strings.TrimSpace(item.Path)
		if path == "" {
			continue
		}
		item.Path = path
		if idx, ok := indexByPath[path]; ok {
			if out[idx].Size <= 0 && item.Size > 0 {
				out[idx] = item
			}
			continue
		}
		indexByPath[path] = len(out)
		out = append(out, item)
	}
	return out
}

func readWorkspaceFileData(st *repo.State, filePath string) (interface{}, bool) {
	switch filePath {
	case workspaceFileEnvs:
		return cloneWorkspaceEnvs(st.Envs), true
	case workspaceFileChannels:
		return cloneWorkspaceChannels(st.Channels), true
	case workspaceFileModels:
		return cloneWorkspaceProviders(st.Providers), true
	case workspaceFileActiveLLM:
		return st.ActiveLLM, true
	default:
		name, ok := workspaceSkillNameFromPath(filePath)
		if !ok {
			return nil, false
		}
		spec, exists := st.Skills[name]
		if !exists {
			return nil, false
		}
		return cloneWorkspaceSkill(spec), true
	}
}

func workspaceFilePathFromRequest(r *http.Request) (string, bool) {
	raw := chi.URLParam(r, "*")
	if raw == "" {
		raw = chi.URLParam(r, "file_path")
	}
	return normalizeWorkspaceFilePath(raw)
}

func normalizeWorkspaceFilePath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	if trimmed == "" {
		return "", false
	}
	if unescaped, err := url.PathUnescape(trimmed); err == nil {
		trimmed = unescaped
	}
	trimmed = filepath.ToSlash(trimmed)
	parts := strings.Split(trimmed, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." {
			return "", false
		}
		cleaned = append(cleaned, part)
	}
	return strings.Join(cleaned, "/"), true
}

func workspaceSkillNameFromPath(filePath string) (string, bool) {
	if !strings.HasPrefix(filePath, "skills/") || !strings.HasSuffix(filePath, ".json") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(filePath, "skills/"), ".json"))
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func workspaceSkillFilePath(name string) string {
	return "skills/" + strings.TrimSpace(name) + ".json"
}

func isWorkspaceConfigFile(filePath string) bool {
	return filePath == workspaceFileEnvs ||
		filePath == workspaceFileChannels ||
		filePath == workspaceFileModels ||
		filePath == workspaceFileActiveLLM ||
		isWorkspaceTextFilePath(filePath)
}

func normalizeWorkspaceEnvs(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range in {
		k := strings.TrimSpace(key)
		if k == "" {
			return nil, errors.New("env key cannot be empty")
		}
		out[k] = value
	}
	return out, nil
}

func normalizeWorkspaceChannels(in domain.ChannelConfigMap, supported map[string]plugin.ChannelPlugin) (domain.ChannelConfigMap, error) {
	out := domain.ChannelConfigMap{}
	for name, cfg := range in {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return nil, errors.New("channel name cannot be empty")
		}
		if _, ok := supported[normalized]; !ok {
			return nil, fmt.Errorf("channel %q is not supported", name)
		}
		out[normalized] = cloneWorkspaceJSONMap(cfg)
	}
	return out, nil
}

func normalizeWorkspaceProviders(in map[string]repo.ProviderSetting) (map[string]repo.ProviderSetting, error) {
	out := map[string]repo.ProviderSetting{}
	for rawID, rawSetting := range in {
		id := normalizeProviderID(rawID)
		if id == "" {
			return nil, errors.New("provider id cannot be empty")
		}
		if id == "demo" {
			continue
		}
		setting := rawSetting
		normalizeProviderSetting(&setting)
		if setting.TimeoutMS < 0 {
			return nil, fmt.Errorf("provider %q timeout_ms must be >= 0", rawID)
		}
		setting.Headers = sanitizeStringMap(setting.Headers)
		setting.ModelAliases = sanitizeStringMap(setting.ModelAliases)
		out[id] = setting
	}
	return out, nil
}

func normalizeWorkspaceSkills(in map[string]domain.SkillSpec, dataDir string) (map[string]domain.SkillSpec, error) {
	out := map[string]domain.SkillSpec{}
	for rawName, rawSpec := range in {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return nil, errors.New("skill name cannot be empty")
		}
		content := strings.TrimSpace(rawSpec.Content)
		if content == "" {
			return nil, fmt.Errorf("skill %q content is required", name)
		}
		source := strings.TrimSpace(rawSpec.Source)
		if source == "" {
			source = "customized"
		}
		out[name] = domain.SkillSpec{
			Name:       name,
			Content:    rawSpec.Content,
			Source:     source,
			Path:       filepath.Join(dataDir, "skills", name),
			References: safeMap(rawSpec.References),
			Scripts:    safeMap(rawSpec.Scripts),
			Enabled:    rawSpec.Enabled,
		}
	}
	return out, nil
}

func normalizeWorkspaceActiveLLM(in domain.ModelSlotConfig, providers map[string]repo.ProviderSetting) (domain.ModelSlotConfig, error) {
	providerID := normalizeProviderID(in.ProviderID)
	modelID := strings.TrimSpace(in.Model)
	if providerID == "" && modelID == "" {
		return domain.ModelSlotConfig{}, nil
	}
	if providerID == "" || modelID == "" {
		return domain.ModelSlotConfig{}, errors.New("provider_id and model must be set together")
	}
	if _, ok := providers[providerID]; !ok {
		return domain.ModelSlotConfig{}, errors.New("active_llm provider not found")
	}
	return domain.ModelSlotConfig{ProviderID: providerID, Model: modelID}, nil
}

func cloneWorkspaceEnvs(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneWorkspaceChannels(in domain.ChannelConfigMap) domain.ChannelConfigMap {
	out := domain.ChannelConfigMap{}
	for name, cfg := range in {
		out[name] = cloneWorkspaceJSONMap(cfg)
	}
	return out
}

func cloneWorkspaceProviders(in map[string]repo.ProviderSetting) map[string]repo.ProviderSetting {
	out := map[string]repo.ProviderSetting{}
	for id, raw := range in {
		setting := raw
		normalizeProviderSetting(&setting)
		headers := map[string]string{}
		for key, value := range setting.Headers {
			headers[key] = value
		}
		aliases := map[string]string{}
		for key, value := range setting.ModelAliases {
			aliases[key] = value
		}
		setting.Headers = headers
		setting.ModelAliases = aliases
		if setting.Enabled != nil {
			enabled := *setting.Enabled
			setting.Enabled = &enabled
		}
		out[id] = setting
	}
	return out
}

func cloneWorkspaceSkills(in map[string]domain.SkillSpec) map[string]domain.SkillSpec {
	out := map[string]domain.SkillSpec{}
	for name, spec := range in {
		out[name] = cloneWorkspaceSkill(spec)
	}
	return out
}

func cloneWorkspaceSkill(in domain.SkillSpec) domain.SkillSpec {
	return domain.SkillSpec{
		Name:       in.Name,
		Content:    in.Content,
		Source:     in.Source,
		Path:       in.Path,
		References: cloneWorkspaceJSONMap(in.References),
		Scripts:    cloneWorkspaceJSONMap(in.Scripts),
		Enabled:    in.Enabled,
	}
}

func cloneWorkspaceJSONMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	buf, err := json.Marshal(in)
	if err != nil {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(buf, &out); err != nil {
		return map[string]interface{}{}
	}
	if out == nil {
		return map[string]interface{}{}
	}
	return out
}

func jsonSize(v interface{}) int {
	buf, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(buf)
}

func (s *Server) listChannels(w http.ResponseWriter, _ *http.Request) {
	var out domain.ChannelConfigMap
	s.store.Read(func(st *repo.State) {
		out = st.Channels
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listChannelTypes(w http.ResponseWriter, _ *http.Request) {
	out := make([]string, 0, len(s.channels))
	for name := range s.channels {
		out = append(out, name)
	}
	sort.Strings(out)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putChannels(w http.ResponseWriter, r *http.Request) {
	var body domain.ChannelConfigMap
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	for name := range body {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if _, ok := s.channels[normalized]; !ok {
			writeErr(w, http.StatusBadRequest, "channel_not_supported", fmt.Sprintf("channel %q is not supported", name), nil)
			return
		}
	}
	if err := s.store.Write(func(st *repo.State) error {
		st.Channels = domain.ChannelConfigMap{}
		for name, cfg := range body {
			st.Channels[strings.ToLower(strings.TrimSpace(name))] = cfg
		}
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) getChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "channel_name")
	normalized := strings.ToLower(strings.TrimSpace(name))
	found := false
	var out map[string]interface{}
	s.store.Read(func(st *repo.State) {
		out, found = st.Channels[normalized]
	})
	if !found {
		writeErr(w, http.StatusNotFound, "not_found", "channel not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) putChannel(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "channel_name")
	normalized := strings.ToLower(strings.TrimSpace(name))
	if _, ok := s.channels[normalized]; !ok {
		writeErr(w, http.StatusBadRequest, "channel_not_supported", fmt.Sprintf("channel %q is not supported", name), nil)
		return
	}
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_json", "invalid request body", nil)
		return
	}
	if err := s.store.Write(func(st *repo.State) error {
		if st.Channels == nil {
			st.Channels = domain.ChannelConfigMap{}
		}
		st.Channels[normalized] = body
		return nil
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "store_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func mapRunnerError(err error) (status int, code string, message string) {
	var runnerErr *runner.RunnerError
	if errors.As(err, &runnerErr) {
		switch runnerErr.Code {
		case runner.ErrorCodeProviderNotConfigured:
			return http.StatusBadRequest, runnerErr.Code, runnerErr.Message
		case runner.ErrorCodeProviderNotSupported:
			return http.StatusBadRequest, runnerErr.Code, runnerErr.Message
		case runner.ErrorCodeProviderRequestFailed:
			return http.StatusBadGateway, runnerErr.Code, runnerErr.Message
		case runner.ErrorCodeProviderInvalidReply:
			return http.StatusBadGateway, runnerErr.Code, runnerErr.Message
		default:
			return http.StatusInternalServerError, "runner_error", "runner execution failed"
		}
	}
	return http.StatusInternalServerError, "runner_error", "runner execution failed"
}

func mapToolError(err error) (status int, code string, message string) {
	var te *toolError
	if errors.As(err, &te) {
		switch te.Code {
		case "tool_disabled":
			return http.StatusForbidden, te.Code, te.Message
		case "tool_not_supported":
			return http.StatusBadRequest, te.Code, te.Message
		case "tool_invoke_failed":
			switch {
			case errors.Is(te.Err, plugin.ErrShellToolCommandMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input command is required"
			case errors.Is(te.Err, plugin.ErrShellToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrShellToolExecutorUnavailable):
				return http.StatusBadGateway, "tool_runtime_unavailable", "shell executor is unavailable on current host"
			case errors.Is(te.Err, plugin.ErrFileLinesToolPathMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input path is required"
			case errors.Is(te.Err, plugin.ErrFileLinesToolPathInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input path is invalid"
			case errors.Is(te.Err, plugin.ErrFileLinesToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrFileLinesToolStartInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input start must be an integer >= 1"
			case errors.Is(te.Err, plugin.ErrFileLinesToolEndInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input end must be an integer >= 1"
			case errors.Is(te.Err, plugin.ErrFileLinesToolRangeInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input line range is invalid"
			case errors.Is(te.Err, plugin.ErrFileLinesToolRangeTooLarge):
				return http.StatusBadRequest, "invalid_tool_input", "tool input line range is too large"
			case errors.Is(te.Err, plugin.ErrFileLinesToolContentMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input content is required"
			case errors.Is(te.Err, plugin.ErrFileLinesToolOutOfRange):
				return http.StatusBadRequest, "invalid_tool_input", "tool input line range is out of file bounds"
			case errors.Is(te.Err, plugin.ErrFileLinesToolFileNotFound):
				return http.StatusBadRequest, "invalid_tool_input", "target file does not exist"
			case errors.Is(te.Err, plugin.ErrBrowserToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrBrowserToolTaskMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input task is required"
			case errors.Is(te.Err, plugin.ErrSearchToolItemsInvalid):
				return http.StatusBadRequest, "invalid_tool_input", "tool input items must be a non-empty array of objects"
			case errors.Is(te.Err, plugin.ErrSearchToolQueryMissing):
				return http.StatusBadRequest, "invalid_tool_input", "tool input query is required"
			case errors.Is(te.Err, plugin.ErrSearchToolProviderUnsupported):
				return http.StatusBadRequest, "invalid_tool_input", "tool input provider is unsupported"
			case errors.Is(te.Err, plugin.ErrSearchToolProviderUnconfigured):
				return http.StatusBadRequest, "invalid_tool_input", "tool input provider is not configured"
			default:
				return http.StatusBadGateway, te.Code, te.Message
			}
		case "tool_invalid_result":
			return http.StatusBadGateway, te.Code, te.Message
		default:
			return http.StatusInternalServerError, "tool_error", "tool execution failed"
		}
	}
	return http.StatusInternalServerError, "tool_error", "tool execution failed"
}

func compactFeedbackField(raw string, maxLen int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	compact := strings.Join(strings.Fields(trimmed), " ")
	if maxLen <= 0 {
		return compact
	}
	runes := []rune(compact)
	if len(runes) <= maxLen {
		return compact
	}
	return string(runes[:maxLen]) + "...(truncated)"
}

func formatProviderToolArgumentsErrorFeedback(toolName, rawArguments, parseErr string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "unknown_tool"
	}
	detail := compactFeedbackField(parseErr, 160)
	if detail == "" {
		detail = "invalid json arguments"
	}
	raw := compactFeedbackField(rawArguments, 320)
	if raw == "" {
		raw = "{}"
	}
	return fmt.Sprintf(
		"tool_error code=invalid_tool_input message=provider tool call arguments for %s are invalid detail=%s raw_arguments=%s",
		name,
		detail,
		raw,
	)
}

func formatToolErrorFeedback(err error) string {
	if err == nil {
		return "tool_error code=tool_error message=tool execution failed"
	}
	_, code, message := mapToolError(err)
	if strings.TrimSpace(code) == "" {
		code = "tool_error"
	}
	if strings.TrimSpace(message) == "" {
		message = "tool execution failed"
	}
	detail := strings.TrimSpace(err.Error())
	if detail == "" {
		return fmt.Sprintf("tool_error code=%s message=%s", code, message)
	}
	detail = strings.Join(strings.Fields(detail), " ")
	if detail == message {
		return fmt.Sprintf("tool_error code=%s message=%s", code, message)
	}
	return fmt.Sprintf("tool_error code=%s message=%s detail=%s", code, message, detail)
}

func (s *Server) collectProviderCatalog() ([]domain.ProviderInfo, map[string]string, domain.ModelSlotConfig) {
	out := make([]domain.ProviderInfo, 0)
	defaults := map[string]string{}
	active := domain.ModelSlotConfig{}

	s.store.Read(func(st *repo.State) {
		active = st.ActiveLLM
		settingsByID := map[string]repo.ProviderSetting{}

		for rawID, setting := range st.Providers {
			id := normalizeProviderID(rawID)
			if id == "" {
				continue
			}
			normalizeProviderSetting(&setting)
			settingsByID[id] = setting
		}

		ids := make([]string, 0, len(settingsByID))
		for id := range settingsByID {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			setting := settingsByID[id]
			out = append(out, buildProviderInfo(id, setting))
			defaults[id] = provider.DefaultModelID(id)
		}
	})
	return out, defaults, active
}

func buildProviderInfo(providerID string, setting repo.ProviderSetting) domain.ProviderInfo {
	normalizeProviderSetting(&setting)
	spec := provider.ResolveProvider(providerID)
	apiKey := resolveProviderAPIKey(providerID, setting)
	return domain.ProviderInfo{
		ID:                 providerID,
		Name:               spec.Name,
		DisplayName:        resolveProviderDisplayName(setting, spec.Name),
		OpenAICompatible:   provider.ResolveAdapter(providerID) == provider.AdapterOpenAICompatible,
		APIKeyPrefix:       spec.APIKeyPrefix,
		Models:             provider.ResolveModels(providerID, setting.ModelAliases),
		Headers:            sanitizeStringMap(setting.Headers),
		TimeoutMS:          setting.TimeoutMS,
		ModelAliases:       sanitizeStringMap(setting.ModelAliases),
		AllowCustomBaseURL: spec.AllowCustomBaseURL,
		Enabled:            providerEnabled(setting),
		HasAPIKey:          strings.TrimSpace(apiKey) != "",
		CurrentAPIKey:      maskKey(apiKey),
		CurrentBaseURL:     resolveProviderBaseURL(providerID, setting),
	}
}

func resolveProviderAPIKey(providerID string, setting repo.ProviderSetting) string {
	if key := strings.TrimSpace(setting.APIKey); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv(providerEnvPrefix(providerID) + "_API_KEY"))
}

func resolveProviderBaseURL(providerID string, setting repo.ProviderSetting) string {
	if baseURL := strings.TrimSpace(setting.BaseURL); baseURL != "" {
		return baseURL
	}
	if envBaseURL := strings.TrimSpace(os.Getenv(providerEnvPrefix(providerID) + "_BASE_URL")); envBaseURL != "" {
		return envBaseURL
	}
	return provider.ResolveProvider(providerID).DefaultBaseURL
}

func resolveProviderDisplayName(setting repo.ProviderSetting, defaultName string) string {
	if displayName := strings.TrimSpace(setting.DisplayName); displayName != "" {
		return displayName
	}
	return strings.TrimSpace(defaultName)
}

func providerEnvPrefix(providerID string) string {
	return provider.EnvPrefix(providerID)
}

func normalizeProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func providerEnabled(setting repo.ProviderSetting) bool {
	if setting.Enabled == nil {
		return true
	}
	return *setting.Enabled
}

func normalizeProviderSetting(setting *repo.ProviderSetting) {
	if setting == nil {
		return
	}
	setting.DisplayName = strings.TrimSpace(setting.DisplayName)
	setting.APIKey = strings.TrimSpace(setting.APIKey)
	setting.BaseURL = strings.TrimSpace(setting.BaseURL)
	if setting.Enabled == nil {
		enabled := true
		setting.Enabled = &enabled
	}
	if setting.Headers == nil {
		setting.Headers = map[string]string{}
	}
	if setting.ModelAliases == nil {
		setting.ModelAliases = map[string]string{}
	}
}

func sanitizeStringMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range in {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func sanitizeModelAliases(raw *map[string]string) (map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	out := map[string]string{}
	for key, value := range *raw {
		alias := strings.TrimSpace(key)
		modelID := strings.TrimSpace(value)
		if alias == "" || modelID == "" {
			return nil, errors.New("model_aliases requires non-empty key and value")
		}
		out[alias] = modelID
	}
	return out, nil
}

func getProviderSettingByID(st *repo.State, providerID string) repo.ProviderSetting {
	if setting, ok := findProviderSettingByID(st, providerID); ok {
		return setting
	}
	setting := repo.ProviderSetting{}
	normalizeProviderSetting(&setting)
	return setting
}

func findProviderSettingByID(st *repo.State, providerID string) (repo.ProviderSetting, bool) {
	if st == nil {
		return repo.ProviderSetting{}, false
	}
	if setting, ok := st.Providers[providerID]; ok {
		return setting, true
	}
	for key, setting := range st.Providers {
		if normalizeProviderID(key) == providerID {
			return setting, true
		}
	}
	return repo.ProviderSetting{}, false
}

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, code int, errCode, message string, details interface{}) {
	writeJSON(w, code, domain.APIErrorBody{Error: domain.APIError{Code: errCode, Message: message, Details: details}})
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func maskKey(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-3:]
}

func safeMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}

type systemPromptLayer struct {
	Name    string
	Role    string
	Source  string
	Content string
}

func prependSystemLayers(input []domain.AgentInputMessage, layers []systemPromptLayer) []domain.AgentInputMessage {
	effective := make([]domain.AgentInputMessage, 0, len(input)+len(layers))
	for _, layer := range layers {
		if strings.TrimSpace(layer.Content) == "" {
			continue
		}
		effective = append(effective, domain.AgentInputMessage{
			Role:    "system",
			Type:    "message",
			Content: []domain.RuntimeContent{{Type: "text", Text: layer.Content}},
		})
	}
	effective = append(effective, input...)
	return effective
}

func (s *Server) buildSystemLayers() ([]systemPromptLayer, error) {
	layers := make([]systemPromptLayer, 0, 5)

	basePath, baseContent, err := loadRequiredSystemLayer([]string{aiToolsGuideRelativePath})
	if err != nil {
		return nil, err
	}
	layers = append(layers, systemPromptLayer{
		Name:    "base_system",
		Role:    "system",
		Source:  basePath,
		Content: formatLayerSourceContent(basePath, baseContent),
	})

	toolGuidePath, toolGuideContent, err := loadRequiredSystemLayer([]string{
		aiToolsGuideLegacyRelativePath,
		aiToolsGuideLegacyV0RelativePath,
	})
	if err != nil {
		return nil, err
	}
	layers = append(layers, systemPromptLayer{
		Name:    "tool_guide_system",
		Role:    "system",
		Source:  toolGuidePath,
		Content: formatLayerSourceContent(toolGuidePath, toolGuideContent),
	})

	workspacePolicyLayer := systemPromptLayer{
		Name:    "workspace_policy_system",
		Role:    "system",
		Source:  "",
		Content: "",
	}
	layers = appendSystemLayerIfPresent(layers, workspacePolicyLayer)

	sessionPolicyLayer := systemPromptLayer{
		Name:    "session_policy_system",
		Role:    "system",
		Source:  "",
		Content: "",
	}
	layers = appendSystemLayerIfPresent(layers, sessionPolicyLayer)

	if s != nil && s.cfg.EnablePromptContextIntrospect {
		layers = append(layers, buildEnvironmentContextLayer())
	}
	return layers, nil
}

func appendSystemLayerIfPresent(layers []systemPromptLayer, layer systemPromptLayer) []systemPromptLayer {
	if strings.TrimSpace(layer.Content) == "" {
		return layers
	}
	return append(layers, layer)
}

func formatLayerSourceContent(sourcePath, content string) string {
	return fmt.Sprintf("## %s\n%s", sourcePath, content)
}

func loadRequiredSystemLayer(candidatePaths []string) (string, string, error) {
	var lastNotFound error
	for _, candidatePath := range candidatePaths {
		_, rawContent, err := readWorkspaceTextFileRawForPath(candidatePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				lastNotFound = err
				continue
			}
			return "", "", err
		}
		trimmed := strings.TrimSpace(rawContent)
		if trimmed == "" {
			return "", "", fmt.Errorf("system layer is empty: %s", candidatePath)
		}
		return candidatePath, trimmed, nil
	}
	if lastNotFound != nil {
		return "", "", lastNotFound
	}
	return "", "", fmt.Errorf("%w: no candidate paths configured", os.ErrNotExist)
}

func buildEnvironmentContextLayer() systemPromptLayer {
	return systemPromptLayer{
		Name:    "environment_context_system",
		Role:    "system",
		Source:  "runtime",
		Content: buildEnvironmentContextContent(),
	}
}

func buildEnvironmentContextContent() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "unknown"
	}
	network := strings.TrimSpace(os.Getenv("NEXTAI_NETWORK_ACCESS"))
	if network == "" {
		network = "unknown"
	}

	return fmt.Sprintf(
		"<environment_context>\n  <cwd>%s</cwd>\n  <shell>%s</shell>\n  <network>%s</network>\n</environment_context>",
		escapeXMLText(cwd),
		escapeXMLText(shell),
		escapeXMLText(network),
	)
}

func escapeXMLText(v string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	).Replace(v)
}

func summarizeLayerPreview(text string, limit int) string {
	normalized := strings.TrimSpace(text)
	if normalized == "" || limit <= 0 {
		return ""
	}
	runes := []rune(normalized)
	if len(runes) <= limit {
		return normalized
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func estimatePromptTokenCount(text string) int {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return 0
	}

	cjkCount := 0
	remaining := make([]rune, 0, len(normalized))
	for _, r := range normalized {
		if isCJKTokenRune(r) {
			cjkCount++
			remaining = append(remaining, ' ')
			continue
		}
		remaining = append(remaining, r)
	}

	estimate := cjkCount
	for _, chunk := range strings.Fields(string(remaining)) {
		runeLen := len([]rune(chunk))
		if runeLen == 0 {
			continue
		}
		tokenCount := (runeLen + 3) / 4
		if tokenCount < 1 {
			tokenCount = 1
		}
		estimate += tokenCount
	}
	return estimate
}

func isCJKTokenRune(r rune) bool {
	switch {
	case r >= 0x3400 && r <= 0x4DBF:
		return true
	case r >= 0x4E00 && r <= 0x9FFF:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0x3040 && r <= 0x30FF:
		return true
	case r >= 0xAC00 && r <= 0xD7AF:
		return true
	default:
		return false
	}
}

func workspaceAIToolsFileEntry() (workspaceFileEntry, bool) {
	relativePath, content, err := readAIToolsGuideRawWithPath()
	if err != nil {
		return workspaceFileEntry{}, false
	}
	return workspaceFileEntry{
		Path: relativePath,
		Kind: "config",
		Size: len([]byte(content)),
	}, true
}

func readAIToolsGuideRaw() (string, error) {
	_, content, err := readAIToolsGuideRawWithPath()
	if err != nil {
		return "", err
	}
	return content, nil
}

func readAIToolsGuideRawWithPath() (string, string, error) {
	guidePath, relativePath, err := resolveAIToolsGuidePathForRead()
	if err != nil {
		return "", "", err
	}
	content, err := os.ReadFile(guidePath)
	if err != nil {
		return "", "", err
	}
	return relativePath, string(content), nil
}

func writeAIToolsGuideRaw(content string) error {
	return writeAIToolsGuideRawForPath("", content)
}

func writeAIToolsGuideRawForPath(relativePath, content string) error {
	guidePath, _, err := resolveAIToolsGuidePathForWrite(relativePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(guidePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(guidePath, []byte(content), 0o644)
}

func readWorkspaceTextFileRawForPath(relativePath string) (string, string, error) {
	normalized, ok := normalizeAIToolsGuideRelativePath(relativePath)
	if !ok {
		return "", "", errors.New("invalid workspace text file path")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(repoRoot, filepath.FromSlash(normalized))
	content, err := os.ReadFile(target)
	if err != nil {
		return "", "", err
	}
	return normalized, string(content), nil
}

func writeWorkspaceTextFileRawForPath(relativePath, content string) error {
	normalized, ok := normalizeAIToolsGuideRelativePath(relativePath)
	if !ok {
		return errors.New("invalid workspace text file path")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	target := filepath.Join(repoRoot, filepath.FromSlash(normalized))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte(content), 0o644)
}

func isWorkspaceTextFilePath(filePath string) bool {
	return isWorkspaceDocsAIFilePath(filePath) ||
		isWorkspacePromptFilePath(filePath) ||
		isAIToolsWorkspaceFilePath(filePath)
}

func isWorkspaceDocsAIFilePath(filePath string) bool {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return false
	}
	return strings.HasPrefix(path, workspaceDocsAIDir+"/")
}

func isWorkspacePromptFilePath(filePath string) bool {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return false
	}
	return strings.HasPrefix(path, workspacePromptsDir+"/") ||
		strings.HasPrefix(path, workspacePromptDir+"/")
}

func isAIToolsWorkspaceFilePath(filePath string) bool {
	path := strings.TrimSpace(filePath)
	if path == "" {
		return false
	}
	candidates, err := aiToolsGuidePathCandidates()
	if err != nil {
		return false
	}
	for _, candidate := range candidates {
		if path == candidate {
			return true
		}
	}
	return false
}

func resolveAIToolsGuidePathForRead() (string, string, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	candidates, err := aiToolsGuidePathCandidates()
	if err != nil {
		return "", "", err
	}
	for _, relativePath := range candidates {
		guidePath := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
		info, statErr := os.Stat(guidePath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return "", "", statErr
		}
		if info.IsDir() {
			continue
		}
		return guidePath, relativePath, nil
	}
	return "", "", fmt.Errorf("%w: ai tools guide not found in %s", os.ErrNotExist, strings.Join(candidates, ", "))
}

func resolveAIToolsGuidePathForWrite(relativePath string) (string, string, error) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		return "", "", err
	}
	target := strings.TrimSpace(relativePath)
	if target == "" {
		envPath, hasEnv, err := aiToolsGuidePathFromEnv()
		if err != nil {
			return "", "", err
		}
		if hasEnv {
			target = envPath
		} else {
			target = aiToolsGuideRelativePath
		}
	}
	normalized, ok := normalizeAIToolsGuideRelativePath(target)
	if !ok {
		return "", "", errors.New("invalid ai tools guide path")
	}
	return filepath.Join(repoRoot, filepath.FromSlash(normalized)), normalized, nil
}

func aiToolsGuidePathCandidates() ([]string, error) {
	candidates := []string{}
	if envPath, hasEnv, err := aiToolsGuidePathFromEnv(); err != nil {
		return nil, err
	} else if hasEnv {
		candidates = append(candidates, envPath)
	}
	candidates = append(
		candidates,
		aiToolsGuideRelativePath,
		aiToolsGuideLegacyRelativePath,
		aiToolsGuideLegacyV0RelativePath,
	)

	seen := map[string]struct{}{}
	unique := make([]string, 0, len(candidates))
	for _, path := range candidates {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	return unique, nil
}

func aiToolsGuidePathFromEnv() (string, bool, error) {
	raw := strings.TrimSpace(os.Getenv(aiToolsGuidePathEnv))
	if raw == "" {
		return "", false, nil
	}
	normalized, ok := normalizeAIToolsGuideRelativePath(raw)
	if !ok {
		return "", false, fmt.Errorf("%s must be a relative path without traversal", aiToolsGuidePathEnv)
	}
	return normalized, true, nil
}

func normalizeAIToolsGuideRelativePath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || filepath.IsAbs(trimmed) {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(trimmed))
	if clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", false
	}
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return clean, true
}

func findRepoRoot() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", err
	}
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	// Release bundles do not contain .git metadata. In that case, treat the
	// current working directory as the workspace root.
	return start, nil
}
