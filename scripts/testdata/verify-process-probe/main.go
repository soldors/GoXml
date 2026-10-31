// verify-process-probe 是 verify-examples.ps1 宿主测试专用的 Go 包装进程。
//
// 它记录真实子进程看到的 argv、cwd 与关键环境变量，然后原样转发给真实 Go，
// 保留标准输出、标准错误和退出码。只用于 scripts/test-verify-examples-hosts.py，
// 不属于框架运行时，也不创建窗口。
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type record struct {
	Argv      []string          `json:"argv"`
	Cwd       string            `json:"cwd"`
	Env       map[string]string `json:"env"`
	StartedAt string            `json:"startedAt"`
}

func main() {
	logPath := os.Getenv("GOXML_VERIFY_ARGV_LOG")
	realGo := os.Getenv("GOXML_VERIFY_REAL_GO")
	if logPath == "" || realGo == "" {
		fmt.Fprintln(os.Stderr, "verify-process-probe: GOXML_VERIFY_ARGV_LOG and GOXML_VERIFY_REAL_GO are required")
		os.Exit(70)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(71)
	}
	keys := []string{
		"CGO_ENABLED", "GOMAXPROCS", "GOPROXY", "GOSUMDB", "GOTOOLCHAIN", "GOFLAGS",
		"TMP", "TEMP", "HOME", "PATH",
	}
	env := make(map[string]string, len(keys))
	for _, key := range keys {
		env[key] = os.Getenv(key)
	}
	blob, err := json.Marshal(record{
		Argv: os.Args, Cwd: cwd, Env: env,
		StartedAt: time.Now().Format(time.RFC3339Nano),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(72)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(73)
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(74)
	}
	if _, err := file.Write(append(blob, '\n')); err != nil {
		_ = file.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(75)
	}
	if err := file.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(76)
	}

	cmd := exec.Command(realGo, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	err = cmd.Run()
	if err == nil {
		return
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(77)
}
