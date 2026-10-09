package broker

import (
	"archive/zip"
	"assistdemo/internal/wire"
	"assistdemo/web"
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func call(s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(w, r)
	return w
}
func setup(t *testing.T) (*Server, *Session) {
	s := New("http://127.0.0.1:18777", ".", []byte("demo"))
	w := call(s, "POST", "/api/requests", `{"local_ip":"127.0.0.1","purpose":"test"}`, s.Admin)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	var x map[string]any
	json.Unmarshal(w.Body.Bytes(), &x)
	return s, s.Sessions[x["id"].(string)]
}
func TestPolicySessionAndExpiry(t *testing.T) {
	s, p := setup(t)
	path := "/api/requests/" + p.ID
	if call(s, "GET", path, "", "bad").Code != 403 {
		t.Fatal("access")
	}
	if call(s, "GET", "/api/bootstrap", "", p.Ticket).Code != 200 {
		t.Fatal("policy session should not require browser approval")
	}
	if call(s, "POST", path+"/accept", "{}", p.View).Code != 405 {
		t.Fatal("legacy approval endpoint should not exist")
	}
	p.Expires = time.Now().Add(-time.Second)
	if call(s, "GET", "/api/bootstrap", "", p.Ticket).Code != 410 {
		t.Fatal("expiry")
	}
}
func TestCommandsRequireOwnerAndOnline(t *testing.T) {
	s, p := setup(t)
	path := "/api/requests/" + p.ID + "/commands"
	body := `{"shell":"bash","command":"rm -rf ./cache","timeout":30}`
	if call(s, "POST", path, body, p.View).Code != 403 {
		t.Fatal("customer submitted")
	}
	if call(s, "POST", path, body, s.Admin).Code != 409 {
		t.Fatal("offline execute")
	}
	p.Online = true
	p.Device = wire.Message{Type: "hello", Shells: []string{"bash"}}
	if call(s, "POST", path, body, s.Admin).Code != 201 {
		t.Fatal("submit")
	}
	if p.Operations[0].Status != "blocked" || p.Running || p.Operations[0].Policy == "" {
		t.Fatal("blacklisted command was not blocked")
	}
	if call(s, "POST", path+"/"+p.Operations[0].ID+"/approve", `{}`, p.View).Code != 405 {
		t.Fatal("legacy per-command approval endpoint should not exist")
	}
}
func TestCORS(t *testing.T) {
	s, _ := setup(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/requests", strings.NewReader(`{"purpose":"test"}`))
	r.Header.Set("Origin", "https://untrusted.example")
	r.Header.Set("Authorization", "Bearer "+s.Admin)
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("CORS")
	}
}
func TestNetworkCheckAndLocalIPSelection(t *testing.T) {
	s := New("http://127.0.0.1:18777", ".", []byte("demo"))
	if got := call(s, "POST", "/api/network-check", `{}`, "bad"); got.Code != 403 {
		t.Fatalf("unauthorized network check: %d", got.Code)
	}
	if got := call(s, "POST", "/api/network-check", `{"target":"127.0.0.1"}`, s.Admin); got.Code != 400 {
		t.Fatalf("legacy peer IP field accepted: %d", got.Code)
	}
	got := call(s, "POST", "/api/network-check", `{}`, s.Admin)
	if got.Code != 200 {
		t.Fatalf("list local addresses: %d %s", got.Code, got.Body.String())
	}
	var network struct {
		LocalIP   string         `json:"local_ip"`
		Addresses []localAddress `json:"addresses"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &network); err != nil || network.LocalIP == "" || len(network.Addresses) == 0 {
		t.Fatalf("local address list: %+v, %v", network, err)
	}
	selected := network.Addresses[0].IP
	got = call(s, "POST", "/api/requests", `{"purpose":"test","local_ip":"`+selected+`"}`, s.Admin)
	if got.Code != 201 {
		t.Fatalf("listed local IP rejected: %d %s", got.Code, got.Body.String())
	}
	var result struct {
		LocalIP   string `json:"local_ip"`
		InviteURL string `json:"invite_url"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &result); err != nil || result.LocalIP != selected || !strings.Contains(result.InviteURL, selected) {
		t.Fatalf("selected local IP result: %+v, %v", result, err)
	}
	if got = call(s, "POST", "/api/requests", `{"purpose":"test","local_ip":"203.0.113.7"}`, s.Admin); got.Code != 400 {
		t.Fatalf("non-local IP accepted: %d %s", got.Code, got.Body.String())
	}
}
func TestMacDownloadsKeepPairingFilename(t *testing.T) {
	s, p := setup(t)
	s.Assets = t.TempDir()
	for _, variant := range []struct {
		platform string
		binary   string
	}{
		{"darwin-arm64", "assist-darwin-arm64"},
		{"darwin-amd64", "assist-darwin-amd64"},
	} {
		if err := os.WriteFile(filepath.Join(s.Assets, variant.binary), []byte(variant.platform), 0600); err != nil {
			t.Fatal(err)
		}
		url := "/api/requests/" + p.ID + "/download?platform=" + variant.platform
		got := call(s, "GET", url, "", p.View)
		if got.Code != 200 || got.Body.String() != variant.platform {
			t.Fatalf("%s: %d %s", variant.platform, got.Code, got.Body.String())
		}
		want := `filename="` + s.snapshot(p)["filename_linux"].(string) + `"`
		if !strings.Contains(got.Header().Get("Content-Disposition"), want) {
			t.Fatalf("%s: pairing filename missing: %s", variant.platform, got.Header().Get("Content-Disposition"))
		}
	}
	if got := call(s, "GET", "/api/requests/"+p.ID+"/download?platform=unknown", "", p.View); got.Code != 400 {
		t.Fatalf("unknown platform: %d", got.Code)
	}
	got := call(s, "GET", "/api/requests/"+p.ID+"/download?platform=python", "", p.View)
	if got.Code != 200 || got.Body.String() != string(web.MacPython) {
		t.Fatalf("Python download: %d", got.Code)
	}
	want := `filename="` + s.snapshot(p)["filename_python"].(string) + `"`
	if !strings.Contains(got.Header().Get("Content-Disposition"), want) {
		t.Fatalf("Python pairing filename missing: %s", got.Header().Get("Content-Disposition"))
	}
}

func TestMacAppDownloadPackagesBothArchitecturesAndCurrentInvitation(t *testing.T) {
	s, p := setup(t)
	s.Assets = t.TempDir()
	for _, binary := range []string{"assist-darwin-arm64", "assist-darwin-amd64"} {
		if err := os.WriteFile(filepath.Join(s.Assets, binary), []byte(binary), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got := call(s, "GET", "/api/requests/"+p.ID+"/download?platform=mac-app", "", p.View)
	if got.Code != 200 || got.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("Mac App download: %d %s", got.Code, got.Body.String())
	}
	if want := `filename="Assist-` + p.ID + `-macOS.zip"`; !strings.Contains(got.Header().Get("Content-Disposition"), want) {
		t.Fatalf("wrong filename: %s", got.Header().Get("Content-Disposition"))
	}
	archive, err := zip.NewReader(bytes.NewReader(got.Body.Bytes()), int64(got.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]*zip.File{}
	for _, f := range archive.File {
		entries[f.Name] = f
	}
	for _, name := range []string{
		"Assist.app/Contents/Info.plist", "Assist.app/Contents/MacOS/launcher",
		"Assist.app/Contents/Resources/start.command", "Assist.app/Contents/Resources/invite.txt",
		"Assist.app/Contents/MacOS/assist-arm64", "Assist.app/Contents/MacOS/assist-amd64",
	} {
		if entries[name] == nil {
			t.Fatalf("missing %s", name)
		}
	}
	for _, name := range []string{"Assist.app/Contents/MacOS/launcher", "Assist.app/Contents/Resources/start.command", "Assist.app/Contents/MacOS/assist-arm64", "Assist.app/Contents/MacOS/assist-amd64"} {
		if entries[name].Mode()&0111 == 0 {
			t.Fatalf("not executable: %s", name)
		}
	}
	f, err := entries["Assist.app/Contents/Resources/invite.txt"].Open()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	b, err := wire.Decode(strings.TrimSpace(string(data)))
	if err != nil || b.Base != p.Base || b.Ticket != p.Ticket {
		t.Fatalf("App invitation mismatch: %v", err)
	}
	infoFile, err := entries["Assist.app/Contents/Info.plist"].Open()
	if err != nil {
		t.Fatal(err)
	}
	info, err := io.ReadAll(infoFile)
	infoFile.Close()
	if err != nil || bytes.Contains(info, []byte("INVITE_ID")) || !bytes.Contains(info, []byte("dev.assistdemo.temporary.")) {
		t.Fatalf("bundle identifier was not generated: %v", err)
	}
}
func TestStoppedReceipt(t *testing.T) {
	s, p := setup(t)
	p.Revoked = true
	if s.snapshot(p)["stopped"] != false {
		t.Fatal("fabricated cleanup")
	}
}
