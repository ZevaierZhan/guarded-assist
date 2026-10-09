package mcpserver

import (
	"assistdemo/internal/broker"
	"assistdemo/internal/wire"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func testServer() *Server {
	return &Server{Broker: broker.New("http://127.0.0.1:18777", ".", []byte("page")), LocalBase: "http://127.0.0.1:18777"}
}

func TestMCPHandshakeAndTools(t *testing.T) {
	s := testServer()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"assist_start","arguments":{"purpose":"test","local_ip":"127.0.0.1","idempotency_key":"start-test-1"}}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := s.Run(strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 responses, got %d: %s", len(lines), out.String())
	}
	var init map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatal(err)
	}
	result := init["result"].(map[string]any)
	if result["protocolVersion"] != protocolVersion {
		t.Fatal(result)
	}
	var listed map[string]any
	json.Unmarshal([]byte(lines[1]), &listed)
	if len(listed["result"].(map[string]any)["tools"].([]any)) != 6 {
		t.Fatal(lines[1])
	}
	if strings.Contains(lines[2], s.Broker.Admin) {
		t.Fatal("admin credential leaked")
	}
	var created map[string]any
	json.Unmarshal([]byte(lines[2]), &created)
	data := created["result"].(map[string]any)["structuredContent"].(map[string]any)
	if data["id"] == "" || data["invite_url"] == "" {
		t.Fatal(data)
	}
}

func TestMCPFlowAndCredentialFiltering(t *testing.T) {
	s := testServer()
	start, err := s.call("assist_start", []byte(`{"purpose":"diagnose","local_ip":"127.0.0.1","idempotency_key":"start-flow-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	id := start.(map[string]any)["id"].(string)
	preview, err := s.call("assist_preview", []byte(`{"request_id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	link := preview.(map[string]any)["preview_url"].(string)
	if !strings.HasPrefix(link, "http://127.0.0.1:18777/supervise/") || strings.Contains(link, s.Broker.Admin) {
		t.Fatal(link)
	}
	status, err := s.call("assist_status", []byte(`{"request_id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(status)
	if bytes.Contains(b, []byte(s.Broker.Admin)) || bytes.Contains(b, []byte("invite_url")) {
		t.Fatal(string(b))
	}
	_, err = s.call("assist_command", []byte(`{"request_id":"`+id+`","shell":"powershell","command":"Get-Date","idempotency_key":"command-flow-1"}`))
	if err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatalf("offline command: %v", err)
	}
	ended, err := s.call("assist_end", []byte(`{"request_id":"`+id+`"}`))
	if err != nil || ended.(map[string]any)["revoked"] != true {
		t.Fatalf("end: %v %v", ended, err)
	}
}

func TestIdempotentStartAndCommand(t *testing.T) {
	s := testServer()
	args := []byte(`{"purpose":"diagnose","local_ip":"127.0.0.1","idempotency_key":"same-start"}`)
	first, err := s.call("assist_start", args)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.call("assist_start", args)
	if err != nil || first.(map[string]any)["id"] != again.(map[string]any)["id"] || len(s.Broker.Sessions) != 1 {
		t.Fatal("start duplicated", err)
	}
	if _, err := s.call("assist_start", []byte(`{"purpose":"different","local_ip":"127.0.0.1","idempotency_key":"same-start"}`)); err == nil {
		t.Fatal("key reused with different arguments")
	}
	id := first.(map[string]any)["id"].(string)
	p := s.Broker.Sessions[id]
	p.Online = true
	p.Device = wire.Message{Type: "hello", Shells: []string{"bash"}}
	command := []byte(`{"request_id":"` + id + `","shell":"bash","command":"rm -rf ./cache","idempotency_key":"same-command"}`)
	firstOp, err := s.call("assist_command", command)
	if err != nil {
		t.Fatal(err)
	}
	againOp, err := s.call("assist_command", command)
	if err != nil || firstOp.(map[string]any)["id"] != againOp.(map[string]any)["id"] || len(p.Operations) != 1 {
		t.Fatal("command duplicated", err)
	}
}
