//go:build windows

package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"
)

var user32 = syscall.NewLazyDLL("user32.dll")
var messageBox = user32.NewProc("MessageBoxW")

func Confirm(title, text string) bool {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	r, _, _ := messageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x00000004|0x00000020|0x00000100|0x00010000)
	return r == 6
}
func Notify(title, text string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	messageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x40|0x10000)
}
func ConfigureChild(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
func PingCommand(target string) (string, []string) {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE"), []string{"-n", "3", "-w", "2000", target}
}
func DecodeConsole(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	if len(b) == 0 {
		return ""
	}
	k := syscall.NewLazyDLL("kernel32.dll")
	cp, _, _ := k.NewProc("GetOEMCP").Call()
	f := k.NewProc("MultiByteToWideChar")
	n, _, _ := f.Call(cp, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 {
		return string(b)
	}
	out := make([]uint16, n)
	f.Call(cp, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&out[0])), n)
	return syscall.UTF16ToString(out)
}

const registryKey = `HKCU\Software\Classes\assist`
const ownerMarker = "assist-demo-v1"

func reg(args ...string) ([]byte, error) {
	c := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "reg.exe"), args...)
	ConfigureChild(c)
	return c.CombinedOutput()
}
func installedPath() (string, error) {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return "", errors.New("LOCALAPPDATA 不可用")
	}
	return filepath.Join(root, "AssistDemo", "assist.exe"), nil
}
func Install() (bool, error) {
	// Never take over a pre-existing, unrelated assist:// handler.
	if _, e := reg("query", registryKey); e == nil {
		out, e := reg("query", registryKey, "/v", "AssistDemoOwner")
		if e != nil || !bytes.Contains(out, []byte(ownerMarker)) {
			return false, errors.New("assist:// 已由其他软件注册；本 demo 不会覆盖，请改用独立协议名后构建")
		}
	}
	// A machine-wide handler must not be shadowed by this per-user demo.
	if _, localErr := reg("query", registryKey); localErr != nil {
		if _, mergedErr := reg("query", `HKCR\assist`); mergedErr == nil {
			return false, errors.New("assist:// 已被系统级软件注册，本 demo 不会覆盖")
		}
	}
	dst, e := installedPath()
	if e != nil {
		return false, e
	}
	src, e := os.Executable()
	if e != nil {
		return false, e
	}
	if !strings.EqualFold(src, dst) {
		b, e := os.ReadFile(src)
		if e != nil {
			return false, e
		}
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return false, e
		}
		old, _ := os.ReadFile(dst)
		if !bytes.Equal(b, old) {
			if e = os.WriteFile(dst, b, 0700); e != nil {
				return false, e
			}
		}
	}
	values := [][]string{
		{"add", registryKey, "/ve", "/t", "REG_SZ", "/d", "URL:assist Demo", "/f"},
		{"add", registryKey, "/v", "URL Protocol", "/t", "REG_SZ", "/d", "", "/f"},
		{"add", registryKey, "/v", "AssistDemoOwner", "/t", "REG_SZ", "/d", ownerMarker, "/f"},
		{"add", registryKey + `\shell\open\command`, "/ve", "/t", "REG_SZ", "/d", `"` + dst + `" "%1"`, "/f"},
	}
	for _, a := range values {
		if out, e := reg(a...); e != nil {
			return false, fmt.Errorf("注册失败：%s (%w)", DecodeConsole(out), e)
		}
	}
	return true, nil
}
func Uninstall() error {
	dst, e := installedPath()
	if e != nil {
		return e
	}
	if !Confirm("assist · 移除用户级配置", "将删除本 demo 的 assist:// 注册和以下文件：\n"+dst+"\n\n请先在页面结束所有连接。不会删除下载文件。\n继续吗？") {
		return errors.New("已取消")
	}
	out, e := reg("query", registryKey, "/v", "AssistDemoOwner")
	if e == nil && bytes.Contains(out, []byte(ownerMarker)) {
		if _, e = reg("delete", registryKey, "/f"); e != nil {
			return e
		}
	} else if _, exists := reg("query", registryKey); exists == nil {
		return errors.New("协议不属于本 demo，不会删除")
	}
	if e = os.Remove(dst); e != nil && !os.IsNotExist(e) {
		return fmt.Errorf("协议已撤销，但文件仍在使用。请关闭 assist 后从验证包重新运行卸载：%w", e)
	}
	os.Remove(filepath.Dir(dst))
	return nil
}

func PrepareConsole() { syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleOutputCP").Call(65001) }

// Separate native consent dialog process can be cancelled on timeout/revocation.
func ConfirmOperation(ctx context.Context, title, text string) bool {
	exe, e := os.Executable()
	if e != nil {
		return false
	}
	c := exec.CommandContext(ctx, exe, "--confirm-local")
	c.Stdin = strings.NewReader(title + "\n" + text)
	ConfigureChild(c)
	return c.Run() == nil
}
func AvailableShells() []string {
	p := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, e := os.Stat(p); e == nil {
		return []string{"powershell"}
	}
	return []string{}
}
func ShellCommand(shell, command string) (string, []string, error) {
	if shell != "powershell" {
		return "", nil, errors.New("本 Windows 工具仅提供系统 PowerShell，不自动安装 Bash")
	}
	p := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	prefix := "[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false); $OutputEncoding = [Console]::OutputEncoding; $ProgressPreference = 'SilentlyContinue';\n"
	return p, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-OutputFormat", "Text", "-Command", prefix + command}, nil
}
func ConfigureShell(c *exec.Cmd) {
	ConfigureChild(c)
	c.Cancel = func() error {
		if c.Process == nil {
			return nil
		}
		kill := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe"), "/PID", fmt.Sprint(c.Process.Pid), "/T", "/F")
		ConfigureChild(kill)
		_ = kill.Run()
		return c.Process.Kill()
	}
}

// Job object closes with assist, terminating normal descendants. No privilege or
// sandbox guarantee: an authorised command still runs as the logged-in user.
func GuardLifetime() (func(), error) {
	k := syscall.NewLazyDLL("kernel32.dll")
	create := k.NewProc("CreateJobObjectW")
	set := k.NewProc("SetInformationJobObject")
	assign := k.NewProc("AssignProcessToJobObject")
	current := k.NewProc("GetCurrentProcess")
	h, _, e := create.Call(0, 0)
	if h == 0 {
		return nil, e
	}
	type basic struct {
		PerProcessUserTimeLimit, PerJobUserTimeLimit int64
		LimitFlags                                   uint32
		MinimumWorkingSetSize, MaximumWorkingSetSize uintptr
		ActiveProcessLimit                           uint32
		Affinity                                     uintptr
		PriorityClass, SchedulingClass               uint32
	}
	type ioCounters struct{ ReadOperationCount, WriteOperationCount, OtherOperationCount, ReadTransferCount, WriteTransferCount, OtherTransferCount uint64 }
	type extended struct {
		Basic                                                                        basic
		IO                                                                           ioCounters
		ProcessMemoryLimit, JobMemoryLimit, PeakProcessMemoryUsed, PeakJobMemoryUsed uintptr
	}
	limits := extended{}
	limits.Basic.LimitFlags = 0x2000
	ok, _, e := set.Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, e
	}
	proc, _, _ := current.Call()
	ok, _, e = assign.Call(h, proc)
	if ok == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		return nil, e
	}
	// Keep the handle alive until OS process exit. Closing it inside a defer would
	// terminate assist itself (which belongs to the job) before returning normally.
	return func() {}, nil
}
