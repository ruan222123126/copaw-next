package plugin

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	shellToolDefaultTimeout = 20 * time.Second
	shellToolMaxTimeout     = 120 * time.Second
	shellToolMaxOutputBytes = 16 * 1024
)

var (
	ErrShellToolCommandMissing      = errors.New("shell_tool_command_missing")
	ErrShellToolItemsInvalid        = errors.New("shell_tool_items_invalid")
	ErrShellToolExecutorUnavailable = errors.New("shell_tool_executor_unavailable")
)

type ShellTool struct{}

func NewShellTool() *ShellTool {
	return &ShellTool{}
}

func (t *ShellTool) Name() string {
	return "shell"
}

func (t *ShellTool) Invoke(input map[string]interface{}) (map[string]interface{}, error) {
	items, err := parseShellItems(input)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]interface{}, 0, len(items))
	allOK := true
	for _, item := range items {
		one, oneErr := t.invokeOne(item)
		if oneErr != nil {
			return nil, oneErr
		}
		if ok, _ := one["ok"].(bool); !ok {
			allOK = false
		}
		results = append(results, one)
	}
	if len(results) == 1 {
		return results[0], nil
	}
	texts := make([]string, 0, len(results))
	for _, item := range results {
		if text, ok := item["text"].(string); ok {
			texts = append(texts, text)
		}
	}
	return map[string]interface{}{
		"ok":      allOK,
		"count":   len(results),
		"results": results,
		"text":    strings.Join(texts, "\n"),
	}, nil
}

func (t *ShellTool) invokeOne(input map[string]interface{}) (map[string]interface{}, error) {
	command := strings.TrimSpace(stringValue(input["command"]))
	if command == "" {
		return nil, ErrShellToolCommandMissing
	}

	timeout := parseShellTimeout(input["timeout_seconds"])
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	program, baseArgs, resolveErr := resolveShellExecutor(runtime.GOOS, exec.LookPath)
	if resolveErr != nil {
		return nil, resolveErr
	}
	args := append(append([]string{}, baseArgs...), command)
	cmd := exec.CommandContext(ctx, program, args...)
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

func parseShellItems(input map[string]interface{}) ([]map[string]interface{}, error) {
	rawItems, ok := input["items"]
	if !ok || rawItems == nil {
		return nil, ErrShellToolItemsInvalid
	}
	entries, ok := rawItems.([]interface{})
	if !ok || len(entries) == 0 {
		return nil, ErrShellToolItemsInvalid
	}
	out := make([]map[string]interface{}, 0, len(entries))
	for _, item := range entries {
		entry, ok := item.(map[string]interface{})
		if !ok {
			return nil, ErrShellToolItemsInvalid
		}
		out = append(out, cloneShellInputMap(entry))
	}
	return out, nil
}

func cloneShellInputMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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

func resolveShellExecutor(goos string, lookPath func(file string) (string, error)) (string, []string, error) {
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		if hasExecutable(lookPath, "powershell", "powershell.exe") {
			return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command"}, nil
		}
		if hasExecutable(lookPath, "cmd", "cmd.exe") {
			return "cmd", []string{"/C"}, nil
		}
		return "", nil, ErrShellToolExecutorUnavailable
	}

	if hasExecutable(lookPath, "sh") {
		return "sh", []string{"-lc"}, nil
	}
	if hasExecutable(lookPath, "bash") {
		return "bash", []string{"-lc"}, nil
	}
	return "", nil, ErrShellToolExecutorUnavailable
}

func hasExecutable(lookPath func(file string) (string, error), candidates ...string) bool {
	for _, name := range candidates {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, err := lookPath(name); err == nil {
			return true
		}
	}
	return false
}
