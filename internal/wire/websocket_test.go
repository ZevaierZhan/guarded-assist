package wire

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestWebSocket(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		c, e := Accept(w, r)
		if e != nil {
			t.Error(e)
			return
		}
		defer c.Close()
		var m Message
		if e = c.ReadJSON(&m); e != nil {
			t.Error(e)
			return
		}
		if m.Text != "hello 世界" {
			t.Error(m)
		}
		c.Ping()
		c.WriteJSON(Message{Type: "pong", Text: strings.Repeat("x", 8000)})
	}))
	defer srv.Close()
	c, e := Dial(strings.Replace(srv.URL, "http", "ws", 1), "test")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if e = c.WriteJSON(Message{Type: "ping", Text: "hello 世界"}); e != nil {
		t.Fatal(e)
	}
	var m Message
	if e = c.ReadJSON(&m); e != nil {
		t.Fatal(e)
	}
	if len(m.Text) != 8000 {
		t.Fatal("bad payload")
	}
	wg.Wait()
}
