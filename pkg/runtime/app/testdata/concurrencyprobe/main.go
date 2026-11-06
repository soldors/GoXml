// Command concurrencyprobe 验证 TASK-FONT-001-D 的核心设计依据：
// 「同一个 text.Shaper 被多个 goroutine 同时使用会崩溃」。
//
// # 为什么单独一个程序
//
// 本程序**预期崩溃**，崩溃本身就是通过证据：进程以**退出码 2** 终止。
// 因此它不能和 lifecycleprobe 放在一起：会把前面的正常输出一起带崩。
//
// # 崩溃形态不固定——勿写成单一预期
//
// D 卡 §4.3.2 记录了 6 次实测，崩溃点分布在 bidi 与 fontscan 的多个内部结构上：
//
//	index out of range [16] with length 16        bidi.(*Runs).Run
//	slice bounds out of range [:16] with cap 0    bidi.(*Paragraph).prepareInput
//	concurrent map writes                         fontscan.familyList.compileTo
//	concurrent map read and map write             fontscan.familyList.compileTo / runeLRU.Get
//
// 2026-10-23 复跑得到前两种，与 §4.3.2 表格一致（原始输出见
// docs/06-测试/原始日志/2026-10-23-测试矩阵/t7_conc.txt）。
// 因此**不要**把"预期错误"写成某一条固定文案——形态随竞态命中的内部结构而变。
//
// # 关于 recover()——两类崩溃机制不同，勿混为一谈
//
//   - `concurrent map ...` 是 Go 运行时的 **fatal error**：`recover()` 抓不住；
//   - `index out of range` / `slice bounds out of range` 是**普通 panic**，
//     理论可 recover，但发生在 worker goroutine，main 里的 `recover()`
//     **跨 goroutine 抓不到**，进程同样终止。
//
// 对调用方而言结论一致：进程级崩溃、退出码 2、无法在本进程内兜住。
//
// # 这条依据为什么重要
//
// D 卡 **§5（D4：避免竞态）** 冻结了「帧内替换」模型（后台只算结果 + Invalidate，
// 在 FrameEvent 内换 th.Shaper）——注意不是 §4.2，§4.2 是「加载期间的字体」。
// 该设计完全建立在「shaper 非并发安全」上。
// 如果这条不成立，设计就是过度保守；如果成立，则不仅是"有数据竞争"，
// 而是**进程级致命崩溃**——两者的严重性差一个量级。
//
// 证据边界：上述"必现"仅限本机 + Gio v0.10.3 + 本探针这一种调用方式，
// 足以支持出台工程禁令（异步路径不得借用渲染线程的 shaper），
// 但**不等于**断言 Gio 所有版本/调用方式都会同样崩溃（详见 D 卡 §4.3.2）。
// 升级 Gio 后需重跑本探针复核，但不得因为某次"没崩"就解除禁令。
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
	fmt.Println("预期：进程以退出码 2 终止（崩溃形态不固定，实测清单见 D 卡 §4.3.2）")

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
