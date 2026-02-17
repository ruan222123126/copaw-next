package main

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

const gatewayEnvFilePathEnv = "NEXTAI_ENV_FILE"

func loadEnvFile() (string, int, error) {
	path := strings.TrimSpace(os.Getenv(gatewayEnvFilePathEnv))
	if path == "" {
		path = ".env"
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, 0, nil
		}
		return path, 0, err
	}

	loaded := applyEnvFromContent(string(content))
	return path, loaded, nil
}

func applyEnvFromContent(content string) int {
	scanner := bufio.NewScanner(strings.NewReader(content))
	loaded := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		if key == "" {
			continue
		}
		value := normalizeEnvValue(line[idx+1:])

		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err == nil {
			loaded += 1
		}
	}
	return loaded
}

func normalizeEnvValue(raw string) string {
	text := strings.TrimSpace(raw)
	if len(text) >= 2 {
		if strings.HasPrefix(text, "\"") && strings.HasSuffix(text, "\"") {
			return strings.ReplaceAll(text[1:len(text)-1], `\n`, "\n")
		}
		if strings.HasPrefix(text, "'") && strings.HasSuffix(text, "'") {
			return text[1 : len(text)-1]
		}
	}
	return text
}
