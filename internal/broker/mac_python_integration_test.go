package broker

import (
	"assistdemo/web"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This checks the Python executor's real HTTP and WebSocket pairing path.
// Running Bash commands on macOS still requires a Mac test machine.
func TestPythonExecutorPairAndStop(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		python, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("Python 3 is not installed")
	}
	s := New("", t.TempDir(), web.Page)
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	s.Base = server.URL

	request := func(method, path, token string, body io.Reader) *http.Response {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	created := request("POST", "/api/requests", s.Admin, bytes.NewBufferString(`{"local_ip":"127.0.0.1"}`))
	defer created.Body.Close()
	if created.StatusCode != 201 {
		data, _ := io.ReadAll(created.Body)
		t.Fatalf("create: %d %s", created.StatusCode, data)
	}
	var state struct {
		ID       string `json:"id"`
		Filename string `json:"filename_python"`
	}
	if err := json.NewDecoder(created.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	p := s.Sessions[state.ID]
	s.mu.Unlock()
	script := filepath.Join(t.TempDir(), state.Filename)
	if err := os.WriteFile(script, web.MacPython, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, script)
	cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		online := p.Online && p.Device.Type == "hello"
		s.mu.Unlock()
		if online {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.mu.Lock()
	online := p.Online && p.Device.Type == "hello"
	s.mu.Unlock()
	if !online {
		t.Fatalf("Python executor did not connect: %s %s", stdout.String(), stderr.String())
	}
	for _, command := range []string{"date", "pwd", "echo 你好"} {
		body, _ := json.Marshal(map[string]any{"shell": "bash", "command": command, "timeout": 5})
		response := request("POST", "/api/requests/"+state.ID+"/commands", s.Admin, bytes.NewReader(body))
		response.Body.Close()
		if response.StatusCode != 201 {
			t.Fatalf("submit %q: %d; %s %s", command, response.StatusCode, stdout.String(), stderr.String())
		}
		until := time.Now().Add(4 * time.Second)
		for time.Now().Before(until) {
			s.mu.Lock()
			last := p.Operations[len(p.Operations)-1]
			finished := terminal(last.Status)
			s.mu.Unlock()
			if finished {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		s.mu.Lock()
		last := p.Operations[len(p.Operations)-1]
		finished, status := terminal(last.Status), last.Status
		s.mu.Unlock()
		if !finished || status == "blocked" {
			t.Fatalf("command %q did not finish: %s; %s %s", command, status, stdout.String(), stderr.String())
		}
	}
	stopped := request("POST", "/api/requests/"+state.ID+"/stop", s.Admin, bytes.NewBufferString(`{}`))
	stopped.Body.Close()
	if stopped.StatusCode != 200 {
		t.Fatalf("stop: %d", stopped.StatusCode)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Python executor exit: %v; %s %s", err, stdout.String(), stderr.String())
	}
	s.mu.Lock()
	ack := p.Stopped
	s.mu.Unlock()
	if !ack {
		t.Fatal("Python executor did not return stopped receipt")
	}
}
