// Command m1nightprobe 是 NIGHT-FONT-M1-MEASUREMENT 的**真实窗口** M1 探针
// （WorkBuddy，2026-10-27 夜间任务）。
//
// # 它解决什么
//
// D-ACCEPT-04 的现有读数全部来自**合成 FrameEvent**（测试里直接调用私有方法
// `frame()`）。本探针用**真实 Gio 平台窗口 + 真实 FrameEvent** 读取被测
// `*nightapp.App` 的 `App.FirstFrame()`，回答"真实窗口下 M1 能否测到、读数多少"。
//
// # 与既有 windowprobe 的差别（重要，不得混淆）
//
// `pkg/runtime/app/testdata/windowprobe` 用的是**裸 `gioui.org/app.Window`**，
// 它拿不到项目 `App` 的 M1；本探针构造的是**项目 `nightapp.App` 本身**，
// 因此 `App.FirstFrame()` 是被测对象的真实读数，不是仿造事件循环的结果。
//
// # 自动关窗方式与其原因（必须随结论引用）
//
// `App` **未暴露**关闭窗口的公共 API（也不允许为测试新增：本任务禁止改生产 API，
// 且 App.Run() 在 Windows 上调用 app.Main() → osMain() = select{}，**永不返回**）。
// 因此本探针采用「读到所需读数即 **os.Exit** 自行结束进程」的方式关窗：
//   - 不需要任何鼠标/键盘/用户点击；
//   - 进程退出后由操作系统销毁其窗口，不会留下孤儿窗口；
//   - 代价是**收不到 DestroyEvent**，因此本探针不验证"干净销毁"，
//     只验证"真实 FrameEvent 下的 M1 读数"。
//
// # 显式 opt-in
//
// 本探针**不是** go test 的一部分（`./...` 会跳过 testdata），必须显式
// `go run` 执行，普通 `go test` 不会弹窗。执行时会在桌面出现一个 240x90dp
// 的小窗口，持续时间不超过 `-timeout`（默认 20s，由 `-hardtimeout` 兜底）。
//
// # 退出码
//
//	0 = 真实窗口下取得 M1 读数，且达到 -frames 要求的帧数
//	1 = 构造失败 / 运行期错误（有明确诊断）
//	3 = 超时或环境阻塞（**不是通过**；按任务书须记为"环境限制"或"未验证"）
//
// 运行示例：
//
//	go run ./pkg/runtime/app/testdata/m1nightprobe -mode=window -load=none -frames=3 -timeout=20s
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"

	nightapp "github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// ---------- 观测视图 ----------

// probeView 记录 Layout 的调用次数与第一次进入的时刻。
//
// 唯一职责：给主控 goroutine 提供"真实帧已到达"的观测点。它在首次 Layout
// 期间读一次 `App.FirstFrame()`，用于交叉验证 M1 已经可测。
type probeView struct {
	owner *nightapp.App
	mu    sync.Mutex

	calls      int64
	firstEntry time.Time
	// firstAtLayoutMeasured 记录"首个 Layout 期间 FirstFrame().Measured()"
	// 的值——用于证明 M1 止点在 Layout **之前**（定性判据，不受时钟粒度影响）。
	firstAtLayoutMeasured bool
}

type probeSnapshot struct {
	calls    int64
	first    time.Time
	measured bool
}

func (v *probeView) snapshot() probeSnapshot {
	v.mu.Lock()
	defer v.mu.Unlock()
	return probeSnapshot{v.calls, v.firstEntry, v.firstAtLayoutMeasured}
}

func (v *probeView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.mu.Lock()
	v.calls++
	if v.calls == 1 {
		v.firstEntry = time.Now()
		if v.owner != nil {
			v.firstAtLayoutMeasured = v.owner.FirstFrame().Measured()
		}
	}
	v.mu.Unlock()
	return material.Body1(th, "M1 真实窗口探针").Layout(gtx)
}

// ---------- 主流程 ----------

func main() {
	mode := flag.String("mode", "window", "探针模式：window=真实平台窗口（唯一实现）")
	load := flag.String("load", "none", "字体加载方式：none=material.NewTheme(不加载系统字体) / sync=theme.NewTheme / lazy=NewLazyApp")
	frames := flag.Int("frames", 3, "需要观测到的真实帧数（达到即退出 0）")
	timeout := flag.Duration("timeout", 20*time.Second, "等待帧的最长时间；超时退出 3")
	hardTimeout := flag.Duration("hardtimeout", 45*time.Second, "全程硬上限，兜底防止挂起；超过退出 3")
	title := flag.String("title", "M1-Night-Probe", "窗口标题")
	flag.Parse()
	if *frames < 1 || *timeout <= 0 || *hardTimeout <= 0 {
		fmt.Println("RESULT verdict=ERROR detail=frames 与两个 timeout 必须为正数")
		os.Exit(1)
	}

	if *mode != "window" {
		fmt.Printf("MODE_ERR unsupported mode=%q\n", *mode)
		os.Exit(1)
	}

	fmt.Println("=== NIGHT-FONT-M1-MEASUREMENT · 真实窗口 M1 探针（WorkBuddy）===")
	fmt.Printf("ENV go=%s os=%s arch=%s gomaxprocs=%d\n",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0))
	fmt.Printf("CONFIG load=%s frames=%d timeout=%s hardtimeout=%s title=%s mode=real-window(project App)\n",
		*load, *frames, *timeout, *hardTimeout, *title)

	// 硬上限看门狗：任何情况下本进程都会自行结束，不留孤儿窗口。
	time.AfterFunc(*hardTimeout, func() {
		fmt.Println("RESULT verdict=HARD_TIMEOUT detail=全程硬上限到达，强制退出（未完成或未取到读数）")
		os.Exit(3)
	})

	procStart := time.Now()
	view := &probeView{}
	cfg := nightapp.Config{Title: *title, Width: unit.Dp(240), Height: unit.Dp(90)}

	var (
		a   *nightapp.App
		err error
	)
	ctorEntry := time.Now()
	switch *load {
	case "none":
		th := material.NewTheme()
		ctorEntry = time.Now()
		a, err = nightapp.NewApp(view, th, cfg)
	case "sync":
		th := theme.NewTheme()
		ctorEntry = time.Now()
		a, err = nightapp.NewApp(view, th, cfg)
	case "lazy":
		lz, lerr := theme.NewLazy(theme.LazyOptions{})
		if lerr != nil {
			fmt.Printf("RESULT verdict=ERROR detail=Lazy 构造失败: %v\n", lerr)
			os.Exit(1)
		}
		ctorEntry = time.Now()
		a, err = nightapp.NewLazyApp(view, lz, cfg)
	default:
		fmt.Printf("RESULT verdict=ERROR detail=未知 load=%q（合法值 none/sync/lazy）\n", *load)
		os.Exit(1)
	}
	if err != nil {
		fmt.Printf("RESULT verdict=ERROR detail=App 构造失败: %v\n", err)
		os.Exit(1)
	}
	ctorDone := time.Now()
	view.owner = a

	// 检查起点是否已被写入（Must bil CreatedAt 非零）。
	pre := a.FirstFrame()
	if pre.CreatedAt.IsZero() {
		fmt.Println("RESULT verdict=ERROR detail=CreatedAt 为零值，M1 起点未写入")
		os.Exit(1)
	}

	// 驱动被测 App 自己的事件循环。`Run()` 内部先 `go loop()` 再 `app.Main()`；
	// 在 Windows 上 osMain() 是 select{}，永不返回，所以放到后台 goroutine，
	// 主控 goroutine 负责轮询读数与自行退出。
	runResult := make(chan error, 1)
	go func() { runResult <- a.Run() }()

	// 轮询等待真实帧。轮询而非固定 sleep：真实窗口创建耗时受 DPI/后端影响。
	pollStart := time.Now()
	deadline := pollStart.Add(*timeout)
	var observed probeSnapshot
	for {
		select {
		case rerr := <-runResult:
			if rerr != nil {
				fmt.Printf("RESULT verdict=ERROR detail=Run 返回错误: %v\n", rerr)
				os.Exit(1)
			}
		default:
		}
		observed = view.snapshot()
		if observed.calls >= int64(*frames) && a.FirstFrame().Measured() {
			break
		}
		if time.Now().After(deadline) {
			fmt.Printf("RESULT verdict=ENV_TIMEOUT detail=%s 内未取得足够的真实帧（观测帧数=%d，要求=%d）\n",
				*timeout, observed.calls, *frames)
			fmt.Printf("RESULT_DATA framesObserved=%d m1Measured=%v\n", observed.calls, a.FirstFrame().Measured())
			os.Exit(3)
		}
		time.Sleep(2 * time.Millisecond)
	}

	rep := a.FirstFrame()
	fmt.Printf("RESULT_DATA procStartNs=%d\n", procStart.UnixNano())
	fmt.Printf("RESULT_DATA ctorEntryNs=%d\n", ctorEntry.UnixNano())
	fmt.Printf("RESULT_DATA ctorDoneNs=%d\n", ctorDone.UnixNano())
	fmt.Printf("RESULT_DATA createdAtNs=%d\n", rep.CreatedAt.UnixNano())
	fmt.Printf("RESULT_DATA firstFrameAtNs=%d\n", rep.FirstFrameAt.UnixNano())
	fmt.Printf("RESULT_DATA m1ElapsedNs=%d\n", rep.Elapsed.Nanoseconds())
	fmt.Printf("RESULT_DATA m1Measured=%v\n", rep.Measured())
	fmt.Printf("RESULT_DATA layoutFirstEntryNs=%d\n", observed.first.UnixNano())
	fmt.Printf("RESULT_DATA layoutFirstAtMeasured=%v\n", observed.measured)
	fmt.Printf("RESULT_DATA framesObserved=%d\n", observed.calls)
	fmt.Printf("RESULT_DATA frameWindowNs=%d\n", time.Since(pollStart).Nanoseconds())
	if lz := a.Lazy(); lz != nil {
		lr := lz.Report()
		fmt.Printf("RESULT_DATA lazyState=%s\n", lr.State.String())
		fmt.Printf("RESULT_DATA lazyTempElapsedNs=%d\n", lr.TempElapsed.Nanoseconds())
		fmt.Printf("RESULT_DATA lazyProbeElapsedNs=%d\n", lr.ProbeElapsed.Nanoseconds())
		fmt.Printf("RESULT_DATA lazyLoadElapsedNs=%d\n", lr.LoadElapsed.Nanoseconds())
		if lr.Probe != nil {
			fmt.Printf("RESULT_DATA lazyProbeRan=%v\n", lr.Probe.ProbeRan)
		}
	}

	verdictOK := rep.Measured() && rep.Elapsed >= 0 && observed.measured && observed.calls >= int64(*frames)
	if !rep.Measured() {
		fmt.Println("RESULT verdict=NOT_MEASURED detail=真实帧已到但 FirstFrame 仍报未测量（属于缺陷，不是环境问题）")
		os.Exit(1)
	}
	if !verdictOK {
		fmt.Println("RESULT verdict=FAIL detail=读数不完整")
		os.Exit(1)
	}
	fmt.Printf("RESULT verdict=OK detail=真实窗口下取得 M1 读数（Elapsed=%v，观测 %d 帧）\n",
		rep.Elapsed, observed.calls)
	os.Exit(0)
}
