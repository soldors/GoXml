package app

// FACE-SELECT：真实 App 接线测试（NewLazyApp + NewLazyWithSelection）。
//
// 目的：证明"显式选面"产出的最终主题**只在帧内一次性换上**，并保持既有
// StrictFontFailure 与默认路径语义。不打开真实 GUI（真实窗口 smoke 留给
// 后续独立验收，本文件不声称已执行）。
//
// 帧内证据用既有 `lazyProbeView`（Layout 内记录 `th.Shaper` 与 `inFrame`）。

import (
	"errors"
	"testing"

	"gioui.org/op"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// faceSelOK 返回一个必然成功的单字体选面配置（Go Regular，index 0）。
func faceSelOK() theme.FaceSelection {
	return theme.FaceSelection{
		Source:  theme.FontSource{Bytes: goRegularTTF(), Name: "go-regular"},
		Indices: []int{0},
	}
}

// faceSelCorrupt 返回一个必然失败的选面配置（损坏 loca）。
func faceSelCorrupt(t *testing.T) theme.FaceSelection {
	t.Helper()
	return theme.FaceSelection{
		Source:  theme.FontSource{Bytes: corruptLocaForApp(t), Name: "corrupt-loca"},
		Indices: []int{0},
	}
}

// newFaceSelLazy 构造选面 Lazy（未 Request，交给 NewLazyApp 去 Request）。
func newFaceSelLazy(t *testing.T, sel theme.FaceSelection) *theme.Lazy {
	t.Helper()
	lz, err := theme.NewLazyWithSelection(sel, theme.LazyOptions{})
	if err != nil {
		t.Fatalf("构造选面 Lazy 失败: %v", err)
	}
	if got := lz.State(); got != theme.LoadUnloaded {
		t.Fatalf("构造后应处于 unloaded，实际 %s", got)
	}
	return lz
}

// TestFontFaceSelFrameInternalSwap 证明：selected 最终主题只在帧内一次性换上。
func TestFontFaceSelFrameInternalSwap(t *testing.T) {
	lz := newFaceSelLazy(t, faceSelOK())
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{Title: "facesel-swap"})
	if err != nil {
		t.Fatalf("NewLazyApp 失败: %v", err)
	}
	v.owner = a

	tempShaper := a.th.Shaper
	if tempShaper == nil {
		t.Fatal("构造后应有临时主题的 Shaper")
	}

	// 等后台把选中面解析完（Ready 由既有 worker 达成）。
	requireReady(t, lz)

	var ops op.Ops
	var rec frameRecorder
	// 逐帧驱动：第一帧应完成交接，其后应保持最终 Shaper。
	for i := 0; i < 4; i++ {
		a.frame(&ops, synthFrameEvent(&rec))
	}

	if len(v.shapers) != 4 {
		t.Fatalf("记录到的帧数 = %d，期望 4", len(v.shapers))
	}
	// ① 全部帧都在 inFrame 内（帧内交换的前提）。
	for i, in := range v.inFrames {
		if !in {
			t.Errorf("第 %d 帧的 Layout 不在帧内（inFrame=false）", i)
		}
	}
	// ② 第一帧即换成最终 Shaper，且最终 Shaper ≠ 临时 Shaper。
	if v.shapers[0] == tempShaper {
		t.Error("第 1 帧仍使用临时 Shaper：交接没有发生在帧内")
	}
	finalShaper := v.shapers[0]
	// ③ 只换一次：后续帧保持同一个最终 Shaper，不再回到临时。
	changes := 0
	prev := tempShaper
	for i, sh := range v.shapers {
		if sh != prev {
			changes++
			prev = sh
		}
		if i > 0 && sh != finalShaper {
			t.Errorf("第 %d 帧的 Shaper 与第 0 帧不同：发生多次替换", i)
		}
	}
	if changes != 1 {
		t.Errorf("Shaper 变化次数 = %d，期望恰好 1", changes)
	}
	// ④ 一次性交接：Take 已被消费。
	if lz.Take() != nil {
		t.Error("帧内已交接，Take 不应再返回主题")
	}
	// ⑤ 选面诊断标记已应用。
	sr := lz.SelectionReport()
	if sr == nil || !sr.SelectionApplied {
		t.Fatalf("SelectionReport 应标记已应用，实际 %+v", sr)
	}
	// ⑥ 选面路径不探测：Probe 为 nil、ProbeElapsed 为 0。
	rep := lz.Report()
	if rep.Probe != nil {
		t.Error("选面路径不应产生 Probe 报告")
	}
	if rep.ProbeElapsed != 0 {
		t.Errorf("ProbeElapsed = %v，期望 0", rep.ProbeElapsed)
	}
}

// TestFontFaceSelFailureNoSwapKeepsTemp 证明失败时永不换主题（保持临时主题）。
func TestFontFaceSelFailureNoSwapKeepsTemp(t *testing.T) {
	lz := newFaceSelLazy(t, faceSelCorrupt(t))
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{Title: "facesel-fail"})
	if err != nil {
		t.Fatalf("NewLazyApp 失败: %v", err)
	}
	v.owner = a
	tempShaper := a.th.Shaper

	requireTerminal(t, lz)
	if lz.State() != theme.LoadFailed {
		t.Fatalf("应进入 LoadFailed，实际 %s", lz.State())
	}

	var ops op.Ops
	var rec frameRecorder
	for i := 0; i < 3; i++ {
		a.frame(&ops, synthFrameEvent(&rec))
	}
	for i, sh := range v.shapers {
		if sh != tempShaper {
			t.Errorf("第 %d 帧出现了替换：失败路径不得换上任何最终主题", i)
		}
	}
	if lz.Take() != nil {
		t.Error("失败路径不应有待交接主题")
	}
	if !errors.Is(lz.Report().Err, contracts.ErrFontNotFound) {
		t.Errorf("失败 Err 应可判定为 ErrFontNotFound，实际 %v", lz.Report().Err)
	}
}

// TestFontFaceSelStrictFailure 严格模式下窗口照常运行，Run 返回 ErrFontNotFound。
func TestFontFaceSelStrictFailure(t *testing.T) {
	lz := newFaceSelLazy(t, faceSelCorrupt(t))
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{
		Title: "facesel-strict", StrictFontFailure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireTerminal(t, lz)
	stubMainNow(t)

	runErr := a.Run()
	if runErr == nil {
		t.Fatal("严格模式下 Run() 必须返回错误")
	}
	if !errors.Is(runErr, contracts.ErrFontNotFound) {
		t.Errorf("Run() 错误 = %v，必须可判定为 ErrFontNotFound", runErr)
	}
	if rep := a.Lazy().Report(); rep.Err == nil || !errors.Is(rep.Err, contracts.ErrFontNotFound) {
		t.Errorf("Report().Err 与 Run() 不一致：%v", rep.Err)
	}
	// 选面原因链仍可取回。
	var se *theme.SelectionError
	if !errors.As(a.Lazy().Report().Err, &se) {
		t.Error("Report().Err 应可用 errors.As 取回 *theme.SelectionError")
	}
}

// TestFontFaceSelNonStrictKeepsRunning 默认非严格：Run 返回 nil，失败仍可诊断。
func TestFontFaceSelNonStrictKeepsRunning(t *testing.T) {
	lz := newFaceSelLazy(t, faceSelCorrupt(t))
	a, err := NewLazyApp(&lazyProbeView{}, lz, Config{Title: "facesel-nonstrict"})
	if err != nil {
		t.Fatal(err)
	}
	requireTerminal(t, lz)
	stubMainNow(t)

	if runErr := a.Run(); runErr != nil {
		t.Errorf("非严格模式 Run() 应返回 nil，实际 %v", runErr)
	}
	rep := a.Lazy().Report()
	if rep.State != theme.LoadFailed {
		t.Errorf("state = %s，期望 failed", rep.State)
	}
	if !errors.Is(rep.Err, contracts.ErrFontNotFound) {
		t.Errorf("失败原因仍应可判定，实际 %v", rep.Err)
	}
}

// TestFontFaceSelDefaultPathUnchanged 默认 NewLazy 路径不受本功能影响。
func TestFontFaceSelDefaultPathUnchanged(t *testing.T) {
	lz := newTestLazy(t)
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{Title: "default-path"})
	if err != nil {
		t.Fatalf("NewLazyApp 失败: %v", err)
	}
	v.owner = a
	requireReady(t, lz)

	// 默认路径仍是"探测链"，选面报告不应存在。
	if sr := lz.SelectionReport(); sr != nil {
		t.Errorf("默认路径不应产生 SelectionReport，实际 %+v", sr)
	}
	rep := lz.Report()
	if rep.Probe == nil {
		t.Error("默认路径应产生 Probe 报告（与选面路径相反）")
	}

	var ops op.Ops
	var rec frameRecorder
	a.frame(&ops, synthFrameEvent(&rec))
	if len(v.shapers) != 1 {
		t.Fatalf("帧数 = %d", len(v.shapers))
	}
	if !v.inFrames[0] {
		t.Error("默认路径的替换同样必须在帧内")
	}
}

// ---------------------------------------------------------------------------
// 本文件专用 fixture（不复制解析实现，只构造损坏字节）
// ---------------------------------------------------------------------------

// goRegularTTF 返回 Go Regular 字节。
func goRegularTTF() []byte { return goregular.TTF }

// corruptLocaForApp 返回"loca 表前 8 字节被破坏"的 Go Regular 副本。
func corruptLocaForApp(t *testing.T) []byte {
	t.Helper()
	cp := append([]byte(nil), goregular.TTF...)
	if len(cp) < 12 {
		t.Fatal("字体过短")
	}
	numTables := int(cp[4])<<8 | int(cp[5])
	if numTables <= 0 || len(cp) < 12+16*numTables {
		t.Fatalf("表目录非法：numTables=%d", numTables)
	}
	for i := 0; i < numTables; i++ {
		e := 12 + 16*i
		if string(cp[e:e+4]) != "loca" {
			continue
		}
		off := int(cp[e+8])<<24 | int(cp[e+9])<<16 | int(cp[e+10])<<8 | int(cp[e+11])
		if off+8 > len(cp) {
			t.Fatal("loca 偏移越界")
		}
		for k := 0; k < 8; k++ {
			cp[off+k] = 0xAB
		}
		return cp
	}
	t.Fatal("未找到 loca 表")
	return nil
}
