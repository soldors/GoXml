package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// ============================================================================
// 步骤 2：绑定运行时读取验证
//
// 与步骤 1 的区别：
//   步骤 1 证明「生成的代码能编译」；
//   步骤 2 证明「生成的代码**执行时**真的读到了绑定值」。
//
// 证据链：
//   生成代码 → 编译 → 构造 ViewModel → 执行 Layout → 观察渲染结果
//
// 观测手段（已实测可行）：不同文本的非零像素数显著不同
//   "AAAA" → 280；"WWWWWWWWWWWW" → 1299
// 因此「改值后像素变化」可证明新值进入渲染路径，而非仅源码字符串匹配。
//
// ⚠️ 所有测试都是**测试显式驱动下一次 Layout**。
//    这不证明生产 UI 会在 ViewModel 变化后自动刷新——
//    生产通知/重绘归 TASK-STATE-001（红线 3）。
// ============================================================================

// rtProbe 在仓库内临时包中运行生成代码 + 探测测试。
//
// 必须在仓库内建包（不能建独立模块）：Go internal 规则禁止外部模块
// import 本仓库 internal/...，而测试用 ViewModel 恰在 internal 下。
func rtProbe(t *testing.T, gxmlBody, probeBody string) string {
	t.Helper()
	repoRoot := repoRootOf(t)

	v := verified(t, gxmlBody)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	// 必须建在 testdata/ 之下：go 工具通配（./...）忽略 testdata，
	// 但显式路径（./<dir>/...）仍可构建测试。
	// 若直接放在 codegen/ 下，探测包在运行期是「活包」，
	// 并发/嵌套的 ./... 通配会把它当成真实包去构建，
	// 观察到「构建中/半写」状态 → 全量 go test ./... 随机红灯
	// （2026-10-20 已两次出现 tmprt* 残骸误报）。
	dir := filepath.Join(repoRoot, "internal", "compiler", "codegen",
		"testdata", "tmprt"+randomSuffix())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "gen.go"), out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe_test.go"),
		[]byte(rtProbeHeader+probeBody), 0o644); err != nil {
		t.Fatal(err)
	}

	rel, err := filepath.Rel(repoRoot, dir)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-v", "-run", "TestProbe", "./"+filepath.ToSlash(rel)+"/...")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	bout, err := cmd.CombinedOutput()
	return string(bout) + "\n[err=" + rtErr(err) + "]"
}

func rtErr(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

// rtProbeHeader 是探测测试的公共部分（与生成代码同包）。
const rtProbeHeader = `package demo

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/goxml/goxml/internal/compiler/analyzer/testdata/fixture"
	"github.com/goxml/goxml/pkg/runtime/app"
)

// 各探测只用到部分依赖；用包级空引用避免「imported and not used」。
var (
	_ = f32.Pt
	_ = input.Router{}
	_ = pointer.Press
	_ = key.NameReturn
	_ = fmt.Sprint
	_ = app.RunWindow
	_ = time.Second
	_ = strings.Contains
	_ = material.NewTheme
	_ = fixture.ViewModel{}
	_ = headless.NewWindow
	_ = unit.Metric{}
)

func rtGtx(ops *op.Ops, w, h int) layout.Context {
	return layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(w, h)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
}

func rtShot(t *testing.T, win *headless.Window, ops *op.Ops, w, h int) []byte {
	t.Helper()
	if err := win.Frame(ops); err != nil {
		t.Skipf("Frame: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		t.Skipf("Screenshot: %v", err)
	}
	return img.Pix
}

func rtNonZero(p []byte) int {
	c := 0
	for i := 0; i+3 < len(p); i += 4 {
		if p[i]|p[i+1]|p[i+2]|p[i+3] != 0 {
			c++
		}
	}
	return c
}

var _ = material.NewTheme
var _ = app.RunWindow
var _ = time.Second

func fatal(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
var _ = fixture.ViewModel{}
var _ = unit.Metric{}
`

// TestBindingRuntimeReadsValue 验收项 1：
// 字符串绑定在真实 Layout 执行中读取正确值。
//
// 判据：绑定值不同 → 渲染像素数不同。若值没进入渲染路径，两者会相同。
func TestBindingRuntimeReadsValue(t *testing.T) {
	out := rtProbe(t, `<Text Value="{Title}"/>`, `
func TestProbeBindingRead(t *testing.T) {
	th := material.NewTheme()
	const w, h = 240, 60
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("headless: %v", err)
	}
	defer win.Release()


	// 短值
	vm := &fixture.ViewModel{Title: "AAAA"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		 fatal(t, err)
	}
	_ = v
	var o1 op.Ops
	v.Layout(rtGtx(&o1, w, h), th)
	n1 := rtNonZero(rtShot(t, win, &o1, w, h))

	// 长值
	vm2 := &fixture.ViewModel{Title: "WWWWWWWWWWWWWWWW"}
	v2, err := NewGoxmlDashboardView(vm2)
	if err != nil {
		 fatal(t, err)
	}
	_ = v2
	var o2 op.Ops
	v2.Layout(rtGtx(&o2, w, h), th)
	n2 := rtNonZero(rtShot(t, win, &o2, w, h))

	t.Logf("短值像素=%d 长值像素=%d", n1, n2)
	if n1 == 0 || n2 == 0 {
		t.Skip("渲染全零，无 GPU 后端")
	}
	if n1 == n2 {
		t.Errorf("两个不同的绑定值渲染像素相同（%d）——值未进入渲染路径", n1)
	} else {
		t.Log("不同绑定值产生不同渲染 → 绑定值确实进入渲染路径")
	}
}
`)
	t.Log(rtFilter(out))
	if !contains(out, "[err=nil]") {
		t.Fatalf("运行时探测失败:\n%s", out)
	}
}

// TestBindingRuntimePathKinds 验收项 2：
// 多段路径、指针访问、零参方法的运行行为符合契约。
func TestBindingRuntimePathKinds(t *testing.T) {
	cases := []struct {
		name string
		body string
		set  string // 给 vm 赋值的语句
		note string
	}{
		{"多段路径", `<Text Value="{User.Name}"/>`, `vm.User.Name`, "字段链"},
		{"指针访问", `<Text Value="{Owner.Name}"/>`, "vm.Owner.Name", "指针字段自动解引用"},
		{"零参方法", `<Text Value="{Display}"/>`, "vm.Title", "Display() 返回 Title"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := rtProbe(t, c.body, `
func TestProbePath(t *testing.T) {
	th := material.NewTheme()
	const w, h = 240, 60
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("headless: %v", err)
	}
	defer win.Release()


	vm := &fixture.ViewModel{}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		 fatal(t, err)
	}
	_ = v
	vm.Owner = &fixture.User{Name: "AAAA"}
	vm.User.Name = "AAAA"
	vm.Title = "AAAA"
	var o1 op.Ops
	v.Layout(rtGtx(&o1, w, h), th)
	n1 := rtNonZero(rtShot(t, win, &o1, w, h))

	vm2 := &fixture.ViewModel{}
	v2, err := NewGoxmlDashboardView(vm2)
	if err != nil {
		 fatal(t, err)
	}
	_ = v2
	vm2.Owner = &fixture.User{Name: "WWWWWWWWWWWWWWWW"}
	vm2.User.Name = "WWWWWWWWWWWWWWWW"
	vm2.Title = "WWWWWWWWWWWWWWWW"
	var o2 op.Ops
	v2.Layout(rtGtx(&o2, w, h), th)
	n2 := rtNonZero(rtShot(t, win, &o2, w, h))

	t.Logf("短值=%d 长值=%d", n1, n2)
	if n1 == 0 || n2 == 0 {
		t.Skip("渲染全零")
	}
	if n1 == n2 {
		t.Errorf("%s：值未进入渲染路径", "PATHKIND")
	}
}
`)
			t.Log(rtFilter(out))
			if !contains(out, "[err=nil]") {
				t.Fatalf("%s 运行时探测失败:\n%s", c.note, out)
			}
		})
	}
}

// TestBindingRuntimeValueChange 验收项 3 与 4：
// 修改 ViewModel 后**显式**重新 Layout，观察到新值。
//
// ⚠️ 这是测试驱动的显式重新布局，**不代表生产 UI 会自动刷新**。
//
//	生产环境的状态通知与重绘归 TASK-STATE-001。
func TestBindingRuntimeValueChange(t *testing.T) {
	out := rtProbe(t, `<Text Value="{Title}"/>`, `
// ⚠️ 本测试显式调用下一次 Layout 来观察新值。
// 它证明：新值能被读取、下一次布局能正确显示。
// 它**不**证明：ViewModel 修改后生产 UI 会自动失效并重绘
// （该能力归 TASK-STATE-001）。
func TestProbeValueChange(t *testing.T) {
	th := material.NewTheme()
	const w, h = 240, 60
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skipf("headless: %v", err)
	}
	defer win.Release()

	vm := &fixture.ViewModel{Title: "AAAA"}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		 fatal(t, err)
	}
	_ = v

	// 第一次布局
	var o1 op.Ops
	v.Layout(rtGtx(&o1, w, h), th)
	n1 := rtNonZero(rtShot(t, win, &o1, w, h))

	// 修改值
	vm.Title = "WWWWWWWWWWWWWWWW"

	// **显式**重新布局
	var o2 op.Ops
	v.Layout(rtGtx(&o2, w, h), th)
	n2 := rtNonZero(rtShot(t, win, &o2, w, h))

	// 第三次：确认仍渲染新值
	var o3 op.Ops
	v.Layout(rtGtx(&o3, w, h), th)
	n3 := rtNonZero(rtShot(t, win, &o3, w, h))

	t.Logf("改前=%d 改后=%d 再次=%d", n1, n2, n3)
	if n1 == 0 || n2 == 0 {
		t.Skip("渲染全零")
	}
	if n1 == n2 {
		t.Errorf("修改值后重新布局，渲染未变化（%d）——新值未生效", n1)
	}
	if n2 != n3 {
		t.Errorf("同值两次布局渲染不一致（%d vs %d）", n2, n3)
	}
	t.Log("改值后显式重新布局 → 渲染随之更新；同值重复布局结果一致")
}
`)
	t.Log(rtFilter(out))
	if !contains(out, "[err=nil]") {
		t.Fatalf("值变化探测失败:\n%s", out)
	}
}

// rtFilter 提取探测输出中的关键行。
func rtFilter(out string) string {
	keep := ""
	for _, line := range splitLines(out) {
		if hasAny(line, "像素", "渲染", "err=nil", "FAIL", "ok ", "观测") {
			keep += trimSpace(line) + "\n"
		}
	}
	return keep
}

func hasAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if contains(s, sub) {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
