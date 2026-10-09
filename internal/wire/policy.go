package wire

import (
	"regexp"
	"strings"
)

// PolicyDecision is intentionally small enough to be evaluated independently
// by both the broker and the native executor. The broker provides fast product
// feedback; the executor is the final enforcement point.
type PolicyDecision struct {
	Allowed bool
	Rule    string
}

var blockedCommandPatterns = []struct {
	rule string
	re   *regexp.Regexp
}{
	{"block/destructive-files", regexp.MustCompile(`(?i)(^|[\s;|&])(rm\s+-|rmdir\b|del\s+[/\\-]|erase\b|remove-item\b|clear-content\b|format(?:-volume)?\b|mkfs\b|diskpart\b|dd\s+if=)`)},
	{"block/privilege-or-identity", regexp.MustCompile(`(?i)(^|[\s;|&])(sudo\b|su\s+-|runas\b|net\s+user\b|useradd\b|userdel\b|passwd\b|chmod\b|chown\b|set-acl\b)`)},
	{"block/system-control", regexp.MustCompile(`(?i)(^|[\s;|&])(shutdown\b|reboot\b|poweroff\b|restart-computer\b|stop-computer\b|sc(?:\.exe)?\s+(?:create|delete|config)\b|systemctl\s+(?:enable|disable|mask)\b)`)},
	{"block/code-download-or-eval", regexp.MustCompile(`(?i)(invoke-expression\b|\biex\b|downloadstring\b|frombase64string\b|curl\b[^\r\n|]*\|\s*(?:sh|bash|pwsh|powershell)\b|wget\b[^\r\n|]*\|\s*(?:sh|bash|pwsh|powershell)\b)`)},
	{"block/registry-or-firewall-write", regexp.MustCompile(`(?i)(reg(?:\.exe)?\s+(?:add|delete|import)\b|new-itemproperty\b|set-itemproperty\b|remove-itemproperty\b|netsh\s+advfirewall\b)`)},
}

var allowedPowerShell = regexp.MustCompile(`(?i)^\s*(?:get-[a-z0-9_-]+|test-[a-z0-9_-]+|resolve-[a-z0-9_-]+|measure-[a-z0-9_-]+|select-[a-z0-9_-]+|where-(?:object)?|sort-(?:object)?|format-(?:table|list|wide|custom)|out-string|write-(?:output|host|warning|verbose|information)|start-sleep|compare-object|convertto-json|[[]console[]]::(?:write|writeline|error\.writeline)|exit\b)`)
var allowedBash = regexp.MustCompile(`(?i)^\s*(?:date|pwd|whoami|uname|id|hostname|printf|echo|ls|cat|head|tail|grep|ps|df|du|env|printenv|which|type|stat|ip|ifconfig|netstat|ss|nslookup|dig|ping|sleep|true|false|exit)\b`)

// EvaluateCommandPolicy implements a deny-by-default diagnostic policy. Every
// non-empty statement must be allowlisted, while the blocklist always wins.
// This is deliberately not a shell parser; production policy should use
// structured tools rather than accepting arbitrary shell text.
func EvaluateCommandPolicy(shell, command string) PolicyDecision {
	for _, item := range blockedCommandPatterns {
		if item.re.MatchString(command) {
			return PolicyDecision{Rule: item.rule}
		}
	}
	allow := allowedPowerShell
	if shell == "bash" {
		allow = allowedBash
	}
	statements := regexp.MustCompile(`[\r\n;]+`).Split(command, -1)
	for _, statement := range statements {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if !allow.MatchString(statement) {
			return PolicyDecision{Rule: "default/not-allowlisted"}
		}
	}
	return PolicyDecision{Allowed: true, Rule: "allow/diagnostics"}
}
