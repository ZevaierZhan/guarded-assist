// Package control provides an authenticated, loopback-only CLI adapter.
package control

import (
	"assistdemo/internal/mcpserver"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
)

type State struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	PID      int    `json:"pid"`
}

func Handler(api *mcpserver.Server, token string, shutdown func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() || r.Header.Get("Origin") != "" || token == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "local CLI authorization required"})
			return
		}
		if r.Method == "GET" && r.URL.Path == "/health" {
			json.NewEncoder(w).Encode(map[string]bool{"ready": true})
			return
		}
		if r.Method == "POST" && r.URL.Path == "/shutdown" {
			json.NewEncoder(w).Encode(map[string]bool{"shutting_down": true})
			go shutdown()
			return
		}
		if r.Method != "POST" || r.URL.Path != "/invoke" {
			http.NotFound(w, r)
			return
		}
		var input struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		d.DisallowUnknownFields()
		if err := d.Decode(&input); err != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		result, err := api.Invoke(input.Name, input.Arguments)
		if err != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(result)
	})
}
