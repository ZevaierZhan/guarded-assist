// Package wire implements the RFC 6455 subset needed by this controlled demo.
// It supports bounded text messages, continuation frames, masking, ping/pong,
// and close. It negotiates no extensions. For production use a maintained WS
// implementation and run the full Autobahn interoperability/conformance suite.
package wire

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxMessage = 65536
const magic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type Conn struct {
	net.Conn
	r      *bufio.Reader
	client bool
	mu     sync.Mutex
}

func acceptKey(s string) string {
	h := sha1.Sum([]byte(s + magic))
	return base64.StdEncoding.EncodeToString(h[:])
}
func hasToken(s, t string) bool {
	for _, v := range strings.Split(s, ",") {
		if strings.EqualFold(strings.TrimSpace(v), t) {
			return true
		}
	}
	return false
}
func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if r.Method != "GET" || !hasToken(r.Header.Get("Connection"), "upgrade") || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, errors.New("invalid WebSocket handshake")
	}
	k := r.Header.Get("Sec-WebSocket-Key")
	b, e := base64.StdEncoding.DecodeString(k)
	if e != nil || len(b) != 16 {
		return nil, errors.New("invalid WebSocket key")
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("HTTP hijacking unavailable")
	}
	c, rw, e := h.Hijack()
	if e != nil {
		return nil, e
	}
	if _, e = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", acceptKey(k)); e == nil {
		e = rw.Flush()
	}
	if e != nil {
		c.Close()
		return nil, e
	}
	return &Conn{Conn: c, r: rw.Reader}, nil
}
func Dial(raw, token string) (*Conn, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return nil, e
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, errors.New("invalid WebSocket scheme")
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	var c net.Conn
	if u.Scheme == "wss" {
		c, e = tls.DialWithDialer(&d, "tcp", net.JoinHostPort(host, port), &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		c, e = d.Dial("tcp", net.JoinHostPort(host, port))
	}
	if e != nil {
		return nil, e
	}
	c.SetDeadline(time.Now().Add(12 * time.Second))
	defer func() {
		if e != nil {
			c.Close()
		}
	}()
	k := make([]byte, 16)
	if _, e = rand.Read(k); e != nil {
		return nil, e
	}
	key := base64.StdEncoding.EncodeToString(k)
	req := &http.Request{Method: "GET", URL: u, Host: u.Host, Header: make(http.Header)}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)
	req.Header.Set("Authorization", "Bearer "+token)
	if e = req.Write(c); e != nil {
		return nil, e
	}
	rd := bufio.NewReader(c)
	var res *http.Response
	res, e = http.ReadResponse(rd, req)
	if e != nil {
		return nil, e
	}
	if res.StatusCode != 101 || !hasToken(res.Header.Get("Connection"), "upgrade") || !strings.EqualFold(res.Header.Get("Upgrade"), "websocket") || res.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) {
		e = fmt.Errorf("WebSocket rejected: HTTP %d", res.StatusCode)
		return nil, e
	}
	c.SetDeadline(time.Time{})
	return &Conn{Conn: c, r: rd, client: true}, nil
}
func (c *Conn) frame(op byte, p []byte) error {
	if len(p) > maxMessage {
		return errors.New("message too large")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.SetWriteDeadline(time.Now().Add(5 * time.Second))
	h := []byte{0x80 | op}
	m := byte(0)
	if c.client {
		m = 0x80
	}
	if len(p) < 126 {
		h = append(h, m|byte(len(p)))
	} else if len(p) < 65536 {
		h = append(h, m|126, byte(len(p)>>8), byte(len(p)))
	} else {
		h = append(h, m|127, 0, 0, 0, 0, 0, 1, 0, 0)
	}
	payload := p
	if c.client {
		key := make([]byte, 4)
		if _, e := rand.Read(key); e != nil {
			return e
		}
		h = append(h, key...)
		payload = append([]byte(nil), p...)
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	out := append(h, payload...)
	for len(out) > 0 {
		n, e := c.Conn.Write(out)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		out = out[n:]
	}
	return nil
}
func (c *Conn) WriteJSON(v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return c.frame(1, b)
}
func (c *Conn) Ping() error  { return c.frame(9, []byte("assist")) }
func (c *Conn) Close() error { return c.Conn.Close() }
func (c *Conn) ReadJSON(v any) error {
	var msg []byte
	started := false
	for {
		c.SetReadDeadline(time.Now().Add(45 * time.Second))
		h := make([]byte, 2)
		if _, e := io.ReadFull(c.r, h); e != nil {
			return e
		}
		fin := h[0]&128 != 0
		op := h[0] & 15
		if h[0]&112 != 0 {
			return errors.New("unnegotiated reserved bits")
		}
		masked := h[1]&128 != 0
		if masked == c.client {
			return errors.New("invalid frame masking")
		}
		n := uint64(h[1] & 127)
		if n == 126 {
			b := make([]byte, 2)
			if _, e := io.ReadFull(c.r, b); e != nil {
				return e
			}
			n = uint64(binary.BigEndian.Uint16(b))
			if n < 126 {
				return errors.New("nonminimal frame")
			}
		} else if n == 127 {
			b := make([]byte, 8)
			if _, e := io.ReadFull(c.r, b); e != nil {
				return e
			}
			n = binary.BigEndian.Uint64(b)
			if n < 65536 {
				return errors.New("nonminimal frame")
			}
		}
		if n > maxMessage {
			return errors.New("frame limit exceeded")
		}
		if op >= 8 && (!fin || n > 125) {
			return errors.New("invalid control frame")
		}
		var mask [4]byte
		if masked {
			if _, e := io.ReadFull(c.r, mask[:]); e != nil {
				return e
			}
		}
		b := make([]byte, int(n))
		if _, e := io.ReadFull(c.r, b); e != nil {
			return e
		}
		if masked {
			for i := range b {
				b[i] ^= mask[i%4]
			}
		}
		switch op {
		case 8:
			return io.EOF
		case 9:
			if e := c.frame(10, b); e != nil {
				return e
			}
			continue
		case 10:
			continue
		case 1:
			if started {
				return errors.New("unexpected text frame")
			}
			started = true
		case 0:
			if !started {
				return errors.New("unexpected continuation")
			}
		default:
			return errors.New("unsupported opcode")
		}
		if len(msg)+len(b) > maxMessage {
			return errors.New("message limit exceeded")
		}
		msg = append(msg, b...)
		if fin {
			return json.Unmarshal(msg, v)
		}
	}
}
