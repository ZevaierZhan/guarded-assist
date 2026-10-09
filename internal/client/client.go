package client

import (
	"assistdemo/internal/wire"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const Version = "0.2.0"

var filenameRE = regexp.MustCompile(`^assist--([A-Za-z0-9_-]+?)(?: \([0-9]+\))?(?:\.exe)?$`)

func ParseInput(arg string) (wire.Bootstrap, error) {
	if strings.HasPrefix(arg, "assist://join/") {
		return wire.Decode(strings.TrimPrefix(arg, "assist://join/"))
	}
	m := filenameRE.FindStringSubmatch(filepath.Base(arg))
	if m == nil {
		return wire.Bootstrap{}, errors.New("没有配对信息：请从当前申请页重新下载，或使用网页上的‘打开 assist’；不要重命名下载文件")
	}
	return wire.Decode(m[1])
}
func httpJSON(ctx context.Context, method, endpoint, token string, out any) error {
	req, e := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	h := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("拒绝认证请求跳转") }}
	res, e := h.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 16384))
	if e != nil {
		return e
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, b)
	}
	return json.Unmarshal(b, out)
}

type Options struct{ NoInstall bool }

func Run(parent context.Context, b wire.Bootstrap, o Options) error {
	if e := wire.ValidateBase(b.Base); e != nil {
		return e
	}
	var g wire.Grant
	var initial error
	for i := 0; i < 5; i++ {
		initial = httpJSON(parent, "GET", b.Base+"/api/bootstrap", b.Ticket, &g)
		if initial == nil || !strings.Contains(initial.Error(), "HTTP 409") {
			break
		}
		time.Sleep(400 * time.Millisecond)
	}
	if initial != nil {
		return initial
	}
	if g.Scope != "policy-shell-v3" || time.Until(g.Expires) <= 0 {
		return errors.New("授权类型或期限无效；请使用最新 v0.2.0 工具")
	}
	registered := false
	if !o.NoInstall {
		var e error
		registered, e = Install()
		if e != nil {
			return e
		}
	}
	bootstrapGrant := g
	if e := httpJSON(parent, "POST", b.Base+"/api/pair", b.Ticket, &g); e != nil {
		return e
	}
	if g.RequestID != bootstrapGrant.RequestID || g.Scope != bootstrapGrant.Scope || !g.Expires.Equal(bootstrapGrant.Expires) || len(g.DeviceToken) != 32 {
		return errors.New("配对权限与初始策略不一致")
	}
	expected := strings.Replace(b.Base, "http", "ws", 1) + "/api/device"
	if g.WebSocket != expected {
		return errors.New("WebSocket 来源不一致")
	}
	until := g.Expires
	if max := time.Now().Add(10 * time.Minute); until.After(max) {
		until = max
	}
	ctx, cancel := context.WithDeadline(parent, until)
	defer cancel()
	c, e := wire.Dial(g.WebSocket, g.DeviceToken)
	if e != nil {
		return e
	}
	defer c.Close()
	// A Windows job ties child processes to assist lifetime. It is not a sandbox.
	release, e := GuardLifetime()
	if e != nil {
		return fmt.Errorf("无法建立进程生命周期保护，已拒绝执行：%w", e)
	}
	defer release()
	host, _ := os.Hostname()
	shells := AvailableShells()
	home, _ := os.UserHomeDir()
	c.WriteJSON(wire.Message{Type: "hello", Host: host, OS: runtime.GOOS + "/" + runtime.GOARCH, Version: Version, PID: os.Getpid(), ProtocolRegistered: registered, Shells: shells, Cwd: home})
	fmt.Printf("assist %s | %s\n已连接 %s\n命令由黑/白名单策略自动判定；Ctrl+C / 关闭本窗口停止。\n", Version, runtime.GOOS, b.Base)
	incoming := make(chan wire.Message, 8)
	readErr := make(chan error, 1)
	go func() {
		for {
			var m wire.Message
			if e := c.ReadJSON(&m); e != nil {
				readErr <- e
				return
			}
			select {
			case incoming <- m:
			case <-ctx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(12 * time.Second)
	defer ticker.Stop()
	type completed struct {
		id  string
		msg wire.Message
	}
	finished := make(chan completed, 1)
	var opCancel context.CancelFunc
	active := ""
	seen := map[string]bool{}
	cancelledIDs := map[string]bool{}
	var wg sync.WaitGroup
	defer func() {
		if opCancel != nil {
			opCancel()
		}
		cancel()
		wg.Wait()
		// At most one operation can be active. Flush its actual result before
		// reporting that the executor has stopped handling this session.
		select {
		case done := <-finished:
			_ = c.WriteJSON(done.msg)
		default:
		}
		_ = c.WriteJSON(wire.Message{Type: "stopped"})
		fmt.Println("连接结束；本次凭据未写入磁盘。已执行的修改不会回滚。")
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-readErr:
			return nil
		case <-ticker.C:
			if c.Ping() != nil {
				return nil
			}
		case done := <-finished:
			if active == done.id {
				c.WriteJSON(done.msg)
				active = ""
				opCancel = nil
			}
		case m := <-incoming:
			switch m.Type {
			case "stop":
				return nil
			case "cancel":
				cancelledIDs[m.ID] = true
				if m.ID == active && opCancel != nil {
					opCancel()
				}
			case "exec":
				if cancelledIDs[m.ID] {
					c.WriteJSON(wire.Message{Type: "result", ID: m.ID, Status: "cancelled", ExitCode: -1})
					continue
				}
				if active != "" || seen[m.ID] || len(seen) >= 30 || m.ID == "" || m.Hash != wire.CommandHash(m) {
					c.WriteJSON(wire.Message{Type: "error", ID: m.ID, Text: "本机拒绝：并发、重复或命令摘要不匹配"})
					continue
				}
				if e := wire.ValidateCommand(m); e != nil {
					c.WriteJSON(wire.Message{Type: "error", ID: m.ID, Text: e.Error()})
					continue
				}
				found := false
				for _, sh := range shells {
					if sh == m.Shell {
						found = true
					}
				}
				if !found {
					c.WriteJSON(wire.Message{Type: "error", ID: m.ID, Text: "目标机不支持此 Shell"})
					continue
				}
				decision := wire.EvaluateCommandPolicy(m.Shell, m.Command)
				if !decision.Allowed {
					seen[m.ID] = true
					c.WriteJSON(wire.Message{Type: "blocked", ID: m.ID, Text: decision.Rule})
					continue
				}
				seen[m.ID] = true
				active = m.ID
				opCtx, stop := context.WithCancel(ctx)
				opCancel = stop
				wg.Add(1)
				go func(m wire.Message) {
					defer wg.Done()
					defer stop()
					cwd := m.Cwd
					if cwd == "" {
						cwd = home
					}
					if opCtx.Err() != nil {
						finished <- completed{m.ID, wire.Message{Type: "result", ID: m.ID, Status: "cancelled", ExitCode: -1}}
						return
					}
					c.WriteJSON(wire.Message{Type: "started", ID: m.ID})
					result := ExecuteShell(opCtx, m, func(stream, text string) {
						fmt.Print(text)
						c.WriteJSON(wire.Message{Type: "output", ID: m.ID, Stream: stream, Text: text})
					})
					finished <- completed{m.ID, wire.Message{Type: "result", ID: m.ID, Status: result.Status, ExitCode: result.ExitCode, DurationMS: result.DurationMS}}
				}(m)
			default:
				c.WriteJSON(wire.Message{Type: "error", ID: m.ID, Text: "不支持的消息"})
			}
		}
	}
}
