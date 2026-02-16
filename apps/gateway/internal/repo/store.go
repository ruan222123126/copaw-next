package repo

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"copaw-next/apps/gateway/internal/domain"
)

type ProviderSetting struct {
	APIKey       string            `json:"api_key"`
	BaseURL      string            `json:"base_url"`
	Enabled      *bool             `json:"enabled,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	TimeoutMS    int               `json:"timeout_ms,omitempty"`
	ModelAliases map[string]string `json:"model_aliases,omitempty"`
}

type State struct {
	Chats      map[string]domain.ChatSpec         `json:"chats"`
	Histories  map[string][]domain.RuntimeMessage `json:"histories"`
	CronJobs   map[string]domain.CronJobSpec      `json:"cron_jobs"`
	CronStates map[string]domain.CronJobState     `json:"cron_states"`
	Providers  map[string]ProviderSetting         `json:"providers"`
	ActiveLLM  domain.ModelSlotConfig             `json:"active_llm"`
	Envs       map[string]string                  `json:"envs"`
	Skills     map[string]domain.SkillSpec        `json:"skills"`
	Channels   domain.ChannelConfigMap            `json:"channels"`
}

type Store struct {
	mu           sync.RWMutex
	state        State
	stateFile    string
	workspaceDir string
}

func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	workspace := filepath.Join(dataDir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		stateFile:    filepath.Join(dataDir, "state.json"),
		workspaceDir: workspace,
		state:        defaultState(dataDir),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func defaultState(dataDir string) State {
	return State{
		Chats:      map[string]domain.ChatSpec{},
		Histories:  map[string][]domain.RuntimeMessage{},
		CronJobs:   map[string]domain.CronJobSpec{},
		CronStates: map[string]domain.CronJobState{},
		Providers: map[string]ProviderSetting{
			"demo":   defaultProviderSetting(),
			"openai": defaultProviderSetting(),
		},
		ActiveLLM: domain.ModelSlotConfig{ProviderID: "demo", Model: "demo-chat"},
		Envs:      map[string]string{},
		Skills:    map[string]domain.SkillSpec{},
		Channels: domain.ChannelConfigMap{
			"console": {
				"enabled":    true,
				"bot_prefix": "",
			},
			"webhook": {
				"enabled":         false,
				"url":             "",
				"method":          "POST",
				"headers":         map[string]interface{}{},
				"timeout_seconds": 5,
			},
		},
	}
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return s.saveLocked()
	}
	if err != nil {
		return err
	}
	var state State
	if err := json.Unmarshal(b, &state); err != nil {
		return err
	}
	if state.Chats == nil {
		state.Chats = map[string]domain.ChatSpec{}
	}
	if state.Histories == nil {
		state.Histories = map[string][]domain.RuntimeMessage{}
	}
	if state.CronJobs == nil {
		state.CronJobs = map[string]domain.CronJobSpec{}
	}
	if state.CronStates == nil {
		state.CronStates = map[string]domain.CronJobState{}
	}
	if state.Providers == nil {
		state.Providers = map[string]ProviderSetting{
			"demo": defaultProviderSetting(),
		}
	}
	if _, ok := state.Providers["demo"]; !ok {
		state.Providers["demo"] = defaultProviderSetting()
	}
	if _, ok := state.Providers["openai"]; !ok {
		state.Providers["openai"] = defaultProviderSetting()
	}
	for id, setting := range state.Providers {
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
		state.Providers[id] = setting
	}
	if state.ActiveLLM.ProviderID == "" {
		state.ActiveLLM.ProviderID = "demo"
	}
	if state.ActiveLLM.Model == "" {
		state.ActiveLLM.Model = "demo-chat"
	}
	if state.Envs == nil {
		state.Envs = map[string]string{}
	}
	if state.Skills == nil {
		state.Skills = map[string]domain.SkillSpec{}
	}
	if state.Channels == nil {
		state.Channels = domain.ChannelConfigMap{}
	}
	if _, ok := state.Channels["console"]; !ok {
		state.Channels["console"] = map[string]interface{}{
			"enabled":    true,
			"bot_prefix": "",
		}
	}
	if _, ok := state.Channels["webhook"]; !ok {
		state.Channels["webhook"] = map[string]interface{}{
			"enabled":         false,
			"url":             "",
			"method":          "POST",
			"headers":         map[string]interface{}{},
			"timeout_seconds": 5,
		}
	}
	s.state = state
	return nil
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.stateFile, b, 0o644)
}

func (s *Store) WorkspaceDir() string {
	return s.workspaceDir
}

func (s *Store) Read(fn func(state *State)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(&s.state)
}

func (s *Store) Write(fn func(state *State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.state); err != nil {
		return err
	}
	return s.saveLocked()
}

func defaultProviderSetting() ProviderSetting {
	enabled := true
	return ProviderSetting{
		Enabled:      &enabled,
		Headers:      map[string]string{},
		ModelAliases: map[string]string{},
	}
}
