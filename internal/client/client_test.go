package client

import (
	"assistdemo/internal/wire"
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFilenameHandoff(t *testing.T) {
	b := wire.Bootstrap{Version: 2, Base: "http://127.0.0.1:18777", Ticket: wire.Token()}
	for _, s := range []string{"assist--" + b.Encode() + ".exe", "assist--" + b.Encode() + " (1).exe", "assist--" + b.Encode(), "assist--" + b.Encode() + " (1)", "assist://join/" + b.Encode()} {
		out, e := ParseInput(s)
		if e != nil || out != b {
			t.Fatal(s, e)
		}
	}
	for _, s := range []string{"assist.exe", "assist://join/invalid", "assist://join/" + b.Encode() + `" --uninstall`} {
		if _, e := ParseInput(s); e == nil {
			t.Fatal("accepted malformed input", s)
		}
	}
}
func TestShellValidation(t *testing.T) {
	r := ExecuteShell(context.Background(), wire.Message{Shell: "cmd", Command: "date", Timeout: 30}, func(string, string) {})
	if r.ExitCode != -1 {
		t.Fatal(r)
	}
}
func TestBashOutputAndExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash fixture")
	}
	out, stderr := "", ""
	r := ExecuteShell(context.Background(), wire.Message{Shell: "bash", Command: "printf 'hello 中文\\n'; printf 'failure\\n' >&2; exit 7", Timeout: 5}, func(s, v string) {
		if s == "stdout" {
			out += v
		} else {
			stderr += v
		}
	})
	if r.ExitCode != 7 || r.Status != "failed" || !strings.Contains(out, "中文") || !strings.Contains(stderr, "failure") {
		t.Fatal(r, out, stderr)
	}
}
func TestBashTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash fixture")
	}
	start := time.Now()
	r := ExecuteShell(context.Background(), wire.Message{Shell: "bash", Command: "sleep 20", Timeout: 1}, func(string, string) {})
	if r.Status != "timed_out" || time.Since(start) > 4*time.Second {
		t.Fatal(r)
	}
}
func TestBashCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash fixture")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	r := ExecuteShell(ctx, wire.Message{Shell: "bash", Command: "sleep 20", Timeout: 30}, func(string, string) {})
	if r.Status != "cancelled" {
		t.Fatal(r)
	}
}
func TestCwd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash fixture")
	}
	dir := t.TempDir()
	out := ""
	r := ExecuteShell(context.Background(), wire.Message{Shell: "bash", Command: "pwd", Cwd: dir, Timeout: 5}, func(s, v string) { out += v })
	if r.ExitCode != 0 || !strings.Contains(out, dir) {
		t.Fatal(r, out)
	}
}
func TestOutputBudget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash fixture")
	}
	out := ""
	r := ExecuteShell(context.Background(), wire.Message{Shell: "bash", Command: "printf '%080000d' 1", Timeout: 5}, func(s, v string) { out += v })
	if r.ExitCode != 0 || len(out) > 61000 || !strings.Contains(out, "截断") {
		t.Fatal(r, len(out))
	}
}
