package widgets

// R28-B2 生产者/消费者对齐：用**实际 Go 日志写入路径**（newFixtureLog/
// emit + fixtureView.Layout，经离屏 input.Router 真实事件驱动）生成验收
// 日志，供 Python judge 消费——不用手拼 Go 永远写不出的日志。
//
// 产出文件（-kbfix-out 指定目录；缺省仅 t.TempDir 内存+临时文件断言）：
//   producer-positive.jsonl      结构完整正对照（含 100%→150% 同帧选择/滚动）
//   producer-pointer-only.jsonl  纯鼠标轨迹（拒作键盘证据）
//   producer-run-error.jsonl     RunWindow 非 nil 错误轨迹（fixture 失败）

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var kbfixOut = flag.String("kbfix-out", "", "生成生产者对齐日志的输出目录（空 = 仅临时目录断言）")

type kbProducer struct {
	r       input.Router
	ops     op.Ops
	th      *material.Theme
	log     *fixtureLog
	view    *fixtureView
	ed      *widget.Editor
	metric  unit.Metric
	w, h    int
	logPath string
}

func newKBProducer(t *testing.T, rows int, w, h int, pxPerDp float32) *kbProducer {
	t.Helper()
	gridA := NewDataGrid(unit.Dp(24))
	if err := gridA.SetColumns(fixtureGridColumns()); err != nil {
		t.Fatal(err)
	}
	if rep := gridA.SetRows(fixtureGridRows(rows)); !rep.OK {
		t.Fatal("gridA 数据失败")
	}
	gridA.EnableKeyboardNav(DataGridKeyboardConfig{})
	gridB := NewDataGrid(unit.Dp(24))
	if err := gridB.SetColumns(fixtureGridColumns()); err != nil {
		t.Fatal(err)
	}
	if rep := gridB.SetRows(fixtureGridRows(rows)); !rep.OK {
		t.Fatal("gridB 数据失败")
	}
	// gridB 不开启键盘：禁用对照。
	ed := &widget.Editor{}
	th := material.NewTheme()
	p := &kbProducer{
		th:      th,
		ed:      ed,
		metric:  unit.Metric{PxPerDp: pxPerDp, PxPerSp: pxPerDp},
		w:       w,
		h:       h,
		logPath: filepath.Join(t.TempDir(), "producer.jsonl"),
	}
	var err error
	p.log, err = newFixtureLog(p.logPath)
	if err != nil {
		t.Fatal(err)
	}
	p.view = &fixtureView{gridA: gridA, gridB: gridB, ed: ed, th: th, log: p.log}
	t.Cleanup(func() { p.log.flushAndClose() })
	return p
}

// frame 用真实离屏 Router 帧驱动 fixtureView.Layout（与窗口同构；
// Metric 来自当前 DPI 设定）。事件序列中的 gtxFocusEvent 在帧内先执行
// FocusCmd（焦点持有资格由同帧 Layout 注册的处理器授予）。
func (p *kbProducer) frame(events ...event.Event) {
	var focusTag event.Tag
	var real []event.Event
	for _, ev := range events {
		if fe, ok := ev.(gtxFocusEvent); ok {
			focusTag = fe.tag
			continue
		}
		real = append(real, ev)
	}
	p.r.Queue(real...)
	p.ops.Reset()
	gtx := layout.Context{
		Ops:         &p.ops,
		Source:      p.r.Source(),
		Metric:      p.metric,
		Constraints: layout.Exact(image.Pt(p.w, p.h)),
	}
	if focusTag != nil {
		gtx.Execute(key.FocusCmd{Tag: focusTag})
	}
	p.view.Layout(gtx, p.th)
	p.r.Frame(&p.ops)
}

// gtxFocusEvent 是帧内 FocusCmd 的载体（kbProducer.frame 专用）。
type gtxFocusEvent struct{ tag *overlayTag }

func (gtxFocusEvent) ImplementsEvent() {}

func (p *kbProducer) click(x, y int) {
	p.frame(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(float32(x), float32(y))},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(float32(x), float32(y))},
	)
}

func (p *kbProducer) keyPress(name key.Name) {
	p.frame(key.Event{Name: name, State: key.Press})
}

// focusSelf 用真实 FocusCmd（Source.Execute 立即生效）让 gridA 持焦，
// 随后一帧完成布局注册 focusable+visible（同帧缺 Layout 会因 handler
// 被清理而丢焦）。
func (p *kbProducer) focusSelf() {
	p.r.Source().Execute(key.FocusCmd{Tag: &p.view.gridA.tag})
	p.frame()
}

func (p *kbProducer) setDPI(px float32) {
	p.metric = unit.Metric{PxPerDp: px, PxPerSp: px}
}

// readProduced 解析真实生产路径产出的日志文件。
func (p *kbProducer) readProduced(t *testing.T) []map[string]interface{} {
	t.Helper()
	b, err := os.ReadFile(p.logPath)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]interface{}
	for _, line := range splitLines(string(b)) {
		if line == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("日志行非法: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// dumpIfRequested 把产出日志复制到 -kbfix-out（供录台账的 judge 消费）。
func (p *kbProducer) dumpIfRequested(t *testing.T, name string) {
	t.Helper()
	if *kbfixOut == "" {
		return
	}
	if err := os.MkdirAll(*kbfixOut, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(p.logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*kbfixOut, name), src, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestKBFixtureLogProducerPositive：结构完整正对照——含 100%→150% 同帧
// 选择/滚动（几何用当帧 Dp 换算，不引用上一帧缓存）。
func TestKBFixtureLogProducerPositive(t *testing.T) {
	p := newKBProducer(t, 10000, 960, 640, 1.0)
	p.frame() // 起始帧（resize 记录 pxPerDp=1）
	p.focusSelf()
	p.click(50, 24+2*24+12) // 行 2（DPI 100%，几何 rowHpx=24）
	p.keyPress(key.NameDownArrow)
	// 100%→150% 同帧选择/滚动：改 DPI 后按 End——末行选择与滚到底在
	// 新几何当帧完成（rowHpx=36），不得引用上一帧缓存（24）。
	p.setDPI(1.5)
	p.keyPress(key.NameEnd)
	// 在离屏用例中模拟调用点，和窗口入口共享记录函数。
	p.log.recordDestroy()
	p.log.recordRunResult(nil)
	p.dumpIfRequested(t, "producer-positive.jsonl")

	// 断言（手算）：DPI 1.0 帧（frame<4）rowHpx=24；End 帧（frame≥4）
	// rowHpx=36、offset=10000*36−(300−36)=359736。
	sawOldGeom, sawNewGeom, sawMaxScroll := false, false, false
	for _, ev := range p.readProduced(t) {
		switch ev["event"] {
		case "sel-change", "scroll":
			frameN, _ := ev["frame"].(float64)
			rh, _ := ev["rowHpx"].(float64)
			if frameN < 5 {
				if rh != 24 {
					t.Fatalf("DPI 1.0 帧几何错误：rowHpx=%v (frame %v)", rh, frameN)
				}
				sawOldGeom = true
				continue
			}
			if rh != 36 {
				t.Fatalf("DPI 1.5 帧几何错误：rowHpx=%v (frame %v)", rh, frameN)
			}
			sawNewGeom = true
			if ev["event"] == "scroll" {
				off, _ := ev["offset"].(float64)
				if off != 359736 {
					t.Fatalf("End 滚到底偏移应为 359736，得到 %v", off)
				}
				sawMaxScroll = true
			}
		}
	}
	if !sawOldGeom || !sawNewGeom || !sawMaxScroll {
		t.Fatalf("几何轨迹不完整：old=%v new=%v maxScroll=%v", sawOldGeom, sawNewGeom, sawMaxScroll)
	}
	// End 后末行可见（同帧滚入）。
	if !p.view.selVisible {
		t.Fatal("End 后末行必须在同一帧可见")
	}
}

// TestKBFixtureLogProducerPointerOnly：纯鼠标轨迹——只点击与滚轮，无任何
// 键盘事件。结构合规，但不能作为键盘通过证据。
func TestKBFixtureLogProducerPointerOnly(t *testing.T) {
	p := newKBProducer(t, 10000, 960, 640, 1.0)
	p.frame()
	p.focusSelf()
	p.click(50, 24+3*24+12) // 行 3（鼠标选择）
	p.frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(150, 100), Scroll: f32.Pt(0, 500)})
	p.log.recordDestroy()
	p.log.recordRunResult(nil)
	p.dumpIfRequested(t, "producer-pointer-only.jsonl")

	// 内存断言：轨迹中确有选择与滚动（日志结构完整），但日志本身不携带
	// 任何键盘字段——judge 据此不判键盘。
	hadSel, hadScroll := false, false
	for _, ev := range p.readProduced(t) {
		switch ev["event"] {
		case "sel-change":
			hadSel = true
		case "scroll":
			hadScroll = true
		}
		if _, has := ev["key"]; has {
			t.Fatal("纯鼠标轨迹不得携带键盘字段")
		}
	}
	if !hadSel || !hadScroll {
		t.Fatalf("纯鼠标轨迹应含选择与滚动：sel=%v scroll=%v", hadSel, hadScroll)
	}
}

// TestKBFixtureLogProducerRunErr：运行失败轨迹——RunWindow 非 nil 错误的
// 真实留痕（fixture 失败路径）。
func TestKBFixtureLogProducerRunErr(t *testing.T) {
	p := newKBProducer(t, 10000, 960, 640, 1.0)
	p.frame()
	p.focusSelf()
	p.click(50, 24+12)
	runErr := fmt.Errorf("simulated native window failure")
	p.log.recordDestroy()
	p.log.recordRunResult(runErr)
	p.dumpIfRequested(t, "producer-run-error.jsonl")

	sawErr := false
	for _, ev := range p.readProduced(t) {
		if ev["event"] == "run-return" {
			s, _ := ev["err"].(string)
			if s == "<nil>" || s == "" {
				t.Fatal("run-error 轨迹的 run-return 必须携带非 nil 错误")
			}
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("缺 run-return 事件")
	}
}

func TestKBFixtureLogPreservesExistingEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.jsonl")
	const prior = "prior evidence\n"
	if err := os.WriteFile(path, []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}
	if log, err := newFixtureLog(path); !os.IsExist(err) {
		if log != nil {
			log.flushAndClose()
		}
		t.Fatalf("existing evidence must be refused: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != prior {
		t.Fatalf("existing evidence changed: %q (%v)", b, err)
	}
}
