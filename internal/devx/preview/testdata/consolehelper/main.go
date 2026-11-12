// consolehelper 是预览执行控制的**自有替身进程**（TASK-DEVX-002 / R26-C C2）。
//
// 用途：验证 HostRunner 的生命周期语义（启动/等待/超时/进程树收口/幂等停止），
// 而不是运行真正的预览宿主原生窗口——本线明确不运行原生窗口。
//
// 行为：
//
//	consolehelper ready            打印 READY 后立即退出 0（正常退出路径）
//	consolehelper sleep <seconds>  打印 READY 后睡指定秒数再退出 0（超时路径）
//	consolehelper tree <seconds>   打印 READY，先启动一个子进程（子进程睡
//	                               10 秒后把 marker 文件写到 --child-out
//	                               指定路径），然后父进程睡 <seconds> 秒
//	                               再退出 0（进程树收口判据：父进程被 /T
//	                               终止后，子进程不应再写出 marker）
//
//	tree 的宿主树自然寿命必须由 <seconds> 决定。R28 全树阻断 2 判别
//	（preview-shutdown-delivery-hy4 probe-a）：旧实现父进程 child.Wait()
//	后即退出，整树自然寿命实际只有子进程的 ~3 秒，与测试的 2 秒超时 +
//	taskkill 执行（负载下 2–2.4 秒）重叠，负载下宿主先于终止命令自然
//	退出，taskkill 如实返回 rc128「没有找到进程」，测试误判终止失败。
//	子进程不等待、由判据窗口覆盖：若树未被收口，子进程 3 秒后必然写出
//	marker。
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
			// 子进程：睡 10 秒后写 marker（若父进程树已被收口，这里永远
			// 走不到；杀漏了 marker 必然出现）。10 秒 = killCommandTimeout
			// 5 秒预算的 2 倍余量：R28 全树阻断 2 判别实测终止路径在负载下
			// 可达 4.5 秒以上（定时器唤醒延迟 2.5s + taskkill 执行 2–2.4s），
			// 旧的 3 秒窗口小于该耗时，子进程会在被终止前走完自然寿命写
			// marker 退出，把「未及时终止」误判成泄漏（假阳性）或让
			// taskkill 对已消失的子进程报「没有此进程实例」（rc255）。
			self, err := os.Executable()
			if err != nil {
				os.Exit(3)
			}
			child := exec.Command(self, "child", "10", out)
			child.Stdout = nil
			child.Stderr = nil
			if err := child.Start(); err != nil {
				os.Exit(3)
			}
			fmt.Println("READY")
			// 父进程睡 <seconds>：宿主树自然寿命与参数一致；不等待子进程
			// （旧实现整树寿命只有子进程的 ~3 秒，见文件头注释）。
			time.Sleep(time.Duration(secs) * time.Second)
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
