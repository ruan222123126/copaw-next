package main

import (
	"os"
	"path/filepath"
	"testing"
)

func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	previous, existed := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, previous)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestLoadEnvFileLoadsDefaultDotEnvWithoutOverwritingExisting(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")
	content := "" +
		"NEXTAI_PORT=19088\n" +
		"export NEXTAI_HOST=0.0.0.0\n" +
		"QUOTED_VALUE=\"hello world\"\n" +
		"SINGLE_QUOTED='foo bar'\n" +
		"EXISTING_VALUE=from-file\n"
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write env file failed: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	t.Setenv(gatewayEnvFilePathEnv, "")
	unsetEnvForTest(t, "NEXTAI_PORT")
	unsetEnvForTest(t, "NEXTAI_HOST")
	unsetEnvForTest(t, "QUOTED_VALUE")
	unsetEnvForTest(t, "SINGLE_QUOTED")
	t.Setenv("EXISTING_VALUE", "from-env")

	path, loaded, loadErr := loadEnvFile()
	if loadErr != nil {
		t.Fatalf("loadEnvFile returned error: %v", loadErr)
	}
	if path != ".env" {
		t.Fatalf("expected default path .env, got %s", path)
	}
	if loaded != 4 {
		t.Fatalf("expected 4 loaded keys, got %d", loaded)
	}
	if got := os.Getenv("NEXTAI_PORT"); got != "19088" {
		t.Fatalf("unexpected NEXTAI_PORT: %s", got)
	}
	if got := os.Getenv("NEXTAI_HOST"); got != "0.0.0.0" {
		t.Fatalf("unexpected NEXTAI_HOST: %s", got)
	}
	if got := os.Getenv("QUOTED_VALUE"); got != "hello world" {
		t.Fatalf("unexpected QUOTED_VALUE: %s", got)
	}
	if got := os.Getenv("SINGLE_QUOTED"); got != "foo bar" {
		t.Fatalf("unexpected SINGLE_QUOTED: %s", got)
	}
	if got := os.Getenv("EXISTING_VALUE"); got != "from-env" {
		t.Fatalf("existing env should not be overwritten, got %s", got)
	}
}

func TestLoadEnvFileUsesExplicitPath(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "gateway.release.env")
	if err := os.WriteFile(envPath, []byte("NEXTAI_DATA_DIR=/tmp/nextai-data\n"), 0o644); err != nil {
		t.Fatalf("write env file failed: %v", err)
	}

	t.Setenv(gatewayEnvFilePathEnv, envPath)
	unsetEnvForTest(t, "NEXTAI_DATA_DIR")

	path, loaded, loadErr := loadEnvFile()
	if loadErr != nil {
		t.Fatalf("loadEnvFile returned error: %v", loadErr)
	}
	if path != envPath {
		t.Fatalf("expected explicit path %s, got %s", envPath, path)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 loaded key, got %d", loaded)
	}
	if got := os.Getenv("NEXTAI_DATA_DIR"); got != "/tmp/nextai-data" {
		t.Fatalf("unexpected NEXTAI_DATA_DIR: %s", got)
	}
}

func TestLoadEnvFileMissingIsNotAnError(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, "missing.env")

	t.Setenv(gatewayEnvFilePathEnv, envPath)
	path, loaded, loadErr := loadEnvFile()
	if loadErr != nil {
		t.Fatalf("loadEnvFile returned error for missing file: %v", loadErr)
	}
	if path != envPath {
		t.Fatalf("expected missing path %s, got %s", envPath, path)
	}
	if loaded != 0 {
		t.Fatalf("expected 0 loaded keys, got %d", loaded)
	}
}
