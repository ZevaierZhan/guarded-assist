package broker

import (
	"assistdemo/internal/wire"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SupervisorLink grants only local status viewing and session revocation. The
// global admin credential is never included in an MCP tool result.
func (s *Server) SupervisorLink(id, localBase string) (string, error) {
	u, err := url.Parse(localBase)
	if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		return "", errors.New("监督入口必须使用本机 HTTP 地址")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.Sessions[id]
	if !s.valid(p) {
		return "", errors.New("协助会话不存在或已结束")
	}
	p.Supervisor = wire.Token()
	p.SupervisorUntil = time.Now().Add(2 * time.Minute)
	return strings.TrimRight(localBase, "/") + "/supervise/" + url.PathEscape(id) + "#s=" + p.Supervisor, nil
}

func (s *Server) exchangeSupervisor(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		fail(w, 403, "监督入口只允许本机兑换")
		return
	}
	var in struct {
		RequestID string `json:"request_id"`
		Token     string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.mu.Lock()
	p := s.Sessions[in.RequestID]
	if !s.valid(p) || !equal(in.Token, p.Supervisor) || !time.Now().Before(p.SupervisorUntil) {
		s.mu.Unlock()
		fail(w, 403, "监督链接无效或已过期，请重新获取")
		return
	}
	p.Supervisor = ""
	p.SupervisorCookie = wire.Token()
	cookie := p.SupervisorCookie
	seconds := int(time.Until(p.Expires).Seconds())
	if p.Persistent {
		seconds = 0
	}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "assist_supervisor", Value: cookie, Path: "/api/requests/" + in.RequestID, MaxAge: seconds, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	jsonOut(w, 200, map[string]any{"request_id": in.RequestID, "scope": "local-view-and-stop"})
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func supervisorPathAllowed(r *http.Request, id string) bool {
	base := "/api/requests/" + id
	return r.Method == "GET" && (r.URL.Path == base || r.URL.Path == base+"/events") ||
		r.Method == "POST" && r.URL.Path == base+"/stop"
}

func (s *Server) supervise(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		fail(w, 403, "监督页只允许在协助者本机打开")
		return
	}
	s.mu.Lock()
	p := s.Sessions[r.PathValue("id")]
	exists := p != nil
	s.mu.Unlock()
	if !exists {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(supervisorPage))
}

const supervisorPage = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Assist · 协助监督</title>
<style>body{font:15px/1.6 system-ui,sans-serif;background:#f6f7fb;color:#252738;max-width:900px;margin:40px auto;padding:0 20px}main{background:white;padding:28px;border:1px solid #dde0e9;border-radius:14px}h1{font-size:24px}button{padding:10px 15px;border:1px solid #b6bac8;border-radius:7px;background:white;cursor:pointer}button.danger{color:#b42232;border-color:#d8a4aa}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#202331;color:#e8eaf2;padding:15px;border-radius:8px;max-height:50vh;overflow:auto}#error{color:#b42232}</style>
<main><h1>协助监督</h1><p>此页面只允许查看本次协助和结束连接，不能创建请求或发送命令。</p><p id="state">正在读取…</p><p id="error"></p><button id="end" class="danger">结束协助</button><h2>命令与输出</h2><pre id="operations">等待连接</pre></main>
<script>
const id=location.pathname.split('/').pop();
const token=new URLSearchParams(location.hash.slice(1)).get('s');
history.replaceState(null,'',location.pathname);
const state=document.getElementById('state'),error=document.getElementById('error'),ops=document.getElementById('operations'),end=document.getElementById('end');
async function refresh(){try{const r=await fetch('/api/requests/'+encodeURIComponent(id),{credentials:'same-origin',cache:'no-store'});const data=await r.json();if(!r.ok)throw Error(data.error||'无法查询');
state.textContent='状态：'+data.status+' · 设备：'+(data.device?.os||'未连接')+' · '+(data.persistent?'持续连接：由客户手动结束':'到期：'+new Date(data.expires).toLocaleString())+' · 已撤销：'+(data.revoked?'是':'否')+' · 执行器停止回执：'+(data.stopped?'已收到':'未收到');
ops.textContent=(data.operations||[]).map(o=>'['+o.status+'] '+o.shell+' > '+o.command+'\n'+(o.outputs||[]).map(x=>x.text).join('')+'\nexit='+o.exit_code).join('\n\n')||'暂无命令';end.disabled=!!data.revoked;error.textContent='';
}catch(e){error.textContent=e.message}}
end.onclick=async()=>{if(!confirm('结束本次协助并撤销访问？'))return;try{const r=await fetch('/api/requests/'+encodeURIComponent(id)+'/stop',{method:'POST',credentials:'same-origin'});if(!r.ok)throw Error((await r.json()).error||'结束失败');await refresh()}catch(e){error.textContent=e.message}};
async function init(){if(!token){error.textContent='监督链接缺少凭据，请重新获取。';return}try{const r=await fetch('/api/supervisor/exchange',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({request_id:id,token})});if(!r.ok)throw Error((await r.json()).error||'链接兑换失败');await refresh();setInterval(refresh,1500)}catch(e){error.textContent=e.message}}
init();
</script></html>`
