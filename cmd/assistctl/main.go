package main

import (
	"assistdemo/internal/control"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		json.NewEncoder(os.Stderr).Encode(map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: assistctl up|network|start|status|preview|command|cancel|end|down [--state PATH] [options]; use COMMAND --help")
	}
	command := args[0]
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	statePath := f.String("state", filepath.Join(cache, "guarded-assist", "runtime.json"), "local runtime connection file (keep private)")
	server := f.String("server", "", "up: server executable; defaults to sibling assist-server")
	listen := f.String("listen", "0.0.0.0:0", "up: customer listener; OS selects a free port")
	public := f.String("public", "http://localhost:0", "up: customer origin; use private LAN IP or HTTPS origin")
	purpose := f.String("purpose", "", "start: assistance purpose")
	ip := f.String("local-ip", "", "start: customer-reachable local IP; default chooses LAN route")
	id := f.String("id", "", "session ID")
	op := f.String("operation", "", "operation ID")
	key := f.String("key", "", "start/command: idempotency key; reuse on retry")
	shell := f.String("shell", "", "command: powershell or bash")
	cmd := f.String("command", "", "command text")
	cmdFile := f.String("command-file", "", "read command text from a UTF-8 file")
	cwd := f.String("cwd", "", "command: customer working directory")
	timeout := f.Int("timeout", 30, "command timeout in seconds (1–60)")
	since := f.Int("since", 0, "status: events after this sequence")
	if err := f.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	abs, err := filepath.Abs(*statePath)
	if err != nil {
		return err
	}
	*statePath = abs
	if command == "up" {
		return up(*statePath, *server, *listen, *public, out)
	}
	state, err := readState(*statePath)
	if err != nil {
		return fmt.Errorf("runtime unavailable; run assistctl up: %w", err)
	}
	if command == "down" {
		return request(state, "POST", "/shutdown", map[string]any{}, out)
	}
	if command == "network" {
		return request(state, "POST", "/invoke", map[string]any{"name": "assist_network", "arguments": map[string]any{}}, out)
	}
	input := map[string]any{"request_id": *id}
	switch command {
	case "start":
		if *ip == "" {
			var b bytes.Buffer
			if err := request(state, "POST", "/invoke", map[string]any{"name": "assist_network", "arguments": map[string]any{}}, &b); err != nil {
				return err
			}
			var n struct {
				LocalIP string `json:"local_ip"`
			}
			if err := json.Unmarshal(b.Bytes(), &n); err != nil {
				return err
			}
			*ip = n.LocalIP
		}
		input = map[string]any{"purpose": *purpose, "local_ip": *ip, "idempotency_key": *key}
	case "status":
		input["operation_id"] = *op
		input["since_seq"] = *since
	case "preview", "end":
	case "cancel":
		input["operation_id"] = *op
	case "command":
		if *cmdFile != "" {
			if *cmd != "" {
				return errors.New("use --command or --command-file")
			}
			b, err := os.ReadFile(*cmdFile)
			if err != nil {
				return err
			}
			*cmd = string(b)
		}
		input["shell"] = *shell
		input["command"] = *cmd
		input["cwd"] = *cwd
		input["timeout"] = *timeout
		input["idempotency_key"] = *key
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	return request(state, "POST", "/invoke", map[string]any{"name": "assist_" + command, "arguments": input}, out)
}

func readState(path string) (control.State, error) {
	var s control.State
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	u, err := url.Parse(s.Endpoint)
	if err != nil {
		return s, err
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.Port() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || s.Token == "" {
		return s, errors.New("invalid local control endpoint")
	}
	return s, nil
}

func request(s control.State, method, path string, body any, out io.Writer) error {
	var input io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, s.Endpoint+path, input)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+s.Token)
	r.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := c.Do(r)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("control HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	_, err = out.Write(b)
	return err
}

func up(path, server, listen, public string, out io.Writer) error {
	if _, err := os.Stat(path); err == nil {
		s, err := readState(path)
		if err != nil {
			return err
		}
		if err = request(s, "GET", "/health", nil, io.Discard); err == nil {
			return json.NewEncoder(out).Encode(map[string]any{"ready": true, "pid": s.PID, "state": path, "reused": true})
		}
		return errors.New("connection file exists but runtime is unreachable; inspect its PID and remove the stale file before restarting")
	}
	if server == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		server = filepath.Join(filepath.Dir(exe), "assist-server")
		if runtime.GOOS == "windows" {
			server += ".exe"
		}
	}
	abs, err := filepath.Abs(server)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(path+".log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	c := exec.Command(abs, "--control-file", path, "--listen", listen, "--public", public, "--assets", filepath.Dir(abs), "--no-open")
	c.Stdout = logFile
	c.Stderr = logFile
	detach(c)
	if err = c.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := readState(path); err == nil && request(s, "GET", "/health", nil, io.Discard) == nil {
			return json.NewEncoder(out).Encode(map[string]any{"ready": true, "pid": s.PID, "state": path, "reused": false})
		}
		select {
		case err := <-done:
			return fmt.Errorf("server exited (%v); see %s.log", err, path)
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("server readiness timeout; inspect %s.log before retrying", path)
}
