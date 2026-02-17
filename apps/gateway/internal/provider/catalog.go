package provider

import (
	"sort"
	"strings"

	"nextai/apps/gateway/internal/domain"
)

const (
	AdapterDemo             = "demo"
	AdapterOpenAICompatible = "openai-compatible"
)

type ModelSpec struct {
	ID           string
	Name         string
	Status       string
	Capabilities domain.ModelCapabilities
	Limit        domain.ModelLimit
}

type ProviderSpec struct {
	ID                 string
	Name               string
	APIKeyPrefix       string
	AllowCustomBaseURL bool
	DefaultBaseURL     string
	Adapter            string
	Models             []ModelSpec
}

type ProviderTypeSpec struct {
	ID          string
	DisplayName string
}

var builtinProviders = map[string]ProviderSpec{
	"openai": {
		ID:                 "openai",
		Name:               "OPENAI",
		APIKeyPrefix:       "OPENAI_API_KEY",
		AllowCustomBaseURL: true,
		DefaultBaseURL:     "https://api.openai.com/v1",
		Adapter:            AdapterOpenAICompatible,
		Models: []ModelSpec{
			{
				ID:     "gpt-4o-mini",
				Name:   "GPT-4o Mini",
				Status: "active",
				Capabilities: domain.ModelCapabilities{
					Temperature: true,
					Reasoning:   true,
					Attachment:  true,
					ToolCall:    true,
					Input:       &domain.ModelModalities{Text: true, Image: true, Audio: true},
					Output:      &domain.ModelModalities{Text: true, Audio: true},
				},
				Limit: domain.ModelLimit{Context: 128000, Output: 16384},
			},
			{
				ID:     "gpt-4.1-mini",
				Name:   "GPT-4.1 Mini",
				Status: "active",
				Capabilities: domain.ModelCapabilities{
					Temperature: true,
					Reasoning:   true,
					Attachment:  true,
					ToolCall:    true,
					Input:       &domain.ModelModalities{Text: true, Image: true},
					Output:      &domain.ModelModalities{Text: true},
				},
				Limit: domain.ModelLimit{Context: 128000, Output: 16384},
			},
		},
	},
}

var providerTypes = []ProviderTypeSpec{
	{
		ID:          "openai",
		DisplayName: "openai",
	},
	{
		ID:          AdapterOpenAICompatible,
		DisplayName: "openai Compatible",
	},
}

func ListBuiltinProviderIDs() []string {
	out := make([]string, 0, len(builtinProviders))
	for id := range builtinProviders {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func ListProviderTypes() []ProviderTypeSpec {
	out := make([]ProviderTypeSpec, 0, len(providerTypes))
	for _, item := range providerTypes {
		out = append(out, item)
	}
	return out
}

func ResolveProvider(providerID string) ProviderSpec {
	id := normalizeProviderID(providerID)
	if spec, ok := builtinProviders[id]; ok {
		return cloneProviderSpec(spec)
	}
	return ProviderSpec{
		ID:                 id,
		Name:               strings.ToUpper(id),
		APIKeyPrefix:       EnvPrefix(id) + "_API_KEY",
		AllowCustomBaseURL: true,
		DefaultBaseURL:     "",
		Adapter:            AdapterOpenAICompatible,
		Models:             []ModelSpec{},
	}
}

func IsBuiltinProviderID(providerID string) bool {
	id := strings.ToLower(strings.TrimSpace(providerID))
	if id == "" {
		return false
	}
	_, ok := builtinProviders[id]
	return ok
}

func ResolveAdapter(providerID string) string {
	return ResolveProvider(providerID).Adapter
}

func ResolveModels(providerID string, aliases map[string]string) []domain.ModelInfo {
	spec := ResolveProvider(providerID)
	out := make([]domain.ModelInfo, 0, len(spec.Models)+len(aliases))
	seen := map[string]struct{}{}
	modelByID := map[string]domain.ModelInfo{}

	for _, model := range spec.Models {
		item := domain.ModelInfo{
			ID:           model.ID,
			Name:         model.Name,
			Status:       model.Status,
			Capabilities: cloneCapabilities(model.Capabilities),
			Limit:        cloneLimit(model.Limit),
		}
		out = append(out, item)
		modelByID[model.ID] = item
		seen[model.ID] = struct{}{}
	}

	keys := sortedAliasKeys(aliases)
	for _, alias := range keys {
		target := strings.TrimSpace(aliases[alias])
		if target == "" {
			continue
		}
		if _, exists := seen[alias]; exists {
			continue
		}

		base, ok := modelByID[target]
		if !ok {
			if len(spec.Models) > 0 {
				continue
			}
			item := domain.ModelInfo{
				ID:   alias,
				Name: alias,
			}
			if alias != target {
				item.AliasOf = target
			}
			out = append(out, item)
			seen[alias] = struct{}{}
			continue
		}
		base.ID = alias
		base.Name = alias
		base.AliasOf = target
		out = append(out, base)
		seen[alias] = struct{}{}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].AliasOf == "" && out[j].AliasOf != "" {
			return true
		}
		if out[i].AliasOf != "" && out[j].AliasOf == "" {
			return false
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func ResolveModelID(providerID, requestedModelID string, aliases map[string]string) (string, bool) {
	modelID := strings.TrimSpace(requestedModelID)
	if modelID == "" {
		return "", false
	}
	spec := ResolveProvider(providerID)
	modelSet := map[string]struct{}{}
	for _, model := range spec.Models {
		modelSet[model.ID] = struct{}{}
	}
	if _, ok := modelSet[modelID]; ok {
		return modelID, true
	}
	if target, ok := aliases[modelID]; ok {
		target = strings.TrimSpace(target)
		if target != "" {
			if _, exists := modelSet[target]; exists {
				return target, true
			}
			if len(spec.Models) == 0 {
				return target, true
			}
		}
	}
	// 允许自定义 provider 使用任意模型 ID（openai-compatible 场景）
	if len(spec.Models) == 0 {
		return modelID, true
	}
	return "", false
}

func DefaultModelID(providerID string) string {
	spec := ResolveProvider(providerID)
	if len(spec.Models) == 0 {
		return ""
	}
	return spec.Models[0].ID
}

func EnvPrefix(providerID string) string {
	prefix := strings.ToUpper(strings.TrimSpace(providerID))
	if prefix == "" {
		return "PROVIDER"
	}
	replacer := strings.NewReplacer("-", "_", ".", "_", " ", "_")
	return replacer.Replace(prefix)
}

func sortedAliasKeys(aliases map[string]string) []string {
	if len(aliases) == 0 {
		return nil
	}
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func normalizeProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func cloneProviderSpec(in ProviderSpec) ProviderSpec {
	out := in
	out.Models = make([]ModelSpec, 0, len(in.Models))
	for _, model := range in.Models {
		out.Models = append(out.Models, ModelSpec{
			ID:           model.ID,
			Name:         model.Name,
			Status:       model.Status,
			Capabilities: model.Capabilities,
			Limit:        model.Limit,
		})
	}
	return out
}

func cloneCapabilities(in domain.ModelCapabilities) *domain.ModelCapabilities {
	out := in
	if in.Input != nil {
		input := *in.Input
		out.Input = &input
	}
	if in.Output != nil {
		output := *in.Output
		out.Output = &output
	}
	return &out
}

func cloneLimit(in domain.ModelLimit) *domain.ModelLimit {
	out := in
	return &out
}
