package broker

import (
	"assistdemo/internal/wire"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCustomerCanTogglePersistenceOnlyAfterConnection(t *testing.T) {
	s := New("", ".", []byte("demo"))
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	s.Base = server.URL
	created := call(s, "POST", "/api/requests", `{"local_ip":"127.0.0.1"}`, s.Admin)
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	var state struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	p := s.Sessions[state.ID]
	path := "/api/requests/" + p.ID + "/persistence"
	if got := call(s, "POST", path, `{"enabled":true}`, p.View); got.Code != 409 {
		t.Fatalf("offline toggle: %d", got.Code)
	}
	if got := call(s, "POST", path, `{"enabled":true}`, s.Admin); got.Code != 403 {
		t.Fatalf("admin toggle: %d", got.Code)
	}
	if got := call(s, "POST", "/api/pair", "", p.Ticket); got.Code != 200 {
		t.Fatal(got.Body.String())
	}
	conn, err := wire.Dial(strings.Replace(server.URL, "http", "ws", 1)+"/api/device", p.DeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(wire.Message{Type: "hello", Version: "0.2.0", Host: "test", OS: "linux/amd64"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		online := p.Online && p.Device.Type == "hello"
		s.mu.Unlock()
		if online {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := call(s, "POST", path, `{"enabled":true}`, p.View); got.Code != 200 {
		t.Fatalf("enable: %d %s", got.Code, got.Body.String())
	}
	if !p.Persistent {
		t.Fatal("persistence not enabled")
	}
	s.mu.Lock()
	p.Expires = time.Now().Add(-time.Second)
	valid := s.valid(p)
	s.mu.Unlock()
	if !valid {
		t.Fatal("persistent session expired")
	}
	if got := call(s, "POST", path, `{"enabled":false}`, p.View); got.Code != 200 {
		t.Fatalf("disable: %d %s", got.Code, got.Body.String())
	}
	if p.Persistent || time.Until(p.Expires) < 9*time.Minute {
		t.Fatal("countdown did not restart")
	}
	req, err := http.NewRequest("GET", server.URL+"/api/requests/"+p.ID+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+p.View)
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	viewers := p.Viewers
	s.mu.Unlock()
	if viewers != 1 {
		t.Fatalf("customer page not tracked: %d", viewers)
	}
	stream.Body.Close()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		viewers = p.Viewers
		s.mu.Unlock()
		if viewers == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if viewers != 0 {
		t.Fatal("closed customer page still tracked")
	}
	s.mu.Lock()
	p.LastViewClosed = time.Now().Add(-21 * time.Second)
	p.DeviceConnected = p.LastViewClosed
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Sweep(ctx)
	var stopped wire.Message
	if err := conn.ReadJSON(&stopped); err != nil || stopped.Type != "stop" {
		t.Fatalf("page close did not stop executor: %+v %v", stopped, err)
	}
	s.mu.Lock()
	revoked := p.Revoked
	s.mu.Unlock()
	if !revoked {
		t.Fatal("page close did not revoke session")
	}
}
