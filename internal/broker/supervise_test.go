package broker

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSupervisorLinkScope(t *testing.T) {
	s, p := setup(t)
	s.SupervisorOrigin = "http://127.0.0.1:18777"
	link, err := s.SupervisorLink(p.ID, "http://127.0.0.1:18777")
	if err != nil || !strings.Contains(link, "/supervise/"+p.ID+"#s=") || strings.Contains(link, s.Admin) {
		t.Fatalf("link: %s %v", link, err)
	}
	path := "/api/requests/" + p.ID
	local := func(method, route, body, token, cookie, remote string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, route, strings.NewReader(body))
		r.RemoteAddr = remote
		r.Header.Set("Authorization", "Bearer "+token)
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if got := local("POST", "/api/supervisor/exchange", `{"request_id":"`+p.ID+`","token":"`+p.Supervisor+`"}`, "", "", "192.0.2.1:1234"); got.Code != 403 {
		t.Fatal("remote exchange allowed")
	}
	issued := p.Supervisor
	exchange := local("POST", "/api/supervisor/exchange", `{"request_id":"`+p.ID+`","token":"`+issued+`"}`, "", "", "127.0.0.1:1234")
	if exchange.Code != 200 {
		t.Fatal(exchange.Code, exchange.Body.String())
	}
	if got := local("POST", "/api/supervisor/exchange", `{"request_id":"`+p.ID+`","token":"`+issued+`"}`, "", "", "127.0.0.1:1234"); got.Code != 403 {
		t.Fatal("token reused")
	}
	var cookie *http.Cookie
	for _, c := range exchange.Result().Cookies() {
		if c.Name == "assist_supervisor" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly {
		t.Fatal("missing private supervisor cookie")
	}
	cookieHeader := cookie.Name + "=" + cookie.Value
	originRequest := httptest.NewRequest("POST", path+"/stop", nil)
	originRequest.RemoteAddr = "127.0.0.1:1234"
	originRequest.Header.Set("Origin", s.SupervisorOrigin)
	originRequest.Header.Set("Cookie", cookieHeader)
	originResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(originResponse, originRequest)
	if originResponse.Code != 200 {
		t.Fatal("local browser origin rejected", originResponse.Code, originResponse.Body.String())
	}
	badOrigin := httptest.NewRequest("GET", path, nil)
	badOrigin.RemoteAddr = "127.0.0.1:1234"
	badOrigin.Header.Set("Origin", "https://evil.example")
	badOrigin.Header.Set("Cookie", cookieHeader)
	badResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(badResponse, badOrigin)
	if badResponse.Code != 403 {
		t.Fatal("cross-origin supervisor request allowed")
	}
	if got := local("GET", path, "", "", cookieHeader, "127.0.0.1:1234"); got.Code != 200 {
		t.Fatal(got.Code, got.Body.String())
	} else if strings.Contains(got.Body.String(), "invite_url") || strings.Contains(got.Body.String(), "launch_uri") {
		t.Fatal("supervisor snapshot leaked pairing data")
	}
	if got := local("GET", path, "", "", cookieHeader, "192.0.2.1:1234"); got.Code != 403 {
		t.Fatal("remote access allowed")
	}
	if got := local("POST", path+"/commands", `{"shell":"bash","command":"date","timeout":30}`, "", cookieHeader, "127.0.0.1:1234"); got.Code != 403 {
		t.Fatal("supervisor command allowed")
	}
	if got := local("GET", path+"/download?platform=windows", "", "", cookieHeader, "127.0.0.1:1234"); got.Code != 403 {
		t.Fatal("supervisor download allowed")
	}
	if got := local("POST", path+"/commands/anything/cancel", `{}`, "", cookieHeader, "127.0.0.1:1234"); got.Code != 403 {
		t.Fatal("supervisor cancel allowed")
	}
	if got := local("GET", "/supervise/"+p.ID, "", "", "", "192.0.2.1:1234"); got.Code != 403 {
		t.Fatal("remote page allowed")
	}
	if got := local("POST", path+"/stop", `{}`, "", cookieHeader, "127.0.0.1:1234"); got.Code != 200 {
		t.Fatal("supervisor cannot stop", got.Code)
	}
}
