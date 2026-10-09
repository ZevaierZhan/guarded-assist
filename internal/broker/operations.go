package broker

import (
	"assistdemo/internal/wire"
	"fmt"
	"net/http"
	"time"
)

type Output struct {
	Stream string `json:"stream"`
	Text   string `json:"text"`
	At     string `json:"at"`
}
type Operation struct {
	ID         string   `json:"id"`
	Shell      string   `json:"shell"`
	Command    string   `json:"command"`
	Cwd        string   `json:"cwd"`
	Timeout    int      `json:"timeout"`
	Hash       string   `json:"hash"`
	Status     string   `json:"status"`
	Created    string   `json:"created"`
	Outputs    []Output `json:"outputs"`
	ExitCode   *int     `json:"exit_code"`
	DurationMS int64    `json:"duration_ms"`
	Policy     string   `json:"policy"`
	Bytes      int      `json:"-"`
}

func terminal(s string) bool {
	switch s {
	case "completed", "failed", "cancelled", "blocked", "timed_out", "unknown":
		return true
	}
	return false
}
func findOp(p *Session, id string) *Operation {
	for _, o := range p.Operations {
		if o.ID == id {
			return o
		}
	}
	return nil
}
func (o *Operation) message() wire.Message {
	return wire.Message{Type: "exec", ID: o.ID, Shell: o.Shell, Command: o.Command, Cwd: o.Cwd, Timeout: o.Timeout, Hash: o.Hash}
}
func (s *Server) command(w http.ResponseWriter, r *http.Request) {
	if !equal(bearer(r), s.Admin) {
		fail(w, 403, "只有支持方可以提交命令；开发入口并不等于权限")
		return
	}
	var in struct {
		Shell   string `json:"shell"`
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
		Timeout int    `json:"timeout"`
	}
	if !decode(w, r, &in) {
		return
	}
	m := wire.Message{Shell: in.Shell, Command: in.Command, Cwd: in.Cwd, Timeout: in.Timeout}
	if e := wire.ValidateCommand(m); e != nil {
		fail(w, 400, e.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.Sessions[r.PathValue("id")]
	if !s.valid(p) || !p.Online || p.Device.Type != "hello" {
		fail(w, 409, "客户设备未就绪或申请已结束")
		return
	}
	if len(p.Operations) >= 30 {
		fail(w, 429, "演示会话最多 30 条命令")
		return
	}
	for _, op := range p.Operations {
		if !terminal(op.Status) {
			fail(w, 409, "请先完成或取消当前命令，避免并发修改")
			return
		}
	}
	found := false
	for _, sh := range p.Device.Shells {
		if sh == in.Shell {
			found = true
		}
	}
	if !found {
		fail(w, 400, "目标机没有这个 Shell；不会自动安装")
		return
	}
	m.ID = wire.Token()[:12]
	decision := wire.EvaluateCommandPolicy(m.Shell, m.Command)
	if decision.Allowed && p.Conn == nil {
		fail(w, 409, "客户设备连接尚未就绪")
		return
	}
	status := "blocked"
	if decision.Allowed {
		status = "queued"
	}
	op := &Operation{ID: m.ID, Shell: m.Shell, Command: m.Command, Cwd: m.Cwd, Timeout: m.Timeout, Hash: wire.CommandHash(m), Status: status, Policy: decision.Rule, Created: time.Now().Format(time.RFC3339Nano), Outputs: []Output{}}
	p.Operations = append(p.Operations, op)
	if !decision.Allowed {
		s.addLocked(p, "blocked", "命令未命中白名单或命中黑名单，策略已自动阻断。")
		cp := *op
		jsonOut(w, 201, cp)
		return
	}
	p.Running = true
	p.RunCount++
	p.ActiveID = op.ID
	c := p.Conn
	s.addLocked(p, "policy_allowed", "命令命中诊断白名单，已自动下发执行。")
	if e := c.WriteJSON(op.message()); e != nil {
		op.Status = "unknown"
		p.Running = false
		s.addLocked(p, "unknown", "下发未确认，不自动重发。")
		fail(w, 502, "发送结果未知")
		return
	}
	cp := *op
	jsonOut(w, 201, cp)
}
func (s *Server) cancelCommand(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	p := s.auth(w, r)
	if p == nil {
		s.mu.Unlock()
		return
	}
	o := findOp(p, r.PathValue("op"))
	if o == nil || terminal(o.Status) {
		s.mu.Unlock()
		fail(w, 409, "没有可以停止的操作")
		return
	}
	c := p.Conn
	if o.Status == "queued" {
		o.Status = "cancelled"
		s.addLocked(p, "cancelled", "未执行的命令已取消。")
		s.mu.Unlock()
		jsonOut(w, 200, map[string]string{"status": "cancelled"})
		return
	}
	o.Status = "cancelling"
	s.addLocked(p, "cancelling", "已请求停止，等待执行器回执。")
	id := o.ID
	s.mu.Unlock()
	if c != nil {
		c.WriteJSON(wire.Message{Type: "cancel", ID: id})
	}
	jsonOut(w, 202, map[string]string{"status": "cancelling"})
}
func (s *Server) processMessageLocked(p *Session, m wire.Message) {
	op := findOp(p, m.ID)
	if op == nil || m.ID != p.ActiveID || terminal(op.Status) {
		return
	}
	switch m.Type {
	case "started":
		op.Status = "running"
		s.addLocked(p, "started", "策略校验通过，命令开始执行。")
	case "output":
		if op.Bytes+len(m.Text) > 65536 {
			return
		}
		op.Bytes += len(m.Text)
		op.Outputs = append(op.Outputs, Output{m.Stream, m.Text, time.Now().Format(time.RFC3339Nano)})
		if len(op.Outputs) > 1000 {
			op.Outputs = op.Outputs[len(op.Outputs)-1000:]
		}
		s.addLocked(p, "output", m.Text)
	case "result":
		code := m.ExitCode
		op.ExitCode = &code
		op.DurationMS = m.DurationMS
		op.Status = m.Status
		if !terminal(op.Status) {
			if code == 0 {
				op.Status = "completed"
			} else {
				op.Status = "failed"
			}
		}
		p.Running = false
		s.addLocked(p, "result", fmt.Sprintf("命令已结束 · %s · exit=%d · %d ms", op.Status, code, m.DurationMS))
	case "blocked":
		op.Status = "blocked"
		p.Running = false
		s.addLocked(p, "blocked", "本机策略拒绝该命令。")
	case "error":
		op.Status = "failed"
		p.Running = false
		s.addLocked(p, "error", m.Text)
	}
}
