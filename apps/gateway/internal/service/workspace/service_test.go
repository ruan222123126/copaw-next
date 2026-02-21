package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
)

func TestPutFileRejectsInvalidEnvKey(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Dependencies{})
	err := svc.PutFile(FileEnvs, []byte(`{"":"x"}`))
	validation := (*ValidationError)(nil)
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got=%v", err)
	}
	if validation.Code != "invalid_env_key" {
		t.Fatalf("unexpected validation code: %s", validation.Code)
	}
}

func TestPutFileActiveLLMProviderNotFound(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Dependencies{})
	err := svc.PutFile(FileActiveLLM, []byte(`{"provider_id":"ghost-provider","model":"ghost-model"}`))
	validation := (*ValidationError)(nil)
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got=%v", err)
	}
	if validation.Code != "provider_not_found" {
		t.Fatalf("unexpected validation code: %s", validation.Code)
	}
}

func TestDeleteConfigFileReturnsMethodNotAllowed(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Dependencies{})
	_, err := svc.DeleteFile(FileEnvs)
	if !errors.Is(err, ErrMethodNotAllowed) {
		t.Fatalf("expected ErrMethodNotAllowed, got=%v", err)
	}
}

func TestImportRejectsInvalidMode(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Dependencies{})
	err := svc.Import([]byte(`{"mode":"merge","payload":{"version":"v1"}}`))
	validation := (*ValidationError)(nil)
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got=%v", err)
	}
	if validation.Code != "invalid_import_mode" {
		t.Fatalf("unexpected validation code: %s", validation.Code)
	}
}

func TestGetTextFileNotFound(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Dependencies{
		IsTextFilePath: func(path string) bool {
			return path == "docs/AI/AGENTS.md"
		},
		ReadTextFile: func(string) (string, string, error) {
			return "", "", os.ErrNotExist
		},
	})
	_, err := svc.GetFile("docs/AI/AGENTS.md")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got=%v", err)
	}
}

func TestPutAndGetSkillFile(t *testing.T) {
	t.Parallel()

	svc := newTestService(t, Dependencies{})
	if err := svc.PutFile("skills/hello.json", []byte(`{"name":"hello","content":"hi","enabled":true}`)); err != nil {
		t.Fatalf("put skill failed: %v", err)
	}

	data, err := svc.GetFile("skills/hello.json")
	if err != nil {
		t.Fatalf("get skill failed: %v", err)
	}
	spec, ok := data.(domain.SkillSpec)
	if !ok {
		t.Fatalf("unexpected type: %T", data)
	}
	if spec.Name != "hello" || spec.Content != "hi" {
		t.Fatalf("unexpected skill spec: %+v", spec)
	}
	if filepath.Base(spec.Path) != "hello" {
		t.Fatalf("unexpected skill path: %s", spec.Path)
	}
}

func newTestService(t *testing.T, deps Dependencies) *Service {
	t.Helper()
	store, dataDir := newTestStore(t)

	if deps.Store == nil {
		deps.Store = store
	}
	if deps.DataDir == "" {
		deps.DataDir = dataDir
	}
	if deps.SupportedChannels == nil {
		deps.SupportedChannels = map[string]struct{}{
			"console": {},
			"webhook": {},
			"qq":      {},
		}
	}
	return NewService(deps)
}

func newTestStore(t *testing.T) (*repo.Store, string) {
	t.Helper()

	dir, err := os.MkdirTemp("", "nextai-workspace-service-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := repo.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return store, dir
}
