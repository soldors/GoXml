// Command concurrencyprobe 验证 TASK-FONT-001-D 的核心设计依据：
// 「同一个 text.Shaper 被多个 goroutine 同时使用会崩溃」。
//
// # 为什么单独一个程序
//
// 实测结果是 Go 运行时的 **fatal error: concurrent map read and map write**，
// 它不是普通 panic——`recover()` **抓不住**，进程直接以退出码 2 终止。
// 因此它不能和 lifecycleprobe 放在一起：会把前面的正常输出一起带崩。
// 本程序**预期崩溃**，崩溃本身就是通过证据。
//
// # 这条依据为什么重要
//
// D 卡 §4.2 冻结了「帧内替换」模型（后台只算结果 + Invalidate，
// 在 FrameEvent 内换 th.Shaper）。该设计完全建立在「shaper 非并发安全」上。
// 如果这条不成立，设计就是过度保守；如果成立，则不仅是"有数据竞争"，
// 而是**进程级致命崩溃**——两者的严重性差一个量级。
//
// 依赖：goregular（纯拉丁，随 Go 库分发），不依赖系统字体。
package main

import (
	"fmt"
	"os"
	"sync"

	"gioui.org/font/opentype"
	"gioui.org/text"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

const (
	workers   = 4
	perWorker = 300
)

func main() {
	faces, err := opentype.ParseCollection(goregular.TTF)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解析 goregular 失败:", err)
		os.Exit(1)
	}
	// 同一个 shaper 实例被所有 worker 共享——这正是要复现的用法。
	shared := text.NewShaper(text.NoSystemFonts(), text.WithCollection(faces))
	params := text.Parameters{
		PxPerEm:  fixed.Int26_6(16 * 64),
		MaxWidth: 1 << 20,
	}

	fmt.Printf("=== 同一 shaper 被 %d 个 goroutine 同时 LayoutString（各 %d 次）===\n",
		workers, perWorker)
	fmt.Println("预期：fatal error: concurrent map read and map write（进程终止，recover 无效）")

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for k := 0; k < perWorker; k++ {
				shared.LayoutString(params, "Hello World 并发布局")
				for _, ok := shared.NextGlyph(); ok; _, ok = shared.NextGlyph() {
				}
			}
		}(i)
	}
	wg.Wait()

	// 走到这里说明本轮没崩。这**不能**被读成"并发安全"：
	// 数据竞争是概率性的，且本机 -race 不可用（缺 C 工具链）。
	fmt.Println("\n本轮未复现崩溃。这不等于并发安全：")
	fmt.Println("  - 数据竞争是概率性的，可能被调度掩盖；")
	fmt.Println("  - 本机 -race 不可用（缺 C 工具链），无法用检测器确认；")
	fmt.Println("  - 结论只能表述为「未复现」，不能表述为「安全」。")
	fmt.Println("  多跑几次（或用 -race）才有统计意义。")
}
