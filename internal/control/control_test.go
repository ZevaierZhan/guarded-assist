package control

import (
	"assistdemo/internal/broker"
	"assistdemo/internal/mcpserver"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalAuthorization(t *testing.T) {
	api := &mcpserver.Server{Broker: broker.New("http://127.0.0.1:18777", "", nil), LocalBase: "http://127.0.0.1:18777"}
	h := Handler(api, "secret", func() {})
	for _, tc := range []struct {
		remote, token, origin string
		code                  int
	}{
		{"127.0.0.1:1234", "secret", "", 200},
		{"[::1]:1234", "secret", "", 200},
		{"192.0.2.10:1234", "secret", "", 403},
		{"127.0.0.1:1234", "wrong", "", 403},
		{"127.0.0.1:1234", "secret", "http://untrusted.example", 403},
	} {
		r := httptest.NewRequest("GET", "/health", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Errorf("%+v: %d", tc, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/invoke", strings.NewReader(`{"name":"assist_start","arguments":{"purpose":"CLI test","local_ip":"127.0.0.1","idempotency_key":"start-1"}}`))
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), api.Broker.Admin) {
		t.Fatalf("operation failed or leaked admin: %d", w.Code)
	}
}
