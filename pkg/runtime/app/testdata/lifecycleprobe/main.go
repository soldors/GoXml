// Command lifecycleprobe 是 TASK-FONT-001-D §10 第 2 步的**独立验证探针**。
//
// 目的：在实现懒加载**之前**，用实测回答三个问题（D 卡 §4.2 / §10-2）：
//  1. 帧内替换 `th.Shaper` 是否被 Gio 完全接受（不 panic、真的生效）？
//  2. 替换后旧 shaper 是否还能用（Gio 有没有隐藏的 per-shaper 绑定/所有权）？
//  3. 连续帧绘制在替换前后是否稳定？
//
// **不改动任何字体选择逻辑**：本探针只用 `headless` + `material.Label`，
// 不碰 pkg/runtime/theme 的 Provider / 缓存 / 诊断。
//
// 关于 `Window.Invalidate()`：headless 的 `Window` 没有 Invalidate
// （它只有显式 `Frame`），因此本探针**无法**验证它。那条只有源码依据
// （`app/window.go:284` 注释 "safe for concurrent use"），
// 真实窗口下的并发行为仍列为**未实测**，见输出末尾的声明。
//
// 放在 testdata/ 下：Go 工具链忽略该目录，不会进入 ./... 的包列表。
package main

import (
	"fmt"
	"image"
	"image/color"
	"os"

	"gioui.org/font/opentype"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/goxml/goxml/pkg/runtime/theme/embedded"
)

const (
	winW = 240
	winH = 96
	// 探针字符：汉字。goregular 没有它，嵌入子集有它——
	// 因此两种 shaper 的渲染结果必然不同，可用来判断替换是否真的生效。
	probeText = "世"
)

// pixelStats 是一帧的简单指纹。
type pixelStats struct {
	nonWhite int // 非纯白像素数（背景是白，文字会占掉一部分）
	nonZero  int // 非全零像素数
}

func (p pixelStats) String() string {
	return fmt.Sprintf("nonWhite=%d nonZero=%d", p.nonWhite, p.nonZero)
}

func stats(img *image.RGBA) pixelStats {
	var p pixelStats
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.A != 0 {
				p.nonZero++
			}
			if c.A != 0 && c != (color.RGBA{255, 255, 255, 255}) {
				p.nonWhite++
			}
		}
	}
	return p
}

// renderFrame 用当前 th.Shaper 渲染一帧并截图。
//
// 关键：ops 每帧新建，`th` 是同一个对象——替换 shaper 发生在两次调用之间，
// 与 D 卡 §4.2 冻结的「帧内替换」模型一致（替换点位于两帧之间、
// 即 FrameEvent 处理内，而非渲染中途）。
func renderFrame(w *headless.Window, th *material.Theme) (*image.RGBA, error) {
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(winW, winH)),
	}
	paint.Fill(&ops, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	material.Label(th, unit.Sp(40), probeText).Layout(gtx)

	if err := w.Frame(&ops); err != nil {
		return nil, fmt.Errorf("Frame: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, winW, winH))
	if err := w.Screenshot(img); err != nil {
		return nil, fmt.Errorf("Screenshot: %w", err)
	}
	return img, nil
}

func mustShaper(name string, data []byte) *text.Shaper {
	faces, err := opentype.ParseCollection(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析 %s 失败: %v\n", name, err)
		os.Exit(1)
	}
	if len(faces) == 0 {
		fmt.Fprintf(os.Stderr, "%s 解析出 0 个 face\n", name)
		os.Exit(1)
	}
	// NoSystemFonts：把变量收紧到"只用给定集合"，结果与机器上装了什么无关。
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(faces))
}

func main() {
	assets, err := embedded.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "嵌入资产加载失败:", err)
		os.Exit(1)
	}

	// shaperL：纯拉丁，无汉字 → 渲染 "世" 只能是 notdef。
	// shaperC：嵌入子集，有汉字 → 能画出真实字形。
	shaperL := mustShaper("goregular", goregular.TTF)
	shaperC := mustShaper("embedded", assets.Font)

	w, err := headless.NewWindow(winW, winH)
	if err != nil {
		fmt.Fprintln(os.Stderr, "headless.NewWindow 失败:", err)
		os.Exit(2)
	}
	defer w.Release()

	th := material.NewTheme()
	fail := 0

	// ---- 场景 A：同一 shaper 连续 3 帧，输出必须稳定 ----
	fmt.Println("=== A. 同一 shaper 连续 3 帧（稳定性基线）===")
	th.Shaper = shaperL
	var baseL pixelStats
	for i := 0; i < 3; i++ {
		img, err := renderFrame(w, th)
		if err != nil {
			fmt.Fprintln(os.Stderr, "A 帧失败:", err)
			os.Exit(3)
		}
		s := stats(img)
		if i == 0 {
			baseL = s
		}
		fmt.Printf("  frame %d: %v\n", i+1, s)
		if s != baseL {
			fmt.Printf("  ✗ 帧间输出不一致（基线与本帧不同）\n")
			fail++
		}
	}

	// ---- 场景 B：帧内替换 shaper（拉丁 → 嵌入），输出必须改变 ----
	fmt.Println("\n=== B. 帧内替换 th.Shaper（拉丁 → 嵌入）===")
	th.Shaper = shaperL
	imgBefore, err := renderFrame(w, th)
	if err != nil {
		fmt.Fprintln(os.Stderr, "B 帧 1 失败:", err)
		os.Exit(3)
	}
	sBefore := stats(imgBefore)
	fmt.Printf("  替换前（拉丁）: %v\n", sBefore)

	// 这就是 D 卡 §4.2 的替换点：帧与帧之间，同一个 th 对象换 Shaper。
	th.Shaper = shaperC
	imgAfter, err := renderFrame(w, th)
	if err != nil {
		fmt.Fprintln(os.Stderr, "B 帧 2 失败:", err)
		os.Exit(3)
	}
	sAfter := stats(imgAfter)
	fmt.Printf("  替换后（嵌入）: %v\n", sAfter)

	if sAfter == sBefore {
		fmt.Println("  ✗ 替换后输出完全相同——替换没有生效，或两种字体渲染结果无法区分")
		fail++
	} else {
		fmt.Println("  ✓ 替换生效：同一 th、同一文本，输出随 Shaper 改变")
	}

	// ---- 场景 C：换回旧 shaper，必须能复现旧结果（无隐藏所有权）----
	fmt.Println("\n=== C. 换回旧 shaper（验证旧实例仍可用、无隐藏绑定）===")
	th.Shaper = shaperL
	imgBack, err := renderFrame(w, th)
	if err != nil {
		fmt.Fprintln(os.Stderr, "C 帧失败:", err)
		os.Exit(3)
	}
	sBack := stats(imgBack)
	fmt.Printf("  换回后: %v\n", sBack)
	if sBack != sBefore {
		fmt.Println("  ✗ 换回旧 shaper 后输出与首次不一致——存在隐藏的 per-shaper 状态")
		fail++
	} else {
		fmt.Println("  ✓ 换回旧 shaper 输出一致：shaper 可自由替换，无隐藏所有权")
	}

	// ---- 场景 D：每帧交替替换 10 帧，连续绘制必须稳定 ----
	fmt.Println("\n=== D. 每帧交替替换 10 帧（连续帧稳定性）===")
	alternateFail := 0
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			th.Shaper = shaperC
		} else {
			th.Shaper = shaperL
		}
		if _, err := renderFrame(w, th); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ 第 %d 帧失败: %v\n", i+1, err)
			alternateFail++
			break
		}
	}
	if alternateFail == 0 {
		fmt.Println("  ✓ 10 帧交替替换无 panic、无错误")
	} else {
		fail += alternateFail
	}

	// ---- 结论 ----
	fmt.Println("\n=== 结论 ===")
	if fail == 0 {
		fmt.Println("  ✓ 帧内替换 th.Shaper 被 Gio 接受；连续帧绘制稳定；无隐藏所有权")
	} else {
		fmt.Printf("  ✗ 有 %d 项未通过，见上文\n", fail)
	}
	fmt.Println("  ⚠ Window.Invalidate() 未在本探针中验证：headless Window 没有该方法。")
	fmt.Println("    它只有源码依据（app/window.go:284 注释 \"safe for concurrent use\"），")
	fmt.Println("    真实窗口下的并发 Invalidate 行为仍为**未实测**。")

	if fail != 0 {
		os.Exit(10)
	}
}
