package main

import (
	"assistdemo/internal/broker"
	"assistdemo/internal/wire"
	"assistdemo/web"
	"context"
	"flag"
	"fmt"
	"log"
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
