package model

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/provider"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/ports"
)

var ErrProviderNotFound = errors.New("provider_not_found")
var ErrProviderDisabled = errors.New("provider_disabled")
var ErrModelNotFound = errors.New("model_not_found")

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

type Dependencies struct {
	Store     ports.StateStore
	EnvLookup func(string) string
}

type Service struct {
	deps Dependencies
}

type ConfigureProviderInput struct {
	ProviderID   string
	APIKey       *string
	BaseURL      *string
	DisplayName  *string
	Enabled      *bool
	Headers      *map[string]string
	TimeoutMS    *int
	ModelAliases *map[string]string
}

func NewService(deps Dependencies) *Service {
	if deps.EnvLookup == nil {
		deps.EnvLookup = os.Getenv
	}
	return &Service{deps: deps}
}

func (s *Service) ListProviders() ([]domain.ProviderInfo, error) {
	providers, _, _, err := s.collectProviderCatalog()
	if err != nil {
		return nil, err
	}
	return providers, nil
}

func (s *Service) GetCatalog() (domain.ModelCatalogInfo, error) {
	providers, defaults, active, err := s.collectProviderCatalog()
	if err != nil {
		return domain.ModelCatalogInfo{}, err
	}
	providerTypes := provider.ListProviderTypes()
	typeOut := make([]domain.ProviderTypeInfo, 0, len(providerTypes))
	for _, item := range providerTypes {
		typeOut = append(typeOut, domain.ProviderTypeInfo{
			ID:          item.ID,
			DisplayName: item.DisplayName,
		})
	}
	return domain.ModelCatalogInfo{
		Providers:     providers,
		Defaults:      defaults,
		ActiveLLM:     active,
		ProviderTypes: typeOut,
	}, nil
}

func (s *Service) ConfigureProvider(input ConfigureProviderInput) (domain.ProviderInfo, error) {
	if err := s.validateStore(); err != nil {
		return domain.ProviderInfo{}, err
	}

	providerID := normalizeProviderID(input.ProviderID)
	if providerID == "" {
		return domain.ProviderInfo{}, &ValidationError{
			Code:    "invalid_provider_id",
			Message: "provider_id is required",
		}
	}
	if input.TimeoutMS != nil && *input.TimeoutMS < 0 {
		return domain.ProviderInfo{}, &ValidationError{
			Code:    "invalid_provider_config",
			Message: "timeout_ms must be >= 0",
		}
	}

	sanitizedAliases, aliasErr := sanitizeModelAliases(input.ModelAliases)
	if aliasErr != nil {
		return domain.ProviderInfo{}, &ValidationError{
			Code:    "invalid_provider_config",
			Message: aliasErr.Error(),
		}
	}

	var out domain.ProviderInfo
	if err := s.deps.Store.Write(func(st *repo.State) error {
		setting := getProviderSettingByID(st, providerID)
		normalizeProviderSetting(&setting)
		if input.APIKey != nil {
			setting.APIKey = strings.TrimSpace(*input.APIKey)
		}
		if input.BaseURL != nil {
			setting.BaseURL = strings.TrimSpace(*input.BaseURL)
		}
		if input.DisplayName != nil {
			setting.DisplayName = strings.TrimSpace(*input.DisplayName)
		}
		if input.Enabled != nil {
			enabled := *input.Enabled
			setting.Enabled = &enabled
		}
		if input.Headers != nil {
			setting.Headers = sanitizeStringMap(*input.Headers)
		}
		if input.TimeoutMS != nil {
			setting.TimeoutMS = *input.TimeoutMS
		}
		if input.ModelAliases != nil {
			setting.ModelAliases = sanitizedAliases
		}
		st.Providers[providerID] = setting
		out = s.buildProviderInfo(providerID, setting)
		return nil
	}); err != nil {
		return domain.ProviderInfo{}, err
	}
	return out, nil
}

func (s *Service) DeleteProvider(providerID string) (bool, error) {
	if err := s.validateStore(); err != nil {
		return false, err
	}

	providerID = normalizeProviderID(providerID)
	if providerID == "" {
		return false, &ValidationError{
			Code:    "invalid_provider_id",
			Message: "provider_id is required",
		}
	}

	deleted := false
	if err := s.deps.Store.Write(func(st *repo.State) error {
		for key := range st.Providers {
			if normalizeProviderID(key) == providerID {
				delete(st.Providers, key)
				deleted = true
			}
		}
		if deleted && normalizeProviderID(st.ActiveLLM.ProviderID) == providerID {
			st.ActiveLLM = domain.ModelSlotConfig{}
		}
		return nil
	}); err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *Service) GetActiveModels() (domain.ActiveModelsInfo, error) {
	if err := s.validateStore(); err != nil {
		return domain.ActiveModelsInfo{}, err
	}

	out := domain.ActiveModelsInfo{}
	s.deps.Store.Read(func(st *repo.State) {
		out = domain.ActiveModelsInfo{ActiveLLM: st.ActiveLLM}
	})
	return out, nil
}

func (s *Service) SetActiveModels(body domain.ModelSlotConfig) (domain.ActiveModelsInfo, error) {
	if err := s.validateStore(); err != nil {
		return domain.ActiveModelsInfo{}, err
	}

	body.ProviderID = normalizeProviderID(body.ProviderID)
	body.Model = strings.TrimSpace(body.Model)
	if body.ProviderID == "" || body.Model == "" {
		return domain.ActiveModelsInfo{}, &ValidationError{
			Code:    "invalid_model_slot",
			Message: "provider_id and model are required",
		}
	}

	var out domain.ModelSlotConfig
	if err := s.deps.Store.Write(func(st *repo.State) error {
		setting, ok := findProviderSettingByID(st, body.ProviderID)
		if !ok {
			return ErrProviderNotFound
		}
		normalizeProviderSetting(&setting)
		if !providerEnabled(setting) {
			return ErrProviderDisabled
		}
		resolvedModel, ok := provider.ResolveModelID(body.ProviderID, body.Model, setting.ModelAliases)
		if !ok {
			return ErrModelNotFound
		}
		out = domain.ModelSlotConfig{
			ProviderID: body.ProviderID,
			Model:      resolvedModel,
		}
		st.ActiveLLM = out
		return nil
	}); err != nil {
		return domain.ActiveModelsInfo{}, err
	}
	return domain.ActiveModelsInfo{ActiveLLM: out}, nil
}

func (s *Service) collectProviderCatalog() ([]domain.ProviderInfo, map[string]string, domain.ModelSlotConfig, error) {
	if err := s.validateStore(); err != nil {
		return nil, nil, domain.ModelSlotConfig{}, err
	}

	out := make([]domain.ProviderInfo, 0)
	defaults := map[string]string{}
	active := domain.ModelSlotConfig{}

	s.deps.Store.Read(func(st *repo.State) {
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
			out = append(out, s.buildProviderInfo(id, setting))
			defaults[id] = provider.DefaultModelID(id)
		}
	})
	return out, defaults, active, nil
}

func (s *Service) buildProviderInfo(providerID string, setting repo.ProviderSetting) domain.ProviderInfo {
	normalizeProviderSetting(&setting)
	spec := provider.ResolveProvider(providerID)
	apiKey := s.resolveProviderAPIKey(providerID, setting)
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
		CurrentBaseURL:     s.resolveProviderBaseURL(providerID, setting),
	}
}

func (s *Service) resolveProviderAPIKey(providerID string, setting repo.ProviderSetting) string {
	if key := strings.TrimSpace(setting.APIKey); key != "" {
		return key
	}
	return strings.TrimSpace(s.deps.EnvLookup(providerEnvPrefix(providerID) + "_API_KEY"))
}

func (s *Service) resolveProviderBaseURL(providerID string, setting repo.ProviderSetting) string {
	if baseURL := strings.TrimSpace(setting.BaseURL); baseURL != "" {
		return baseURL
	}
	if envBaseURL := strings.TrimSpace(s.deps.EnvLookup(providerEnvPrefix(providerID) + "_BASE_URL")); envBaseURL != "" {
		return envBaseURL
	}
	return provider.ResolveProvider(providerID).DefaultBaseURL
}

func (s *Service) validateStore() error {
	if s == nil || s.deps.Store == nil {
		return errors.New("model state store is required")
	}
	return nil
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

func maskKey(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-3:]
}

func (e *ValidationError) String() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Code) == "" {
		return e.Message
	}
	if strings.TrimSpace(e.Message) == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
