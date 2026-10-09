package wire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

func ValidateCommand(m Message) error {
	if m.Shell != "powershell" && m.Shell != "bash" {
		return errors.New("仅支持 PowerShell / Bash")
	}
	if len(m.Command) == 0 || len(m.Command) > 4096 || !utf8.ValidString(m.Command) || strings.ContainsRune(m.Command, 0) {
		return errors.New("命令必须为有效 UTF-8，长度 1–4096 字节，不含 NUL")
	}
	if strings.TrimSpace(m.Command) == "" || len(m.Cwd) > 1024 || strings.ContainsRune(m.Cwd, 0) {
		return errors.New("命令或目录无效")
	}
	if m.Timeout < 1 || m.Timeout > 60 {
		return errors.New("超时范围为 1–60 秒")
	}
	return nil
}
func CommandHash(m Message) string {
	b, _ := json.Marshal([]any{m.ID, m.Shell, m.Command, m.Cwd, m.Timeout})
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:])
}
