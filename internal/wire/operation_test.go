package wire

import (
	"testing"
)

func TestCommandDigest(t *testing.T) {
	m := Message{ID: "one", Shell: "bash", Command: "date", Cwd: "/tmp", Timeout: 30}
	h := CommandHash(m)
	m.Command = "pwd"
	if h == CommandHash(m) {
		t.Fatal("command change not bound")
	}
	m.Command = "date"
	m.Cwd = "/"
	if h == CommandHash(m) {
		t.Fatal("cwd not bound")
	}
}
func TestCommandValidation(t *testing.T) {
	for _, m := range []Message{{Shell: "cmd", Command: "echo", Timeout: 30}, {Shell: "bash", Command: "", Timeout: 30}, {Shell: "bash", Command: "date", Timeout: 90}, {Shell: "bash", Command: "date\x00", Timeout: 30}} {
		if ValidateCommand(m) == nil {
			t.Fatal(m)
		}
	}
}
