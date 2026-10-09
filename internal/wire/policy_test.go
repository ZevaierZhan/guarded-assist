package wire

import "testing"

func TestCommandPolicy(t *testing.T) {
	tests := []struct {
		shell, command string
		allowed        bool
	}{
		{"powershell", "Get-Date\nGet-Location", true},
		{"powershell", "Remove-Item C:\\temp\\x -Recurse", false},
		{"bash", "date\npwd", true},
		{"bash", "rm -rf ./cache", false},
		{"bash", "python deploy.py", false},
	}
	for _, tt := range tests {
		if got := EvaluateCommandPolicy(tt.shell, tt.command); got.Allowed != tt.allowed {
			t.Fatalf("%s %q: allowed=%v rule=%s", tt.shell, tt.command, got.Allowed, got.Rule)
		}
	}
}
