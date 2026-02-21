package model

import (
	"errors"
	"os"
	"testing"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
)

func TestConfigureProviderRejectsInvalidTimeout(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	svc := NewService(Dependencies{Store: store})

	timeout := -1
	_, err := svc.ConfigureProvider(ConfigureProviderInput{
		ProviderID: "openai",
		TimeoutMS:  &timeout,
	})
	validation := (*ValidationError)(nil)
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got=%v", err)
	}
	if validation.Code != "invalid_provider_config" {
		t.Fatalf("unexpected validation code: %s", validation.Code)
	}
}

func TestSetActiveModelsMapsProviderErrors(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	svc := NewService(Dependencies{Store: store})

	_, err := svc.SetActiveModels(domain.ModelSlotConfig{
		ProviderID: "ghost",
		Model:      "foo",
	})
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("expected ErrProviderNotFound, got=%v", err)
	}

	disabled := false
	if writeErr := store.Write(func(st *repo.State) error {
		st.Providers["openai"] = repo.ProviderSetting{
			Enabled: &disabled,
		}
		return nil
	}); writeErr != nil {
		t.Fatalf("seed disabled provider failed: %v", writeErr)
	}
	_, err = svc.SetActiveModels(domain.ModelSlotConfig{
		ProviderID: "openai",
		Model:      "gpt-4o-mini",
	})
	if !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("expected ErrProviderDisabled, got=%v", err)
	}

	enabled := true
	if writeErr := store.Write(func(st *repo.State) error {
		st.Providers["openai"] = repo.ProviderSetting{
			Enabled: &enabled,
		}
		return nil
	}); writeErr != nil {
		t.Fatalf("seed enabled provider failed: %v", writeErr)
	}
	_, err = svc.SetActiveModels(domain.ModelSlotConfig{
		ProviderID: "openai",
		Model:      "model-not-found",
	})
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("expected ErrModelNotFound, got=%v", err)
	}
}

func TestDeleteProviderClearsActiveModel(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	if err := store.Write(func(st *repo.State) error {
		st.ActiveLLM = domain.ModelSlotConfig{
			ProviderID: "openai",
			Model:      "gpt-4o-mini",
		}
		return nil
	}); err != nil {
		t.Fatalf("seed active model failed: %v", err)
	}

	svc := NewService(Dependencies{Store: store})
	deleted, err := svc.DeleteProvider("openai")
	if err != nil {
		t.Fatalf("delete provider failed: %v", err)
	}
	if !deleted {
		t.Fatalf("expected deleted=true")
	}

	active, err := svc.GetActiveModels()
	if err != nil {
		t.Fatalf("get active models failed: %v", err)
	}
	if active.ActiveLLM.ProviderID != "" || active.ActiveLLM.Model != "" {
		t.Fatalf("expected active llm cleared, got=%+v", active.ActiveLLM)
	}
}

func newTestStore(t *testing.T) *repo.Store {
	t.Helper()

	dir, err := os.MkdirTemp("", "nextai-model-service-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := repo.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
