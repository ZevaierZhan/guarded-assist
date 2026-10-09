//go:build !windows

package client

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func Confirm(title, text string) bool {
	fmt.Printf("%s\n%s\nType YES to allow: ", title, text)
	s, _ := consoleInput.ReadString('\n')
	return strings.TrimSpace(s) == "YES"
}
func Notify(title, text string)     { fmt.Println(title + "\n" + text) }
func ConfigureChild(c *exec.Cmd)    {}
func DecodeConsole(b []byte) string { return strings.ToValidUTF8(string(b), "�") }
func Install() (bool, error)        { return false, nil }
func Uninstall() error {
	fmt.Println("此平台验证版未注册桌面协议或安装任何服务。删除可执行文件即可。")
	return nil
}
func PingCommand(target string) (string, []string) {
	p, e := exec.LookPath("ping")
	if e != nil {
		return "", nil
	}
	return p, []string{"-c", "3", target}
}

func PrepareConsole() {}

var consoleInput = bufio.NewReader(os.Stdin)

func ConfirmOperation(ctx context.Context, title, text string) bool {
	exe, e := os.Executable()
	if e != nil {
		return false
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte(title + "\n" + text))
	c := exec.CommandContext(ctx, exe, "--confirm-console", payload)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run() == nil
}
func AvailableShells() []string {
	if _, e := exec.LookPath("bash"); e == nil {
		return []string{"bash"}
	}
	return []string{}
}
func ShellCommand(shell, command string) (string, []string, error) {
	if shell != "bash" {
		return "", nil, fmt.Errorf("目标机仅提供 Bash")
	}
	p, e := exec.LookPath("bash")
	return p, []string{"--noprofile", "--norc", "-c", command}, e
}
func ConfigureShell(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
}
func GuardLifetime() (func(), error) { return func() {}, nil }
