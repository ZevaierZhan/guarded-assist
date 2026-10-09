package broker

import (
	"assistdemo/internal/wire"
	"assistdemo/web"
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Seq  int    `json:"seq"`
	Time string `json:"time"`
	Type string `json:"type"`
	Text string `json:"text"`
}
type Session struct {
	ID, View, Ticket, DeviceToken, Target string
	Supervisor, SupervisorCookie          string
	SupervisorUntil                       time.Time
	Purpose, LocalIP, Base                string
	Operations                            []*Operation
	Stopped                               bool
	Persistent                            bool
	Viewers                               int
	DeviceConnected, LastViewClosed       time.Time

	Created, Expires                           time.Time
	Accepted, Paired, Online, Revoked, Running bool
	RunCount                                   int
	ActiveID                                   string
	Conn                                       *wire.Conn
	Events                                     []Event
	Seq                                        int
	Subscribers                                map[chan struct{}]bool
	Device                                     wire.Message
}
type Server struct {
	mu                                    sync.Mutex
	Sessions                              map[string]*Session
	Base, Admin, Assets, SupervisorOrigin string
	Page                                  []byte
	TTL                                   time.Duration
}

func New(base, assets string, page []byte) *Server {
	return &Server{Sessions: map[string]*Session{}, Base: strings.TrimRight(base, "/"), Admin: wire.Token(), Assets: assets, Page: page, TTL: 10 * time.Minute}
}
func equal(a, b string) bool {
	return a != "" && b != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, s string) {
	jsonOut(w, status, map[string]string{"error": s})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "请求格式无效")
		return false
	}
	return true
}
func (s *Server) addLocked(p *Session, kind, text string) {
	p.Seq++
	p.Events = append(p.Events, Event{p.Seq, time.Now().Format(time.RFC3339Nano), kind, text})
	if len(p.Events) > 1200 {
		p.Events = p.Events[len(p.Events)-1200:]
	}
	for c := range p.Subscribers {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}
func (s *Server) valid(p *Session) bool {
	return p != nil && !p.Revoked && (p.Persistent || time.Now().Before(p.Expires))
}
func (s *Server) findTicket(t string) *Session {
	for _, p := range s.Sessions {
		if equal(p.Ticket, t) {
			return p
		}
	}
	return nil
}
func (s *Server) findDevice(t string) *Session {
	for _, p := range s.Sessions {
		if equal(p.DeviceToken, t) {
			return p
		}
	}
	return nil
}
func (s *Server) grant(p *Session) wire.Grant {
	return wire.Grant{RequestID: p.ID, Target: p.Target, Expires: p.Expires, Scope: "policy-shell-v3"}
}

func (s *Server) originAllowed(origin string) bool {
	if origin == s.Base {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.Sessions {
		if origin == p.Base {
			return true
		}
	}
	return false
}

func (s *Server) baseForLocalIP(localIP string) string {
	u, err := url.Parse(s.Base)
	if err != nil {
		return s.Base
	}
	hostIP := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (hostIP == nil || !hostIP.IsLoopback() && !hostIP.IsPrivate()) {
		return s.Base
	}
	if u.Port() != "" {
		u.Host = net.JoinHostPort(localIP, u.Port())
	} else if strings.Contains(localIP, ":") {
		u.Host = "[" + localIP + "]"
	} else {
		u.Host = localIP
	}
	return strings.TrimRight(u.String(), "/")
}
func (s *Server) snapshot(p *Session) map[string]any {
	status := "waiting"
	if p.Accepted {
		status = "accepted"
	}
	if p.Paired {
		status = "paired"
	}
	if p.Online && p.Device.Type == "hello" {
		status = "connected"
	}
	if p.Running {
		status = "running"
	}
	if p.Device.Type == "hello" && !p.Online {
		status = "disconnected"
	}
	if p.Revoked {
		status = "revoked"
	} else if !p.Persistent && time.Now().After(p.Expires) {
		status = "expired"
	}
	b := wire.Bootstrap{Version: 2, Base: p.Base, Ticket: p.Ticket}
	encoded := b.Encode()
	ev := append([]Event{}, p.Events...)
	ops := []Operation{}
	for _, o := range p.Operations {
		cp := *o
		cp.Outputs = append([]Output{}, o.Outputs...)
		ops = append(ops, cp)
	}
	expires := any(p.Expires)
	if p.Persistent {
		expires = nil
	}
	return map[string]any{"purpose": p.Purpose, "operations": ops, "stopped": p.Stopped, "id": p.ID, "local_ip": p.LocalIP, "status": status, "accepted": p.Accepted, "paired": p.Paired, "online": p.Online && p.Device.Type == "hello", "revoked": p.Revoked, "running": p.Running, "persistent": p.Persistent, "expires": expires, "created": p.Created, "runs": p.RunCount, "device": p.Device, "events": ev, "launch_uri": "assist://join/" + encoded, "filename_windows": "assist--" + encoded + ".exe", "filename_linux": "assist--" + encoded, "filename_python": "assist--" + encoded + ".py", "invite_url": p.Base + "/#r=" + p.ID + "&t=" + p.View, "base": p.Base, "seq": p.Seq}
}

func (s *Server) snapshotFor(r *http.Request, p *Session) map[string]any {
	out := s.snapshot(p)
	if !equal(bearer(r), s.Admin) && !equal(bearer(r), p.View) {
		delete(out, "launch_uri")
		delete(out, "invite_url")
		delete(out, "filename_windows")
		delete(out, "filename_linux")
		delete(out, "filename_python")
	}
	return out
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(s.Page)
	})
	mux.HandleFunc("GET /supervise/{id}", s.supervise)
	mux.HandleFunc("POST /api/supervisor/exchange", s.exchangeSupervisor)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]string{"service": "assist-demo", "version": "0.2.0", "scope": "policy-gated-shell"})
	})
	mux.HandleFunc("POST /api/requests", s.create)
	mux.HandleFunc("POST /api/network-check", s.networkCheck)
	mux.HandleFunc("GET /api/requests/{id}", s.get)
	mux.HandleFunc("GET /api/requests/{id}/events", s.events)
	mux.HandleFunc("POST /api/requests/{id}/commands", s.command)
	mux.HandleFunc("POST /api/requests/{id}/commands/{op}/cancel", s.cancelCommand)
	mux.HandleFunc("POST /api/requests/{id}/stop", s.stop)
	mux.HandleFunc("POST /api/requests/{id}/persistence", s.setPersistence)
	mux.HandleFunc("GET /api/requests/{id}/download", s.download)
	mux.HandleFunc("GET /api/bootstrap", s.bootstrap)
	mux.HandleFunc("POST /api/pair", s.pair)
	mux.HandleFunc("GET /api/device", s.device)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'")
		// Browser operations must be same-origin. Native clients send no Origin and
		// still need one-use or device credentials; no credential lives in a WS URL.
		origin := r.Header.Get("Origin")
		if origin != "" && !(origin == s.SupervisorOrigin && isLoopbackRequest(r)) && !s.originAllowed(origin) {
			fail(w, 403, "拒绝跨来源请求")
			return
		}
		if r.ContentLength > 8192 && r.Method == "POST" {
			fail(w, 413, "请求太大")
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	if !equal(bearer(r), s.Admin) {
		fail(w, 403, "仅支持方可创建申请，请使用服务启动时打开的管理页")
		return
	}
	var in struct {
		LocalIP string `json:"local_ip"`
		Purpose string `json:"purpose"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.LocalIP = strings.TrimSpace(in.LocalIP)
	if len(in.Purpose) > 600 {
		fail(w, 400, "说明过长")
		return
	}
	if strings.TrimSpace(in.Purpose) == "" {
		in.Purpose = "排查产品使用问题，命令由安全策略自动判定"
	}
	selectedIP := in.LocalIP
	if selectedIP == "" {
		if configured, parseErr := url.Parse(s.Base); parseErr == nil {
			if configured.Hostname() == "localhost" {
				selectedIP = "127.0.0.1"
			} else if ip := net.ParseIP(configured.Hostname()); ip != nil {
				selectedIP = ip.String()
			}
		}
	}
	localIP, _, err := selectLocalIP(selectedIP)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Sessions) >= 100 {
		fail(w, 429, "演示会话数量达到上限，请重启服务")
		return
	}
	sessionBase := s.baseForLocalIP(localIP)
	if err := wire.ValidateBase(sessionBase); err != nil {
		fail(w, 400, "所选 IP 不能用于当前服务地址："+err.Error())
		return
	}
	p := &Session{ID: wire.Token()[:12], View: wire.Token(), Ticket: wire.Token(), LocalIP: localIP, Base: sessionBase, Purpose: in.Purpose, Accepted: true, Created: time.Now(), Expires: time.Now().Add(s.TTL), Subscribers: map[chan struct{}]bool{}}
	s.Sessions[p.ID] = p
	s.addLocked(p, "request", "已创建临时协助请求；本机地址 "+p.LocalIP+"。命令将由黑/白名单策略自动判定。")
	jsonOut(w, 201, s.snapshot(p))
}

func (s *Server) networkCheck(w http.ResponseWriter, r *http.Request) {
	if !equal(bearer(r), s.Admin) {
		fail(w, 403, "仅支持方可检查网络地址")
		return
	}
	var in struct{}
	if !decode(w, r, &in) {
		return
	}
	_, addresses, err := selectLocalIP("")
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	available := make([]localAddress, 0, len(addresses))
	for _, address := range addresses {
		if wire.ValidateBase(s.baseForLocalIP(address.IP)) == nil {
			available = append(available, address)
		}
	}
	localIP := defaultLocalIP(available)
	if localIP == "" {
		fail(w, 400, "未找到可用于当前服务地址的本机 IP")
		return
	}
	jsonOut(w, 200, map[string]any{
		"local_ip":  localIP,
		"addresses": available,
		"note":      "请选择客户所在网络能够访问的本机 IP；实际访问还取决于服务监听地址和防火墙。",
	})
}
func (s *Server) auth(w http.ResponseWriter, r *http.Request) *Session {
	p := s.Sessions[r.PathValue("id")]
	localSupervisor := false
	if p != nil && (p.Persistent || time.Now().Before(p.Expires)) && isLoopbackRequest(r) && supervisorPathAllowed(r, p.ID) {
		if cookie, err := r.Cookie("assist_supervisor"); err == nil {
			localSupervisor = equal(cookie.Value, p.SupervisorCookie)
		}
	}
	if p == nil || (!equal(bearer(r), p.View) && !equal(bearer(r), s.Admin) && !localSupervisor) {
		fail(w, 403, "请求不存在或访问凭据无效")
		return nil
	}
	return p
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.auth(w, r); p != nil {
		jsonOut(w, 200, s.snapshotFor(r, p))
	}
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	p := s.auth(w, r)
	if p == nil {
		s.mu.Unlock()
		return
	}
	ch := make(chan struct{}, 1)
	p.Subscribers[ch] = true
	viewer := equal(bearer(r), p.View)
	if viewer {
		p.Viewers++
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(p.Subscribers, ch)
		if viewer {
			p.Viewers--
			if p.Viewers == 0 {
				p.LastViewClosed = time.Now()
			}
		}
		s.mu.Unlock()
	}()
	f, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, "SSE unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func() bool {
		s.mu.Lock()
		b, e := json.Marshal(s.snapshotFor(r, p))
		s.mu.Unlock()
		if e != nil {
			return false
		}
		if _, e = fmt.Fprintf(w, "event: state\ndata: %s\n\n", b); e != nil {
			return false
		}
		f.Flush()
		return true
	}
	if !send() {
		return
	}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if !send() {
				return
			}
		case <-tick.C:
			if _, e := io.WriteString(w, ": keepalive\n\n"); e != nil {
				return
			}
			f.Flush()
		}
	}
}
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	binaryName, extension, ok := downloadBinary(platform)
	if !ok {
		fail(w, 400, "不支持的下载平台")
		return
	}
	s.mu.Lock()
	p := s.auth(w, r)
	if p == nil {
		s.mu.Unlock()
		return
	}
	if !s.valid(p) || p.Paired {
		s.mu.Unlock()
		fail(w, 409, "申请已结束，或配对凭据已经使用")
		return
	}
	b := wire.Bootstrap{Version: 2, Base: p.Base, Ticket: p.Ticket}
	name := "assist--" + b.Encode() + extension
	if platform != "mac-app" && len(name) > 225 {
		s.mu.Unlock()
		fail(w, 400, "服务地址过长，演示版下载文件名无法承载；请使用更短的服务地址")
		return
	}
	if platform == "mac-app" {
		s.addLocked(p, "download", "已请求下载 Mac App；一次性配对信息写入应用包内，不依赖压缩包文件名。")
	} else {
		s.addLocked(p, "download", "已请求下载原生工具；程序字节不变，一次性配对信息由下载文件名携带。")
	}
	s.mu.Unlock()
	if platform == "mac-app" {
		var archive bytes.Buffer
		if err := writeMacApp(&archive, s.Assets, b.Encode()); err != nil {
			fail(w, 404, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="Assist-`+p.ID+`-macOS.zip"`)
		http.ServeContent(w, r, "Assist-macOS.zip", time.Time{}, bytes.NewReader(archive.Bytes()))
		return
	}
	if platform == "python" {
		w.Header().Set("Content-Type", "text/x-python; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		http.ServeContent(w, r, binaryName, time.Time{}, bytes.NewReader(web.MacPython))
		return
	}
	path := filepath.Join(s.Assets, binaryName)
	f, e := os.Open(path)
	if e != nil {
		fail(w, 404, "未找到对应平台的 assist 二进制，请完整解压验证包")
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		fail(w, 500, "无法读取程序")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeContent(w, r, binaryName, st.ModTime(), f)
}

func downloadBinary(platform string) (name, extension string, ok bool) {
	switch platform {
	case "", "windows":
		return "assist.exe", ".exe", true
	case "linux":
		return "assist-linux-amd64", "", true
	case "darwin-arm64":
		return "assist-darwin-arm64", "", true
	case "darwin-amd64":
		return "assist-darwin-amd64", "", true
	case "python":
		return "assist-macos.py", ".py", true
	case "mac-app":
		return "Assist-macOS.zip", ".zip", true
	default:
		return "", "", false
	}
}
func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.findTicket(bearer(r))
	if !s.valid(p) || p.Paired {
		fail(w, 410, "配对票据已用过、过期或已撤销")
		return
	}
	jsonOut(w, 200, s.grant(p))
}
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.findTicket(bearer(r))
	if !s.valid(p) || p.Paired {
		fail(w, 403, "配对被拒绝：申请无效或票据已经使用")
		return
	}
	p.Paired = true
	p.DeviceToken = wire.Token()
	g := s.grant(p)
	g.DeviceToken = p.DeviceToken
	g.WebSocket = strings.Replace(p.Base, "http", "ws", 1) + "/api/device"
	s.addLocked(p, "paired", "一次性票据已兑换，等待 WebSocket 握手。")
	jsonOut(w, 200, g)
}
func (s *Server) device(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" {
		fail(w, 403, "此接口仅接受原生执行器")
		return
	}
	s.mu.Lock()
	p := s.findDevice(bearer(r))
	if !s.valid(p) || !p.Paired || p.Online {
		s.mu.Unlock()
		fail(w, 403, "设备授权无效或已有活动连接")
		return
	}
	c, e := wire.Accept(w, r)
	if e != nil {
		s.mu.Unlock()
		fail(w, 400, e.Error())
		return
	}
	p.Conn = c
	p.Online = true
	s.mu.Unlock()
	defer func() {
		c.Close()
		s.mu.Lock()
		if p.Conn == c {
			p.Online = false
			p.Conn = nil
			if p.Running {
				p.Running = false
				if op := findOp(p, p.ActiveID); op != nil && !terminal(op.Status) {
					op.Status = "unknown"
				}
				s.addLocked(p, "unknown", "连接结束时操作仍在运行，结果未知；不会自动重发。")
			}
			s.addLocked(p, "offline", "已确认 WebSocket 关闭。未收到的远端清理结果不会标记成功。")
			if p.Persistent && !p.Revoked {
				p.Revoked = true
				s.addLocked(p, "revoked", "持续连接已断开，本次授权已撤销；重新协助需要新请求。")
			}
		}
		s.mu.Unlock()
	}()
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(12 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if c.Ping() != nil {
					c.Close()
					return
				}
			}
		}
	}()
	var m wire.Message
	if e = c.ReadJSON(&m); e != nil || m.Type != "hello" {
		return
	}
	if len(m.Host) > 200 || len(m.OS) > 40 || m.Version != "0.2.0" {
		return
	}
	s.mu.Lock()
	if !s.valid(p) {
		s.mu.Unlock()
		return
	}
	p.Device = m
	p.DeviceConnected = time.Now()
	s.addLocked(p, "connected", "真实设备回连："+m.Host+" / "+m.OS+"。已验证 WebSocket；不是浏览器模拟。")
	s.mu.Unlock()

	for {
		m = wire.Message{}
		if e = c.ReadJSON(&m); e != nil {
			return
		}
		if len(m.Text) > 8192 {
			return
		}
		s.mu.Lock()
		// A cleanup receipt is accepted even after revocation. Never label a
		// mere WebSocket disconnect as completed cleanup.
		if m.Type == "stopped" {
			p.Stopped = true
			p.Revoked = true
			s.addLocked(p, "stopped", "执行器已确认：命令处理结束、会话内存凭据释放；工具文件保留，既有改动不回滚。")
			s.mu.Unlock()
			return
		}
		s.processMessageLocked(p, m)

		s.mu.Unlock()
	}
}
func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	p := s.auth(w, r)
	if p == nil {
		s.mu.Unlock()
		return
	}
	if !p.Revoked {
		p.Revoked = true
		for _, op := range p.Operations {
			if op.Status == "queued" {
				op.Status = "cancelled"
			}
		}
		s.addLocked(p, "revoked", "网页已撤销授权，拒绝后续请求；正在关闭远端连接。")
	}
	c := p.Conn
	out := s.snapshotFor(r, p)
	s.mu.Unlock()
	if c != nil {
		c.WriteJSON(wire.Message{Type: "stop"})
		go func() { time.Sleep(5 * time.Second); c.Close() }()
	}
	jsonOut(w, 200, out)
}
func (s *Server) setPersistence(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.Sessions[r.PathValue("id")]
	if p == nil || !equal(bearer(r), p.View) {
		fail(w, 403, "仅客户页面可调整持续连接")
		return
	}
	if !s.valid(p) || !p.Online || p.Device.Type != "hello" || p.Conn == nil {
		fail(w, 409, "设备未连接或协助已结束")
		return
	}
	if p.Persistent != in.Enabled {
		expires := time.Time{}
		if !in.Enabled {
			expires = time.Now().Add(s.TTL)
		}
		p.Persistent = in.Enabled
		if !in.Enabled {
			p.Expires = expires
		}
		if in.Enabled {
			s.addLocked(p, "persistent", "客户已开启持续连接；不再按倒计时自动结束。可随时手动结束。")
		} else {
			s.addLocked(p, "timed", "客户已关闭持续连接；十分钟倒计时重新开始。")
		}
	}
	jsonOut(w, 200, s.snapshotFor(r, p))
}
func (s *Server) Sweep(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for _, p := range s.Sessions {
				if p.Conn != nil {
					p.Conn.Close()
				}
			}
			s.mu.Unlock()
			return
		case <-t.C:
			s.mu.Lock()
			for _, p := range s.Sessions {
				pageGone := false
				if p.Online && p.Device.Type == "hello" && p.Viewers == 0 {
					last := p.DeviceConnected
					if p.LastViewClosed.After(last) {
						last = p.LastViewClosed
					}
					pageGone = time.Since(last) > 20*time.Second
				}
				if !p.Revoked && ((!p.Persistent && time.Now().After(p.Expires)) || pageGone) {
					p.Revoked = true
					if pageGone {
						s.addLocked(p, "page_closed", "客户页面已关闭或失联，授权已撤销并通知执行器停止。")
					} else {
						s.addLocked(p, "expired", "授权已到期，拒绝新操作并通知执行器停止。")
					}
					if p.Conn != nil {
						c := p.Conn
						go func() { c.WriteJSON(wire.Message{Type: "stop"}); time.Sleep(5 * time.Second); c.Close() }()
					}
				}
			}
			s.mu.Unlock()
		}
	}
}
func Serve(ctx context.Context, addr string, s *Server) error {
	ln, e := net.Listen("tcp", addr)
	if e != nil {
		return e
	}
	return ServeListener(ctx, ln, s)
}
func ServeListener(ctx context.Context, ln net.Listener, s *Server) error {
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 8 * time.Second, IdleTimeout: 70 * time.Second, MaxHeaderBytes: 16384}
	go s.Sweep(ctx)
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	log.Printf("assist demo listening on %s; sessions expire in %s", ln.Addr(), s.TTL)
	e := srv.Serve(ln)
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
