// consoleapp 是 R26-A Watch 进程管理的**无窗口 console helper**（仅测试用，不启动 GUI）。
//
// 行为：打印一行 "CONSOLEAPP pid=<n> gen=<n>" 到 stdout，然后阻塞读取 stdin，
// 直到 stdin EOF 或收到结束（被父进程 Kill）——用于证明 Watch 能真实启动/替换/收口进程。
// 不执行任何用户业务 main/init；不是产品命令。
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	gen := ""
	if len(os.Args) > 1 {
		gen = os.Args[1]
	}
	fmt.Printf("CONSOLEAPP pid=%d gen=%s\n", os.Getpid(), gen)
	os.Stdout.Sync()
	// 阻塞直到 stdin EOF（父进程关闭）或被结束。
	_, _ = io.Copy(io.Discard, os.Stdin)
}
