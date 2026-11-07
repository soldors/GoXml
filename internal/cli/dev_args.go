package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// Keep application output and lifecycle diagnostics safe for arbitrary writers.
type commandWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (w commandWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(b)
}
func synchronizeCommandWriters(out, err io.Writer) (io.Writer, io.Writer) {
	if out == nil {
		out = io.Discard
	}
	if err == nil {
		err = io.Discard
	}
	mu := new(sync.Mutex)
	return commandWriter{mu, out}, commandWriter{mu, err}
}

// Reorder known flags for flag.FlagSet while supporting the documented path-first syntax.
func splitDevArgs(args []string, flags map[string]bool) (positionals, options, tail []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			tail = append(tail, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") {
			positionals = append(positionals, a)
			continue
		}
		if !strings.HasPrefix(a, "--") {
			return nil, nil, nil, fmt.Errorf("未知参数 %q", a)
		}
		name, _, hasValue := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		wantsValue, ok := flags[name]
		if !ok {
			return nil, nil, nil, fmt.Errorf("未知参数 %q", a)
		}
		options = append(options, a)
		if wantsValue && !hasValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return nil, nil, nil, fmt.Errorf("--%s 需要参数值", name)
			}
			i++
			options = append(options, args[i])
		}
	}
	return
}
