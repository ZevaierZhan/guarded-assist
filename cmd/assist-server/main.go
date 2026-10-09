package main

import (
	"assistdemo/internal/broker"
	"assistdemo/internal/control"
	"assistdemo/internal/mcpserver"
	"assistdemo/internal/wire"
	"assistdemo/web"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"
)

func main() {
	exe, _ := os.Executable()
	addr := flag.String("listen", "127.0.0.1:18777", "监听地址；默认仅本机")
	public := flag.String("public", "http://127.0.0.1:18777", "浏览器和客户访问的原点；私有局域网 IP 可使用 HTTP")
	assets := flag.String("assets", filepath.Dir(exe), "assist 下载文件所在目录")
	pagePath := flag.String("page", filepath.Join(filepath.Dir(exe), "index.html"), "可选外部页面；不存在时使用内嵌 HTML")
	noOpen := flag.Bool("no-open", false, "不自动打开浏览器")
	admin := flag.String("admin-token", "", "仅用于自动测试；默认随机生成")
	mcpStdio := flag.Bool("mcp-stdio", false, "以 stdio MCP 模式运行并同时启动本地协助服务")
	controlFile := flag.String("control-file", "", "写入本机 CLI 连接文件；启用独立回环控制接口")
	flag.Parse()
	if e := wire.ValidateBase(*public); e != nil {
		log.Fatal(e)
	}
	page := web.Page
	if b, e := os.ReadFile(*pagePath); e == nil {
		page = b
	} else if !os.IsNotExist(e) {
		log.Fatal(e)
	}
	s := broker.New(*public, *assets, page)
	if *admin != "" {
		s.Admin = *admin
	}
	if *mcpStdio {
		if *controlFile != "" {
			log.Fatal("--control-file 与 --mcp-stdio 不能同时使用")
		}
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			log.Fatal(err)
		}
		bound := ln.Addr().(*net.TCPAddr)
		if !bound.IP.IsUnspecified() && !bound.IP.IsLoopback() {
			ln.Close()
			log.Fatal("MCP 监督页需要回环地址；--listen 请使用 0.0.0.0、[::] 或回环地址")
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		loopback := "127.0.0.1"
		if bound.IP.To4() == nil {
			loopback = "[::1]"
		}
		localBase := fmt.Sprintf("http://%s:%d", loopback, bound.Port)
		s.SupervisorOrigin = localBase
		go func() {
			if err := broker.ServeListener(ctx, ln, s); err != nil {
				log.Print(err)
				cancel()
			}
		}()
		if err := (&mcpserver.Server{Broker: s, LocalBase: localBase}).Run(os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *controlFile != "" {
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			log.Fatal(err)
		}
		defer ln.Close()
		u, _ := url.Parse(s.Base)
		if u.Port() == "0" {
			u.Host = net.JoinHostPort(u.Hostname(), fmt.Sprint(ln.Addr().(*net.TCPAddr).Port))
			s.Base = u.String()
		}
		localBase := fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port)
		s.SupervisorOrigin = localBase
		local, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatal(err)
		}
		defer local.Close()
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		api := &mcpserver.Server{Broker: s, LocalBase: localBase}
		state := control.State{Endpoint: "http://" + local.Addr().String(), Token: wire.Token(), PID: os.Getpid()}
		data, _ := json.Marshal(state)
		if err := os.MkdirAll(filepath.Dir(*controlFile), 0700); err != nil {
			log.Fatal(err)
		}
		f, err := os.OpenFile(*controlFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(*controlFile)
		if _, err = f.Write(data); err != nil {
			f.Close()
			log.Fatal(err)
		}
		f.Close()
		localHTTP := &http.Server{Handler: control.Handler(api, state.Token, cancel), ReadHeaderTimeout: 5 * time.Second}
		controlStopped := make(chan struct{})
		go func() {
			defer close(controlStopped)
			<-ctx.Done()
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer stopCancel()
			localHTTP.Shutdown(stopCtx)
		}()
		go func() {
			if err := localHTTP.Serve(local); err != nil && err != http.ErrServerClosed {
				log.Print(err)
				cancel()
			}
		}()
		if err := broker.ServeListener(ctx, ln, s); err != nil {
			log.Print(err)
		}
		cancel()
		<-controlStopped
		return
	}
	link := s.Base + "/#admin=" + s.Admin
	fmt.Printf("\nASSIST · 原生连接验证 0.2.0\n\n管理页（只给自己）：\n%s\n\n真实 Shell 验证：命令由黑/白名单策略自动判定，未知命令默认阻断。Ctrl+C 退出服务。\n状态保存在内存，重启后旧请求全部失效。\n\n", link)
	if !*noOpen {
		go func() { time.Sleep(600 * time.Millisecond); open(link) }()
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if e := broker.Serve(ctx, *addr, s); e != nil {
		log.Fatal(e)
	}
}
func open(u string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "rundll32.exe"), "url.dll,FileProtocolHandler", u)
	case "darwin":
		c = exec.Command("open", u)
	default:
		c = exec.Command("xdg-open", u)
	}
	c.Run()
}
