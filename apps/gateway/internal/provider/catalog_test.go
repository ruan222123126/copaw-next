package provider

import "testing"

func TestResolveModelsIncludesCustomAliasTargetsForCustomProvider(t *testing.T) {
	models := ResolveModels("custom-openai", map[string]string{
		"fast":     "gpt-4o-mini",
		"my-model": "my-model",
	})
	if len(models) != 2 {
		t.Fatalf("expected 2 custom models, got=%d", len(models))
	}

	byID := map[string]bool{}
	for _, model := range models {
		byID[model.ID] = true
		if model.ID == "fast" && model.AliasOf != "gpt-4o-mini" {
			t.Fatalf("expected alias_of for fast model, got=%q", model.AliasOf)
		}
		if model.ID == "my-model" && model.AliasOf != "" {
			t.Fatalf("expected my-model not to set alias_of, got=%q", model.AliasOf)
		}
	}
	if !byID["fast"] || !byID["my-model"] {
		t.Fatalf("expected models to include fast and my-model, got=%v", byID)
	}
}

func TestResolveModelIDUsesCustomAliasTargetForCustomProvider(t *testing.T) {
	got, ok := ResolveModelID("custom-openai", "fast", map[string]string{
		"fast": "gpt-4o-mini",
	})
	if !ok {
		t.Fatalf("expected alias resolution ok=true")
	}
	if got != "gpt-4o-mini" {
		t.Fatalf("expected resolved model gpt-4o-mini, got=%q", got)
	}
}

func TestListProviderTypes(t *testing.T) {
	types := ListProviderTypes()
	if len(types) < 2 {
		t.Fatalf("expected at least 2 provider types, got=%d", len(types))
	}
	if types[0].ID != "openai" || types[0].DisplayName != "openai" {
		t.Fatalf("unexpected first provider type: %+v", types[0])
	}
	if types[1].ID != AdapterOpenAICompatible || types[1].DisplayName != "openai Compatible" {
		t.Fatalf("unexpected second provider type: %+v", types[1])
	}
}
