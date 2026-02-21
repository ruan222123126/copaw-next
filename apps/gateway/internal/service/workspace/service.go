package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/ports"
)

const (
	FileEnvs      = "config/envs.json"
	FileChannels  = "config/channels.json"
	FileModels    = "config/models.json"
	FileActiveLLM = "config/active-llm.json"
)

var ErrNotFound = errors.New("workspace_file_not_found")
var ErrMethodNotAllowed = errors.New("workspace_method_not_allowed")

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type FileError struct {
	Err error
}

func (e *FileError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *FileError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type FileEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Size int    `json:"size"`
}

type FileListResponse struct {
	Files []FileEntry `json:"files"`
}

type ExportModels struct {
	Providers map[string]repo.ProviderSetting `json:"providers"`
	ActiveLLM domain.ModelSlotConfig          `json:"active_llm"`
}

type ExportConfig struct {
	Envs     map[string]string       `json:"envs"`
	Channels domain.ChannelConfigMap `json:"channels"`
	Models   ExportModels            `json:"models"`
}

type ExportPayload struct {
	Version string                      `json:"version"`
	Skills  map[string]domain.SkillSpec `json:"skills"`
	Config  ExportConfig                `json:"config"`
}

type ImportRequest struct {
	Mode    string        `json:"mode"`
	Payload ExportPayload `json:"payload"`
}

type Dependencies struct {
	Store             ports.StateStore
	DataDir           string
	SupportedChannels map[string]struct{}
	IsTextFilePath    func(string) bool
	ReadTextFile      func(string) (string, string, error)
	WriteTextFile     func(string, string) error
	CollectTextFiles  func() []FileEntry
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	normalizedChannels := map[string]struct{}{}
	for name := range deps.SupportedChannels {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		normalizedChannels[key] = struct{}{}
	}
	deps.SupportedChannels = normalizedChannels
	if deps.CollectTextFiles == nil {
		deps.CollectTextFiles = func() []FileEntry { return nil }
	}
	return &Service{deps: deps}
}

func (s *Service) ListFiles() (FileListResponse, error) {
	if err := s.validateStore(); err != nil {
		return FileListResponse{}, err
	}

	out := FileListResponse{Files: []FileEntry{}}
	s.deps.Store.Read(func(st *repo.State) {
		out.Files = collectWorkspaceFiles(st)
	})
	out.Files = mergeWorkspaceFileEntries(out.Files, s.deps.CollectTextFiles()...)
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	return out, nil
}

func (s *Service) GetFile(filePath string) (interface{}, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	if s.isTextFilePath(filePath) {
		if s.deps.ReadTextFile == nil {
			return nil, errors.New("workspace text file reader is not configured")
		}
		resolvedPath, content, err := s.deps.ReadTextFile(filePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, ErrNotFound
			}
			return nil, &FileError{Err: err}
		}
		if filePath != resolvedPath {
			return nil, ErrNotFound
		}
		return map[string]string{"content": content}, nil
	}

	var data interface{}
	found := false
	s.deps.Store.Read(func(st *repo.State) {
		data, found = readWorkspaceFileData(st, filePath)
	})
	if !found {
		return nil, ErrNotFound
	}
	return data, nil
}

func (s *Service) PutFile(filePath string, body []byte) error {
	if err := s.validateStore(); err != nil {
		return err
	}

	if s.isTextFilePath(filePath) {
		if s.deps.WriteTextFile == nil {
			return errors.New("workspace text file writer is not configured")
		}
		var req struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return &ValidationError{
				Code:    "invalid_json",
				Message: "invalid request body",
			}
		}
		if strings.TrimSpace(req.Content) == "" {
			return &ValidationError{
				Code:    "invalid_ai_tools_guide",
				Message: "content is required",
			}
		}
		if err := s.deps.WriteTextFile(filePath, req.Content); err != nil {
			return &FileError{Err: err}
		}
		return nil
	}

	switch filePath {
	case FileEnvs:
		var req map[string]string
		if err := json.Unmarshal(body, &req); err != nil {
			return &ValidationError{
				Code:    "invalid_json",
				Message: "invalid request body",
			}
		}
		envs, err := normalizeWorkspaceEnvs(req)
		if err != nil {
			return &ValidationError{
				Code:    "invalid_env_key",
				Message: err.Error(),
			}
		}
		return s.deps.Store.Write(func(st *repo.State) error {
			st.Envs = envs
			return nil
		})
	case FileChannels:
		var req domain.ChannelConfigMap
		if err := json.Unmarshal(body, &req); err != nil {
			return &ValidationError{
				Code:    "invalid_json",
				Message: "invalid request body",
			}
		}
		channels, err := s.normalizeWorkspaceChannels(req)
		if err != nil {
			return &ValidationError{
				Code:    "channel_not_supported",
				Message: err.Error(),
			}
		}
		return s.deps.Store.Write(func(st *repo.State) error {
			st.Channels = channels
			return nil
		})
	case FileModels:
		var req map[string]repo.ProviderSetting
		if err := json.Unmarshal(body, &req); err != nil {
			return &ValidationError{
				Code:    "invalid_json",
				Message: "invalid request body",
			}
		}
		providers, err := normalizeWorkspaceProviders(req)
		if err != nil {
			return &ValidationError{
				Code:    "invalid_provider_config",
				Message: err.Error(),
			}
		}
		return s.deps.Store.Write(func(st *repo.State) error {
			st.Providers = providers
			if st.ActiveLLM.ProviderID != "" {
				if _, ok := findProviderSettingByID(st, st.ActiveLLM.ProviderID); !ok {
					st.ActiveLLM = domain.ModelSlotConfig{}
				}
			}
			return nil
		})
	case FileActiveLLM:
		var req domain.ModelSlotConfig
		if err := json.Unmarshal(body, &req); err != nil {
			return &ValidationError{
				Code:    "invalid_json",
				Message: "invalid request body",
			}
		}
		req.ProviderID = normalizeProviderID(req.ProviderID)
		req.Model = strings.TrimSpace(req.Model)
		if (req.ProviderID == "") != (req.Model == "") {
			return &ValidationError{
				Code:    "invalid_model_slot",
				Message: "provider_id and model must be set together",
			}
		}
		if err := s.deps.Store.Write(func(st *repo.State) error {
			if req.ProviderID == "" {
				st.ActiveLLM = domain.ModelSlotConfig{}
				return nil
			}
			if _, ok := findProviderSettingByID(st, req.ProviderID); !ok {
				return &ValidationError{
					Code:    "provider_not_found",
					Message: "provider not found",
				}
			}
			st.ActiveLLM = req
			return nil
		}); err != nil {
			return err
		}
		return nil
	}

	name, ok := workspaceSkillNameFromPath(filePath)
	if !ok {
		return &ValidationError{
			Code:    "invalid_path",
			Message: "invalid workspace file path",
		}
	}
	var req domain.SkillSpec
	if err := json.Unmarshal(body, &req); err != nil {
		return &ValidationError{
			Code:    "invalid_json",
			Message: "invalid request body",
		}
	}
	if req.Name != "" && strings.TrimSpace(req.Name) != name {
		return &ValidationError{
			Code:    "invalid_skill",
			Message: "skill name in body must match file path",
		}
	}
	if strings.TrimSpace(req.Content) == "" {
		return &ValidationError{
			Code:    "invalid_skill",
			Message: "content is required",
		}
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "customized"
	}
	spec := domain.SkillSpec{
		Name:       name,
		Content:    req.Content,
		Source:     source,
		Path:       filepath.Join(s.deps.DataDir, "skills", name),
		References: safeMap(req.References),
		Scripts:    safeMap(req.Scripts),
		Enabled:    req.Enabled,
	}
	return s.deps.Store.Write(func(st *repo.State) error {
		st.Skills[name] = spec
		return nil
	})
}

func (s *Service) DeleteFile(filePath string) (bool, error) {
	if err := s.validateStore(); err != nil {
		return false, err
	}

	if s.isWorkspaceConfigFile(filePath) {
		return false, ErrMethodNotAllowed
	}
	name, ok := workspaceSkillNameFromPath(filePath)
	if !ok {
		return false, &ValidationError{
			Code:    "invalid_path",
			Message: "invalid workspace file path",
		}
	}

	deleted := false
	if err := s.deps.Store.Write(func(st *repo.State) error {
		if _, ok := st.Skills[name]; ok {
			delete(st.Skills, name)
			deleted = true
		}
		return nil
	}); err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *Service) Export() (ExportPayload, error) {
	if err := s.validateStore(); err != nil {
		return ExportPayload{}, err
	}

	out := ExportPayload{
		Version: "v1",
		Skills:  map[string]domain.SkillSpec{},
		Config: ExportConfig{
			Envs:     map[string]string{},
			Channels: domain.ChannelConfigMap{},
			Models: ExportModels{
				Providers: map[string]repo.ProviderSetting{},
				ActiveLLM: domain.ModelSlotConfig{},
			},
		},
	}
	s.deps.Store.Read(func(st *repo.State) {
		out.Skills = cloneWorkspaceSkills(st.Skills)
		out.Config.Envs = cloneWorkspaceEnvs(st.Envs)
		out.Config.Channels = cloneWorkspaceChannels(st.Channels)
		out.Config.Models.Providers = cloneWorkspaceProviders(st.Providers)
		out.Config.Models.ActiveLLM = st.ActiveLLM
	})
	return out, nil
}

func (s *Service) Import(body []byte) error {
	if err := s.validateStore(); err != nil {
		return err
	}

	var req ImportRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return &ValidationError{
			Code:    "invalid_json",
			Message: "invalid request body",
		}
	}
	if strings.ToLower(strings.TrimSpace(req.Mode)) != "replace" {
		return &ValidationError{
			Code:    "invalid_import_mode",
			Message: "mode must be replace",
		}
	}

	skills, err := normalizeWorkspaceSkills(req.Payload.Skills, s.deps.DataDir)
	if err != nil {
		return &ValidationError{
			Code:    "invalid_skill",
			Message: err.Error(),
		}
	}
	envs, err := normalizeWorkspaceEnvs(req.Payload.Config.Envs)
	if err != nil {
		return &ValidationError{
			Code:    "invalid_env_key",
			Message: err.Error(),
		}
	}
	channels, err := s.normalizeWorkspaceChannels(req.Payload.Config.Channels)
	if err != nil {
		return &ValidationError{
			Code:    "channel_not_supported",
			Message: err.Error(),
		}
	}
	providers, err := normalizeWorkspaceProviders(req.Payload.Config.Models.Providers)
	if err != nil {
		return &ValidationError{
			Code:    "invalid_provider_config",
			Message: err.Error(),
		}
	}
	active, err := normalizeWorkspaceActiveLLM(req.Payload.Config.Models.ActiveLLM, providers)
	if err != nil {
		return &ValidationError{
			Code:    "invalid_model_slot",
			Message: err.Error(),
		}
	}

	return s.deps.Store.Write(func(st *repo.State) error {
		st.Skills = skills
		st.Envs = envs
		st.Channels = channels
		st.Providers = providers
		st.ActiveLLM = active
		return nil
	})
}

func (s *Service) validateStore() error {
	if s == nil || s.deps.Store == nil {
		return errors.New("workspace state store is required")
	}
	return nil
}

func (s *Service) isTextFilePath(filePath string) bool {
	if s == nil || s.deps.IsTextFilePath == nil {
		return false
	}
	return s.deps.IsTextFilePath(filePath)
}

func (s *Service) isWorkspaceConfigFile(filePath string) bool {
	return filePath == FileEnvs ||
		filePath == FileChannels ||
		filePath == FileModels ||
		filePath == FileActiveLLM ||
		s.isTextFilePath(filePath)
}

func (s *Service) normalizeWorkspaceChannels(in domain.ChannelConfigMap) (domain.ChannelConfigMap, error) {
	out := domain.ChannelConfigMap{}
	for name, cfg := range in {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return nil, errors.New("channel name cannot be empty")
		}
		if _, ok := s.deps.SupportedChannels[normalized]; !ok {
			return nil, fmt.Errorf("channel %q is not supported", name)
		}
		out[normalized] = cloneWorkspaceJSONMap(cfg)
	}
	return out, nil
}

func collectWorkspaceFiles(st *repo.State) []FileEntry {
	files := []FileEntry{
		{Path: FileEnvs, Kind: "config", Size: jsonSize(cloneWorkspaceEnvs(st.Envs))},
		{Path: FileChannels, Kind: "config", Size: jsonSize(cloneWorkspaceChannels(st.Channels))},
		{Path: FileModels, Kind: "config", Size: jsonSize(cloneWorkspaceProviders(st.Providers))},
		{Path: FileActiveLLM, Kind: "config", Size: jsonSize(st.ActiveLLM)},
	}
	for name, spec := range st.Skills {
		files = append(files, FileEntry{
			Path: workspaceSkillFilePath(name),
			Kind: "skill",
			Size: jsonSize(cloneWorkspaceSkill(spec)),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func mergeWorkspaceFileEntries(base []FileEntry, extra ...FileEntry) []FileEntry {
	out := make([]FileEntry, 0, len(base)+len(extra))
	indexByPath := map[string]int{}
	entries := append(append([]FileEntry{}, base...), extra...)
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
	case FileEnvs:
		return cloneWorkspaceEnvs(st.Envs), true
	case FileChannels:
		return cloneWorkspaceChannels(st.Channels), true
	case FileModels:
		return cloneWorkspaceProviders(st.Providers), true
	case FileActiveLLM:
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

func normalizeProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
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

func safeMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	return v
}
