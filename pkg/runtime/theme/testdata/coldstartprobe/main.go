// Command coldstartprobe 是 TASK-FONT-001-D 的**基线测量探针**（不是产品代码）。
//
// D1 要求：复测 C 落地后的冷启动耗时，区分「系统路径探测 / 字体解析 /
// shaper 构建 / 首帧」。本探针逐个阶段计时，为 D 的验收阈值提供事实基础。
//
// 放在 testdata/ 下：Go 工具链忽略该目录，不会进入 ./... 的包列表。
package main

import (
	"fmt"
	"os"
	"time"

	"gioui.org/font/opentype"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"

	"github.com/goxml/goxml/pkg/runtime/theme"
	"github.com/goxml/goxml/pkg/runtime/theme/embedded"
)

// stage 计时一段并打印。
func stage(name string, f func()) time.Duration {
	start := time.Now()
	f()
	d := time.Since(start)
	fmt.Printf("  %-28s %10v\n", name, d)
	return d
}

func main() {
	fmt.Println("=== D1 冷启动基线（C 落地后，2026-10-21）===")
	fmt.Printf("GOOS=windows 字体目标: CJK\n\n")

	rounds := 5
	fmt.Printf("--- 第 1 轮（完全冷）---\n")
	first := oneRound(true)

	fmt.Printf("\n--- 后续 %d 轮（进程内重复，观察缓存效应）---\n", rounds-1)
	var warmTotal time.Duration
	var warmProbe, warmShaper, warmShape time.Duration
	for i := 0; i < rounds-1; i++ {
		r := oneRound(false)
		warmTotal += r.total
		warmProbe += r.probe
		warmShaper += r.shaper
		warmShape += r.shape
	}
	n := rounds - 1
	fmt.Printf("  平均 total=%v probe=%v shaper=%v shape=%v\n",
		warmTotal/time.Duration(n), warmProbe/time.Duration(n),
		warmShaper/time.Duration(n), warmShape/time.Duration(n))

	fmt.Printf("\n--- 对照：嵌入链路（无系统字体）---\n")
	emb := stage("嵌入 Load + ParseCollection", func() {
		a, err := embedded.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "嵌入资产加载失败:", err)
			os.Exit(1)
		}
		if _, err := opentype.ParseCollection(a.Font); err != nil {
			fmt.Fprintln(os.Stderr, "解析失败:", err)
			os.Exit(1)
		}
	})
	_ = emb

	// ---- 首帧临时 shaper 的代价（D3 的设计依据）----
	//
	// Gio 的 `material.NewTheme()` 用 `&text.Shaper{}` 零值，而零值 shaper 在
	// **首次使用时**（Shaper.init）才构建，且默认加载系统字体库。
	// 因此"首帧先用零值 shaper 顶着"并不能省掉系统字体索引的开销——
	// 必须显式 NoSystemFonts 才可能让首帧不等待。
	fmt.Printf("\n--- 对照：首帧临时 shaper 的构建代价 ---\n")
	fmt.Printf("  %-40s %10v\n", "text.NewShaper()（默认，会索引系统字体）",
		timeIt(func() { _ = text.NewShaper() }))
	fmt.Printf("  %-40s %10v\n", "text.NewShaper(NoSystemFonts())",
		timeIt(func() { _ = text.NewShaper(text.NoSystemFonts()) }))
	fmt.Printf("  %-40s %10v\n", "text.NewShaper(NoSystemFonts+collection)",
		timeIt(func() {
			a, err := embedded.Load()
			if err != nil {
				return
			}
			fs, err := opentype.ParseCollection(a.Font)
			if err != nil {
				return
			}
			_ = text.NewShaper(text.NoSystemFonts(), text.WithCollection(fs))
		}))

	fmt.Printf("\n结论要点：首帧预算与 20ms 探测预算必须分开计量（见 D 卡 §2）。\n")
	fmt.Printf("冷启动 total=%v（其中系统探测 %v）\n", first.total, first.probe)
}

// timeIt 返回一段代码的耗时。
func timeIt(f func()) time.Duration {
	s := time.Now()
	f()
	return time.Since(s)
}

type roundResult struct {
	total, probe, shaper, shape, provider time.Duration
}

// oneRound 跑一轮完整解析，返回各阶段耗时。
func oneRound(verbose bool) roundResult {
	var r roundResult
	var p *theme.Provider
	var id string
	var rep *theme.ProbeReport
	var faces []text.FontFace

	r.probe = stage("ResolveWithReport（探测+覆盖）", func() {
		p = theme.NewProvider(theme.ProviderOptions{})
		i, rp, err := p.ResolveWithReport(theme.FamilyCJK)
		if err != nil {
			fmt.Fprintln(os.Stderr, "解析失败:", err)
			os.Exit(1)
		}
		id, rep = i, rp
	})
	if rep != nil {
		fmt.Printf("      → resolved=%s coverage=%v\n", rep.Resolved, rep.Coverage)
	}

	stage("Faces 反查（同 Provider）", func() {
		fs, ok := p.Faces(id)
		if !ok || len(fs) == 0 {
			fmt.Fprintln(os.Stderr, "Faces 反查失败")
			os.Exit(1)
		}
		faces = fs
	})

	var sh *text.Shaper
	r.shaper = stage("text.NewShaper", func() {
		sh = text.NewShaper(text.WithCollection(faces))
	})

	r.shape = stage("首次 shaping（10 字符）", func() {
		sh.LayoutString(text.Parameters{
			PxPerEm:  fixed.Int26_6(16 * 64),
			MaxWidth: 1 << 30,
		}, "Hello 世界")
		for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
			_ = g
		}
	})

	r.total = r.probe + r.shaper + r.shape
	if verbose {
		fmt.Printf("  %-28s %10v\n", "小计（不含 Faces 反查）", r.total)
	}
	_ = id
	return r
}
