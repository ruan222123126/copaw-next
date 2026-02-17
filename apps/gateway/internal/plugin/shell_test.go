package plugin

import (
	"errors"
	"os/exec"
	"testing"
)

func TestResolveShellExecutorWindowsPrefersPowerShell(t *testing.T) {
	program, args, err := resolveShellExecutor("windows", fakeLookPath(map[string]bool{
		"powershell": true,
		"cmd":        true,
	}))
	if err != nil {
		t.Fatalf("resolve executor failed: %v", err)
	}
	if program != "powershell" {
		t.Fatalf("expected powershell, got=%q", program)
	}
	if len(args) != 3 || args[0] != "-NoProfile" || args[1] != "-NonInteractive" || args[2] != "-Command" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolveShellExecutorWindowsFallsBackToCmd(t *testing.T) {
	program, args, err := resolveShellExecutor("windows", fakeLookPath(map[string]bool{
		"cmd": true,
	}))
	if err != nil {
		t.Fatalf("resolve executor failed: %v", err)
	}
	if program != "cmd" {
		t.Fatalf("expected cmd, got=%q", program)
	}
	if len(args) != 1 || args[0] != "/C" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolveShellExecutorUnixUsesSh(t *testing.T) {
	program, args, err := resolveShellExecutor("linux", fakeLookPath(map[string]bool{
		"sh": true,
	}))
	if err != nil {
		t.Fatalf("resolve executor failed: %v", err)
	}
	if program != "sh" {
		t.Fatalf("expected sh, got=%q", program)
	}
	if len(args) != 1 || args[0] != "-lc" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolveShellExecutorUnixFallsBackToBash(t *testing.T) {
	program, args, err := resolveShellExecutor("darwin", fakeLookPath(map[string]bool{
		"bash": true,
	}))
	if err != nil {
		t.Fatalf("resolve executor failed: %v", err)
	}
	if program != "bash" {
		t.Fatalf("expected bash, got=%q", program)
	}
	if len(args) != 1 || args[0] != "-lc" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolveShellExecutorReturnsUnavailableWhenNoneFound(t *testing.T) {
	_, _, err := resolveShellExecutor("windows", fakeLookPath(map[string]bool{}))
	if !errors.Is(err, ErrShellToolExecutorUnavailable) {
		t.Fatalf("expected ErrShellToolExecutorUnavailable, got=%v", err)
	}
}

func fakeLookPath(available map[string]bool) func(file string) (string, error) {
	return func(file string) (string, error) {
		if available[file] {
			return file, nil
		}
		return "", exec.ErrNotFound
	}
}
