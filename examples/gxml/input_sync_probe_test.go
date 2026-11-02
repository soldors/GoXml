package examples

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/goxml/goxml/internal/cli"
)

// TestInputSyncWriteBackProbe 是 D-3 的**有界** headless 实验（见
// docs/05-开发/2026-11-02-D3-输入同步有界实验观察.md）。
//
// 它只做观察，不改生产语义：fixture（页面 + ViewModel）与断言都放在本文件里，
// 不新增 testdata 目录、不动 CLI / Codegen / runtime / contracts。
//
// 链路：真实 CLI 生成 → 独立 module 编译生成物 → 真实 input.Router 驱动帧，
// 记录"外部改值 → 帧内 SetText → ChangeEvent → ValueChanged → 回写"的逐帧四元组
// （编辑器文本 / 外部值 / applied 记账 / 回调次数），帧数硬上限 8。
//
// 结论口径（本测试只证明其中前两条，第三条靠人工产品流验收）：
//   - 事件机制：外部应用**确实**会产生一次 ChangeEvent（带一次 ValueChanged）；
//   - 可复现往复：生成代码在回调后把 applied 重贴到 vm，因此三种回写都在 2 帧内静默，
//     包括故意非幂等的那种——本实验**没有**复现出往复；
//   - 典型产品流："程序化同步也通知"与"改写载荷后的编辑器/模型分歧"是可观察后果，
//     其中 upper 是幂等函数，故幂等性不足以保证编辑器与模型一致；
//     是否算缺陷由协调者裁决，本测试不下结论。
func TestInputSyncWriteBackProbe(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	mod := strings.Replace(string(read(filepath.Join(repo, "go.mod"))),
		"module github.com/goxml/goxml", "module example.com/goxml-inputsync", 1)
	mod += "\nrequire github.com/goxml/goxml v0.0.0\nreplace github.com/goxml/goxml => " + strconv.Quote(filepath.ToSlash(repo)) + "\n"
	write("go.mod", []byte(mod))
	write("go.sum", read(filepath.Join(repo, "go.sum")))
	write("inputsync.gxml", []byte(inputSyncGXMLSource))
	write("vm.go", []byte(inputSyncVMSource))

	// 真实 CLI 生成（进程内 cli.Run，与教程同一条路径，不是自己拼产物）。
	var out, errs bytes.Buffer
	rc := cli.Run([]string{"generate", filepath.Join(dir, "inputsync.gxml"), "--no-cache"}, &out, &errs)
	if rc != cli.ExitOK {
		t.Fatalf("generate rc=%d want=%d\nstdout=%s\nstderr=%s", rc, cli.ExitOK, out.String(), errs.String())
	}
	gen := read(filepath.Join(dir, "inputsync_gen.go"))
	// 判据必须落在生成物上：证明后续跑的是 Codegen 的同步块，不是手抄一份。
	for _, want := range []string{"applied0", "ed.SetText(cur)", "case widget.ChangeEvent:"} {
		if !bytes.Contains(gen, []byte(want)) {
			t.Fatalf("生成物里没有 %q——实验跑的不是生成代码", want)
		}
	}

	write("inputsync_probe_test.go", []byte(inputSyncSemSource))

	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("编译需要 PATH 上的 Go:", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	env := append(os.Environ(), "CGO_ENABLED=0", "GOMAXPROCS=2", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOFLAGS=")
	cmd := exec.CommandContext(ctx, goExe, "test", "-mod=readonly", "-p", "2", "-count=1", "-v", "-run", "TestInputSync", ".")
	cmd.Dir = dir
	cmd.Env = env
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("D-3 实验失败: %v\n%s", err, raw)
	}
	text := string(raw)
	// 绿色运行也要留下痕迹：把内层真实观察行原样转写进本层日志（不是重新编造），
	// 否则 `-v` 的日志里只剩一句 PASS，看日志的人无法复核结论。
	t.Logf("内层输出 %d 字节", len(raw))
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "OBS ") || strings.Contains(trimmed, "MECH ") {
			t.Logf("%s", trimmed)
		}
	}

	// 1) 六条用例必须真的跑过：既不能缺，也不能被 Skip 兜底。
	for _, want := range []string{
		"--- PASS: TestInputSyncExternalWriteBack/verbatim",
		"--- PASS: TestInputSyncExternalWriteBack/upper",
		"--- PASS: TestInputSyncExternalWriteBack/append",
		"--- PASS: TestInputSyncUserTypingIsolated",
		"--- PASS: TestInputSyncMechanismResnap/resnap",
		"--- PASS: TestInputSyncMechanismResnap/no-resnap",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("缺少 %q\n%s", want, text)
		}
	}
	if n := strings.Count(text, "--- SKIP"); n != 0 {
		t.Fatalf("冻结行为不允许 Skip 兜底，实际 SKIP=%d\n%s", n, text)
	}

	// 2) 机制对照的**量化**结论：重贴 applied 时一次外部改值只回调 1 次，
	//    去掉重贴则每帧回调一次（上限 8 帧到点就停）。
	for _, want := range []string{
		"MECH resnap 到 8 帧为止 changes=1",
		"MECH no-resnap 到 8 帧为止 changes=8",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("缺少机制对照结论 %q\n%s", want, text)
		}
	}

	// 3) 生成代码路径的三条末帧（当前行为基线）：值写在这里是**故意**的——
	//    将来若 Codegen 改成抑制程序化同步事件，这三行会变红，逼着文档同步更新。
	for _, want := range []string{
		`OBS mode=verbatim frames=2 last=editor="abc" vm="abc" applied="abc" changes=1`,
		`OBS mode=upper frames=2 last=editor="abc" vm="ABC" applied="ABC" changes=1`,
		`OBS mode=append frames=2 last=editor="abc" vm="abc!" applied="abc!" changes=1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("生成代码路径的末帧与记录不一致，缺少 %q\n%s", want, text)
		}
	}
}

// inputSyncGXMLSource 是实验页面：只用 Text + Input，Input 在 <For> 之外。
const inputSyncGXMLSource = `<Goxml Version="1" ViewModel="ViewModel">
  <Page Route="/inputsync">
    <Column Gap="8">
      <Text Value="{Status}"/>
      <Input Value="{Draft}" Hint="probe" ValueChanged="OnChanged"/>
    </Column>
  </Page>
</Goxml>
`

// inputSyncVMSource 是实验的手写一半：三种回写方式由 Mode 切换。
const inputSyncVMSource = `package inputsync

import "strings"

const (
	ModeVerbatim = "verbatim"
	ModeUpper    = "upper"
	ModeAppend   = "append"
)

type ViewModel struct {
	Status  string
	Draft   string
	Mode    string
	Changes int

	LastPayload string
}

func NewViewModel() *ViewModel {
	return &ViewModel{Status: "input-sync probe", Mode: ModeVerbatim}
}

func (m *ViewModel) OnChanged(value string) {
	m.Changes++
	m.LastPayload = value
	switch m.Mode {
	case ModeUpper:
		m.Draft = strings.ToUpper(value)
	case ModeAppend:
		m.Draft = value + "!"
	default:
		m.Draft = value
	}
}
`

// inputSyncSemSource 是写入临时 module 的**有界**离屏实验（package inputsync）。
const inputSyncSemSource = `package inputsync

import (
	"fmt"
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

var probeTheme = material.NewTheme()

const (
	probeW, probeH = 640, 240
	capFrames      = 8
)

type probe struct {
	vm  *ViewModel
	v   *GoxmlInputsyncView
	r   input.Router
	gtx layout.Context
}

func newProbe(t *testing.T, mode string) *probe {
	t.Helper()
	vm := NewViewModel()
	vm.Mode = mode
	v, err := NewGoxmlInputsyncView(vm)
	if err != nil {
		t.Fatal(err)
	}
	p := &probe{vm: vm, v: v}
	p.gtx = layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(probeW, probeH)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      p.r.Source(),
	}
	return p
}

func (p *probe) frame() {
	p.gtx.Reset()
	p.v.Layout(p.gtx, probeTheme)
	p.r.Frame(p.gtx.Ops)
}

func (p *probe) click(x, y float32) {
	p.r.Queue(
		pointer.Event{Position: f32.Pt(x, y), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(x, y), Kind: pointer.Release, Source: pointer.Mouse},
	)
	p.frame()
}

func (p *probe) focusEditor(t *testing.T) {
	t.Helper()
	for y := 4; y <= probeH; y += 4 {
		p.click(probeW/2, float32(y))
		if p.gtx.Focused(&p.v.input0) {
			t.Logf("OBS 焦点由点击 (%d,%d) 获得", probeW/2, y)
			return
		}
	}
	t.Fatal("扫描全画布仍未让编辑器获得焦点")
}

type state struct {
	editor  string
	vm      string
	applied string
	changes int
}

func (p *probe) state() state {
	return state{p.v.input0.Text(), p.vm.Draft, p.v.applied0, p.vm.Changes}
}

func (s state) String() string {
	return fmt.Sprintf("editor=%q vm=%q applied=%q changes=%d", s.editor, s.vm, s.applied, s.changes)
}

func (p *probe) drive(t *testing.T, tag string, frames int) ([]state, bool) {
	t.Helper()
	var trace []state
	quiet := false
	for i := 1; i <= frames; i++ {
		p.frame()
		s := p.state()
		trace = append(trace, s)
		t.Logf("TRACE %s frame=%d %s", tag, i, s)
		if i >= 2 && s == trace[i-2] {
			quiet = true
			break
		}
	}
	return trace, quiet
}

func TestInputSyncExternalWriteBack(t *testing.T) {
	want := map[string]string{
		"verbatim": "editor=\"abc\" vm=\"abc\" applied=\"abc\" changes=1",
		"upper":    "editor=\"abc\" vm=\"ABC\" applied=\"ABC\" changes=1",
		"append":   "editor=\"abc\" vm=\"abc!\" applied=\"abc!\" changes=1",
	}
	for _, mode := range []string{ModeVerbatim, ModeUpper, ModeAppend} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			p := newProbe(t, mode)
			warm, _ := p.drive(t, "warm-"+mode, 2)
			if warm[len(warm)-1] != (state{}) {
				t.Fatalf("预热阶段不应产生任何同步或回调，实际 %s", warm[len(warm)-1])
			}
			// 外部改值：不经过编辑器事件，等价于"点了一行/按了清除"这类外部写入。
			p.vm.Draft = "abc"
			trace, quiet := p.drive(t, mode, capFrames)
			last := trace[len(trace)-1]
			t.Logf("OBS mode=%s frames=%d last=%s", mode, len(trace), last)
			if !quiet {
				t.Fatalf("mode=%s 在 %d 帧内没有静默——出现往复，需按 D-3 口径重新评估", mode, capFrames)
			}
			if len(trace) != 2 {
				t.Fatalf("mode=%s 应在第 2 帧静默，实际 %d 帧", mode, len(trace))
			}
			if got := last.String(); got != want[mode] {
				t.Fatalf("mode=%s 末帧应为 %s，实际 %s", mode, want[mode], got)
			}
			// 静默之后必须仍然静默：只相等一帧不算收敛。
			p.frame()
			if after := p.state(); after != last {
				t.Fatalf("mode=%s 静默后又变化：%s -> %s", mode, last, after)
			}
		})
	}
}

func TestInputSyncUserTypingIsolated(t *testing.T) {
	p := newProbe(t, ModeVerbatim)
	p.drive(t, "warm-typing", 2)
	p.focusEditor(t)
	before := p.state()
	p.r.Queue(key.EditEvent{Range: key.Range{}, Text: "z"})
	p.frame()
	after := p.state()
	t.Logf("OBS typing 输入前 %s", before)
	t.Logf("OBS typing 输入后 %s", after)
	if after.changes != before.changes+1 {
		t.Fatalf("敲一个字符应只产生 1 次回调，实际 delta=%d", after.changes-before.changes)
	}
	if after.editor != "z" || after.vm != "z" {
		t.Fatalf("输入后 editor/vm 应同为 \"z\"，实际 %s", after)
	}
}

// --- 机制探针：复刻生成代码的同步块，只在 applied 记账上做反事实对照 ---

type syncProbe struct {
	ed      widget.Editor
	applied string
	vm      string
	resnap  bool
	changes int
}

func (s *syncProbe) frame(gtx layout.Context) string {
	gtx.Reset()
	if s.vm != s.applied {
		s.ed.SetText(s.vm)
		s.applied = s.vm
	}
	for {
		ev, ok := s.ed.Update(gtx)
		if !ok {
			break
		}
		if _, isChange := ev.(widget.ChangeEvent); isChange {
			s.changes++
			// 故意非幂等：每次回写都追加一个字符。
			s.vm = s.ed.Text() + "!"
			if s.resnap {
				// 生成代码的现状：回调结束后把 applied 重贴到 vm。
				s.applied = s.vm
			}
		}
	}
	return s.ed.Text()
}

func TestInputSyncMechanismResnap(t *testing.T) {
	for _, resnap := range []bool{true, false} {
		resnap := resnap
		name := "resnap"
		if !resnap {
			name = "no-resnap"
		}
		t.Run(name, func(t *testing.T) {
			var r input.Router
			gtx := layout.Context{
				Ops:         new(op.Ops),
				Constraints: layout.Exact(image.Pt(probeW, probeH)),
				Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Source:      r.Source(),
			}
			s := &syncProbe{vm: "abc", resnap: resnap}
			for i := 1; i <= capFrames; i++ {
				text := s.frame(gtx)
				r.Frame(gtx.Ops)
				t.Logf("MECH %s frame=%d editor=%q vm=%q applied=%q changes=%d",
					name, i, text, s.vm, s.applied, s.changes)
			}
			t.Logf("MECH %s 到 %d 帧为止 changes=%d vm=%q", name, capFrames, s.changes, s.vm)
			if resnap && s.changes != 1 {
				t.Fatalf("重贴 applied 应只回调 1 次，实际 %d", s.changes)
			}
			if !resnap && s.changes != capFrames {
				t.Fatalf("不重贴 applied 应每帧回调一次（上限 %d），实际 %d", capFrames, s.changes)
			}
		})
	}
}
`
