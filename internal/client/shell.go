package client

import (
	"assistdemo/internal/wire"
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"
	"unicode/utf8"
)

type ShellResult struct {
	ExitCode   int
	DurationMS int64
	Status     string
}

// A shared output budget across stdout/stderr; chunks stream without waiting for
// newlines. Invalid bytes are visibly replaced; no HTML or ANSI is interpreted.
type outputBudget struct {
	mu     sync.Mutex
	used   int
	warned bool
	emit   func(string, string)
}
type chunkWriter struct {
	b       *outputBudget
	stream  string
	pending []byte
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	p = append(w.pending, p...)
	w.pending = nil
	for len(p) > 0 {
		if !utf8.FullRune(p) {
			w.pending = append([]byte{}, p...)
			break
		}
		_, size := utf8.DecodeRune(p)
		if size == 1 && p[0] >= 128 {
			size = 1
		}
		end := size
		for end < len(p) && end < 3000 && utf8.FullRune(p[end:]) {
			_, k := utf8.DecodeRune(p[end:])
			end += k
		}
		if w.b.used+end > 60000 {
			if !w.b.warned {
				w.b.emit("system", "\n[输出达到 60 KB 限制；后续内容已截断]\n")
				w.b.warned = true
			}
			p = p[end:]
			continue
		}
		w.b.used += end
		w.b.emit(w.stream, string([]rune(string(p[:end]))))
		p = p[end:]
	}
	return n, nil
}
func (w *chunkWriter) flush() {
	if len(w.pending) > 0 {
		w.Write([]byte("\n"))
	}
}
func ExecuteShell(parent context.Context, m wire.Message, emit func(string, string)) ShellResult {
	start := time.Now()
	result := ShellResult{ExitCode: -1, Status: "failed"}
	if e := wire.ValidateCommand(m); e != nil {
		emit("stderr", e.Error()+"\n")
		return result
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(m.Timeout)*time.Second)
	defer cancel()
	path, args, e := ShellCommand(m.Shell, m.Command)
	if e != nil {
		emit("stderr", e.Error()+"\n")
		return result
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = nil
	cmd.Dir = m.Cwd
	if cmd.Dir == "" {
		cmd.Dir, _ = os.UserHomeDir()
	}
	ConfigureShell(cmd)
	cmd.WaitDelay = 2 * time.Second
	budget := &outputBudget{emit: emit}
	out := &chunkWriter{b: budget, stream: "stdout"}
	errOut := &chunkWriter{b: budget, stream: "stderr"}
	cmd.Stdout = out
	cmd.Stderr = errOut
	e = cmd.Run()
	out.flush()
	errOut.flush()
	result.DurationMS = time.Since(start).Milliseconds()
	if e == nil {
		result.ExitCode = 0
		result.Status = "completed"
	} else {
		var ee *exec.ExitError
		if errors.As(e, &ee) {
			result.ExitCode = ee.ExitCode()
		} else {
			emit("stderr", e.Error()+"\n")
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result.Status = "timed_out"
	} else if ctx.Err() != nil {
		result.Status = "cancelled"
	}
	return result
}
