package wire

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Bootstrap struct {
	Version int    `json:"v"`
	Base    string `json:"b"`
	Ticket  string `json:"k"`
}

func Token() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func (b Bootstrap) Encode() string {
	p, _ := json.Marshal(b)
	return base64.RawURLEncoding.EncodeToString(p)
}
func Decode(s string) (Bootstrap, error) {
	var b Bootstrap
	if len(s) > 600 {
		return b, errors.New("pairing payload too long")
	}
	p, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil {
		return b, e
	}
	if e = json.Unmarshal(p, &b); e != nil {
		return b, e
	}
	if b.Version != 2 || len(b.Ticket) != 32 {
		return b, errors.New("invalid pairing version or ticket")
	}
	if e = ValidateBase(b.Base); e != nil {
		return b, e
	}
	return b, nil
}
func ValidateBase(s string) error {
	u, e := url.Parse(s)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Hostname() == "" {
		return errors.New("endpoint must be an origin, without path or credentials")
	}
	if u.Scheme == "https" {
		return nil
	}
	h := u.Hostname()
	ip := net.ParseIP(h)
	if u.Scheme == "http" && (h == "localhost" || ip != nil && (ip.IsLoopback() || ip.IsPrivate())) {
		return nil
	}
	return errors.New("HTTP 仅允许 localhost 或私有局域网 IP；公网地址请使用 HTTPS")
}

var hostRE = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?$`)

func ValidTarget(s string) bool {
	if net.ParseIP(s) != nil {
		return true
	}
	if !hostRE.MatchString(s) || strings.Contains(s, "..") {
		return false
	}
	for _, p := range strings.Split(s, ".") {
		if len(p) == 0 || len(p) > 63 || p[0] == '-' || p[len(p)-1] == '-' {
			return false
		}
	}
	return true
}

type Grant struct {
	Scope string `json:"scope"`

	RequestID   string    `json:"request_id"`
	Target      string    `json:"target"`
	Expires     time.Time `json:"expires"`
	DeviceToken string    `json:"device_token,omitempty"`
	WebSocket   string    `json:"websocket,omitempty"`
	TargetInfo  string    `json:"target_info,omitempty"`
}
type Message struct {
	Type               string   `json:"type"`
	Shell              string   `json:"shell,omitempty"`
	Command            string   `json:"command,omitempty"`
	Cwd                string   `json:"cwd,omitempty"`
	Timeout            int      `json:"timeout,omitempty"`
	Hash               string   `json:"hash,omitempty"`
	Stream             string   `json:"stream,omitempty"`
	Status             string   `json:"status,omitempty"`
	Shells             []string `json:"shells,omitempty"`
	ID                 string   `json:"id,omitempty"`
	Target             string   `json:"target,omitempty"`
	Text               string   `json:"text,omitempty"`
	ExitCode           int      `json:"exit_code,omitempty"`
	DurationMS         int64    `json:"duration_ms,omitempty"`
	Host               string   `json:"host,omitempty"`
	OS                 string   `json:"os,omitempty"`
	Version            string   `json:"version,omitempty"`
	PID                int      `json:"pid,omitempty"`
	ProtocolRegistered bool     `json:"protocol_registered,omitempty"`
}
