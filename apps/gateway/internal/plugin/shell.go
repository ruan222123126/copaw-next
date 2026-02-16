package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	shellToolEnableEnv      = "NEXTAI_ENABLE_SHELL_TOOL"
	shellToolDefaultTimeout = 20 * time.Second
	shellToolMaxTimeout     = 120 * time.Second
	shellToolMaxOutputBytes = 16 * 1024
)

var (
	ErrShellToolDisabled       = errors.New("shell_tool_disabled")
	ErrShellToolCommandMissing = errors.New("shell_tool_command_missing")
)

type ShellTool struct{}

func NewShellTool() *ShellTool {
	return &ShellTool{}
}

func (t *ShellTool) Name() string {
	return "shell"
}

func (t *ShellTool) Invoke(input map[string]interface{}) (map[string]interface{}, error) {
	if !envEnabled(os.Getenv(shellToolEnableEnv)) {
		return nil, ErrShellToolDisabled
	}
	command := strings.TrimSpace(stringValue(input["command"]))
	if command == "" {
		return nil, ErrShellToolCommandMissing
	}

	timeout := parseShellTimeout(input["timeout_seconds"])
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-lc", command)
	if cwd := strings.TrimSpace(stringValue(input["cwd"])); cwd != "" {
		cmd.Dir = cwd
	}

	outputBytes, err := cmd.CombinedOutput()
	output := truncateOutput(string(outputBytes), shellToolMaxOutputBytes)
	ok := err == nil
	exitCode := 0

	if err != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr):
			exitCode = exitErr.ExitCode()
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			exitCode = 124
		default:
			exitCode = -1
		}
	}

	text := formatShellText(command, ok, exitCode, output)
	return map[string]interface{}{
		"ok":        ok,
		"command":   command,
		"exit_code": exitCode,
		"output":    output,
		"text":      text,
	}, nil
}

func envEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseShellTimeout(raw interface{}) time.Duration {
	seconds := int64(shellToolDefaultTimeout / time.Second)
	switch value := raw.(type) {
	case float64:
		seconds = int64(value)
	case int:
		seconds = int64(value)
	case int64:
		seconds = value
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			seconds = parsed
		}
	}
	if seconds <= 0 {
		seconds = int64(shellToolDefaultTimeout / time.Second)
	}
	maxSeconds := int64(shellToolMaxTimeout / time.Second)
	if seconds > maxSeconds {
		seconds = maxSeconds
	}
	return time.Duration(seconds) * time.Second
}

func truncateOutput(raw string, maxBytes int) string {
	if maxBytes <= 0 || len(raw) <= maxBytes {
		return raw
	}
	return raw[:maxBytes] + "\n... (output truncated)"
}

func formatShellText(command string, ok bool, exitCode int, output string) string {
	trimmed := strings.TrimSpace(output)
	if ok {
		if trimmed == "" {
			return fmt.Sprintf("$ %s\n(command completed with no output)", command)
		}
		return fmt.Sprintf("$ %s\n%s", command, trimmed)
	}
	if trimmed == "" {
		return fmt.Sprintf("$ %s\n(command failed with exit code %d)", command, exitCode)
	}
	return fmt.Sprintf("$ %s\n(command failed with exit code %d)\n%s", command, exitCode, trimmed)
}

func stringValue(v interface{}) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return ""
	}
}
