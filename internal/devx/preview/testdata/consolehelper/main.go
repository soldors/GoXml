// consolehelper 是预览执行控制的**自有替身进程**（TASK-DEVX-002 / R26-C C2）。
//
// 用途：验证 HostRunner 的生命周期语义（启动/等待/超时/进程树收口/幂等停止），
// 而不是运行真正的预览宿主原生窗口——本线明确不运行原生窗口。
//
// 行为：
//
//	consolehelper ready            打印 READY 后立即退出 0（正常退出路径）
//	consolehelper sleep <seconds>  打印 READY 后睡指定秒数再退出 0（超时路径）
//	consolehelper tree <seconds>   打印 READY，启动一个子进程（子进程睡
//	                               <seconds> 后把 marker 文件写到 --child-out
//	                               指定路径），父进程等待子进程退出
//	                               （进程树收口判据：父进程被 /T 终止后，
//	                               子进程不应再写出 marker）
//
// 它没有任何窗口/图形依赖，纯 console。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: consolehelper ready|sleep <s>|tree <s> [--child-out path]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ready":
		fmt.Println("READY")
		os.Exit(0)
	case "sleep", "tree":
		secs := 30
		if len(os.Args) >= 3 {
			if n, err := strconv.Atoi(os.Args[2]); err == nil && n > 0 {
				secs = n
			}
		}
		if os.Args[1] == "tree" {
			out := childOutArg(os.Args[3:])
			if out == "" {
				fmt.Fprintln(os.Stderr, "tree mode requires --child-out")
				os.Exit(2)
			}
			// 子进程：睡 3 秒后写 marker（若父进程树已被收口，这里永远走不到；
			// 3 秒 < 测试的收口判定窗口，杀漏了 marker 必然出现）。
			self, err := os.Executable()
			if err != nil {
				os.Exit(3)
			}
			child := exec.Command(self, "child", "3", out)
			child.Stdout = nil
			child.Stderr = nil
			if err := child.Start(); err != nil {
				os.Exit(3)
			}
			fmt.Println("READY")
			_ = child.Wait()
			os.Exit(0)
		}
		fmt.Println("READY")
		time.Sleep(time.Duration(secs) * time.Second)
		os.Exit(0)
	case "child":
		// 子进程形态：sleep N 秒后写 marker 文件。
		if len(os.Args) < 4 {
			os.Exit(2)
		}
		secs, err := strconv.Atoi(os.Args[2])
		if err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Duration(secs) * time.Second)
		if err := os.WriteFile(os.Args[3], []byte("child-done\n"), 0o644); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "unknown mode")
		os.Exit(2)
	}
}

func childOutArg(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--child-out" {
			return args[i+1]
		}
	}
	return ""
}
