package main

import (
	"assistdemo/internal/client"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
)

func main() {
	client.PrepareConsole()
	args := os.Args[1:]
	if len(args) == 2 && args[0] == "--confirm-console" && len(args[1]) < 24000 {
		b, e := base64.RawURLEncoding.DecodeString(args[1])
		parts := strings.SplitN(string(b), "\n", 2)
		if e == nil && len(parts) == 2 && client.Confirm(parts[0], parts[1]) {
			os.Exit(0)
		}
		os.Exit(1)
	}
	if len(args) == 1 && args[0] == "--confirm-local" {
		b, _ := io.ReadAll(io.LimitReader(os.Stdin, 16384))
		parts := strings.SplitN(string(b), "\n", 2)
		if len(parts) == 2 && client.Confirm(parts[0], parts[1]) {
			os.Exit(0)
		}
		os.Exit(1)
	}
	if len(args) == 1 && args[0] == "--uninstall" {
		if e := client.Uninstall(); e != nil {
			client.Notify("assist", e.Error())
			os.Exit(1)
		}
		client.Notify("assist", "移除完成。原下载文件仍保留，可手动删除。")
		return
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("assist " + client.Version)
		return
	}
	// A protocol URI is accepted ONLY as the single complete argument. This
	// prevents URI-to-command-line quote injection from adding CLI switches.
	input := ""
	o := client.Options{}
	if len(args) == 1 && strings.HasPrefix(args[0], "assist://join/") {
		input = args[0]
	} else if len(args) == 0 {
		input, _ = os.Executable()
	} else {
		client.Notify("assist", "参数无效。请通过申请页启动。协议 URL 必须是唯一参数。")
		os.Exit(2)
	}
	b, e := client.ParseInput(input)
	if e != nil && len(args) == 0 && runtime.GOOS != "windows" {
		client.Notify("assist", "请使用当前请求页下载的原文件名启动，或将 assist:// URI 作为唯一参数传入。此平台验证版不注册桌面协议。")
		os.Exit(1)
	}
	if e != nil && len(args) == 0 {
		if client.Confirm("assist · 注册网页唤起", "这是通用工具，没有携带申请信息。\n\n是否只进行当前用户级安装并注册 assist://？\n之后回到已接受的网页，点击“打开 assist”即可。\n不需要管理员、Python 或 Node；不会设置开机启动。") {
			if _, e = client.Install(); e == nil {
				client.Notify("assist", "已完成。回到请求页点击“打开 assist”；命令将由安全策略自动判定。")
				return
			}
		}
	}
	if e != nil {
		client.Notify("assist", e.Error())
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if e = client.Run(ctx, b, o); e != nil {
		client.Notify("assist · 未连接", e.Error())
		os.Exit(1)
	}
}
