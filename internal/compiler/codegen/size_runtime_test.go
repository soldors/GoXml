package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// ============================================================================
// 尺寸映射的**真实布局**验证（TASK-CODEGEN-001-10 / -22，第二阶段第 3 步）
//
// 为什么必须有这一层：`sizeWrap` 的模板测试只能证明「生成的语句长得对」。
// 用户明确要求「不能只靠 AST 中出现了某个 Gio 常量就认定正确」——
// 交叉轴 Fill 是否**真的**改写了约束、固定尺寸是否**真的**不被父容器裁剪、
// 无界时是否**真的**不抬 Min，都必须在真实生成的代码、真实的 Gio 布局中观测。
//
// 观测手段：把生成代码放进临时模块，用 headless 后端离屏渲染，再扫描像素
// 求包围盒。位置本身就携带了尺寸信息——Gio 的 Flex 是按子项占位累加偏移的，
// 因此「第二个子项落在了哪里」可以直接反推第一个子项实际占了多少空间。
//
// 单位与字体前提：Metric 取 PxPerDp=1，文本高度实测 16px、单字符宽约 11px，
// 所以断言一律用**区间**（如 y>260）而不是精确像素，避免字体度量变化导致误报。
//
// 覆盖的四种行为：
//   1. 主轴 Fill → layout.Flexed(1)，占满剩余空间；
//   2. 交叉轴 Fill 有界 → Min 抬到 Max（Y 轴与 X 轴各一例，防轴向写反）；
//   3. 交叉轴 Fill 无界 → 不抬（否则会造出 1<<30 的高度）；
//   4. 固定尺寸溢出父容器 → 尺寸被精确保留，不被裁剪/拉回。
// ============================================================================

// sizeProbeHeader 是尺寸探测的公共部分（与生成代码同包）。
//
// 窗口固定 400x1200，比多数探测用的约束更大：**故意让帧缓冲大于约束**，
// 这样「溢出了父容器约束的内容」仍然落在可截图区域内——
// 若窗口与约束同尺寸，溢出部分在帧缓冲外，就分不清「溢出」与「被裁剪」。
const sizeProbeHeader = `package demo

import (
	"image"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

const (
	winW = 400
	winH = 1200
)

// sbox 是非透明像素的包围盒与计数。
type sbox struct{ N, MinX, MinY, MaxX, MaxY int }

func (b sbox) String() string {
	return "n=" + itoa(b.N) + " (" + itoa(b.MinX) + "," + itoa(b.MinY) + ")-(" +
		itoa(b.MaxX) + "," + itoa(b.MaxY) + ")"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	if neg {
		return "-" + string(d)
	}
	return string(d)
}

func sg(ops *op.Ops, max image.Point) layout.Context {
	return layout.Context{
		Ops:         ops,
		Constraints: layout.Constraints{Max: max},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
}

func sexact(ops *op.Ops, w, h int) layout.Context {
	g := sg(ops, image.Pt(w, h))
	g.Constraints = layout.Exact(image.Pt(w, h))
	return g
}

// sshot 渲染一帧并截图。
func sshot(t *testing.T, win *headless.Window, ops *op.Ops) *image.RGBA {
	t.Helper()
	if err := win.Frame(ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, winW, winH))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	return img
}

// snew 创建离屏窗口；失败说明本机无 headless 后端（由外层测试转为 skip，
// 不允许静默 pass——静默通过会让「无证据」冒充「已验证」）。
func snew(t *testing.T) *headless.Window {
	t.Helper()
	win, err := headless.NewWindow(winW, winH)
	if err != nil {
		t.Fatalf("NO_HEADLESS: %v", err)
	}
	return win
}

// sbbox 求区域内非透明像素的包围盒。
func sbbox(img *image.RGBA, r image.Rectangle) sbox {
	b := sbox{MinX: r.Max.X, MinY: r.Max.Y, MaxX: r.Min.X - 1, MaxY: r.Min.Y - 1}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.Pix[y*img.Stride+x*4+3] == 0 {
				continue
			}
			b.N++
			if x < b.MinX {
				b.MinX = x
			}
			if x > b.MaxX {
				b.MaxX = x
			}
			if y < b.MinY {
				b.MinY = y
			}
			if y > b.MaxY {
				b.MaxY = y
			}
		}
	}
	return b
}
`

// sizeRuntime 生成代码并在临时模块里跑探针。
//
// 无 headless 后端时整体 skip：本测试的结论全部来自像素几何，
// 拿不到像素就不该报告「通过」。
func sizeRuntime(t *testing.T, gxmlBody, probeBody string) {
	t.Helper()
	src := mustGen(t, gxmlBody, defaultOpts())
	out, err := genModuleWithProbe(t, src, sizeProbeHeader+probeBody)
	if strings.Contains(out, "NO_HEADLESS") {
		t.Skipf("本机无 headless 后端，尺寸几何验证跳过:\n%s", out)
	}
	if err != nil {
		t.Fatalf("运行时探测失败: %v\n%s", err, out)
	}
	// 探针内部的 t.Fatalf 会让 go test 失败，但 CombinedOutput 只有 err!=nil
	// 才能体现；这里再兜一层，防止探针断言失败被当成通过。
	if strings.Contains(out, "--- FAIL") || strings.Contains(out, "FAIL\t") {
		t.Fatalf("探针断言失败:\n%s", out)
	}
	t.Logf("%s", lastLogLines(out, 8))
}

// lastLogLines 取输出末尾若干行，便于在 go test 日志里看到实测坐标。
func lastLogLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// TestSizeMainFillRuntime 主轴 Fill → layout.Flexed(1) 占满剩余空间。
//
// GXML: <Page> 的子项主轴是 Y（Page 等价于 Column）。
// A 声明 Height="Fill"、B 无尺寸：若 Flexed 生效，B 会被推到容器底部（y≈284）；
// 若退化回 Rigid，A 只占自然高度 16px，B 落在 y≈16。
// 两者的像素位置相差一个数量级，无法用「碰巧」解释。
func TestSizeMainFillRuntime(t *testing.T) {
	sizeRuntime(t, `<Text Value="A" Height="Fill"/><Text Value="B"/>`, `
func TestProbeMainFill(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	var ops op.Ops
	d := v.Layout(sexact(&ops, 200, 300), th)
	if d.Size != image.Pt(200, 300) {
		t.Errorf("根尺寸 = %v, 期望 (200,300)", d.Size)
	}

	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)

	top := sbbox(img, image.Rect(0, 0, 200, 100))
	bottom := sbbox(img, image.Rect(0, 260, 200, 340))
	t.Logf("A(top)=%v B(bottom)=%v", top, bottom)

	if top.N == 0 {
		t.Fatal("A 未在顶部渲染")
	}
	if bottom.N == 0 {
		t.Fatal("B 未出现在底部：主轴 Fill 未生效（Flexed 没有占满 300-16 的剩余空间）")
	}
}
`)
}

// TestSizeCrossFillBoundedRuntime 交叉轴 Fill 在有界约束下把 Min 抬到 Max。
//
// GXML: <Row Align="End"> 内 A 声明 Height="Fill"。
// 对 Row（水平）而言 Height 是**交叉轴**（Y），且 Row 的交叉约束来自 Page 的
// 主轴剩余空间，是**有界但宽松**的 (0..300) —— 正是需要抬升的场景。
//
// 抬升生效 → A 的交叉尺寸变成 300 → maxCross=300 → B 被 Align=End 推到 y≈284。
// 抬升未生效 → maxCross 仍是自然高度 16 → B 留在 y≈0。
// 注意：这里**不能**只看 A 的像素——A 的文本无论多高都画在顶部，
// 必须看「同容器内另一个子项被推到哪」才能反映 maxCross 的变化。
func TestSizeCrossFillBoundedRuntime(t *testing.T) {
	sizeRuntime(t,
		`<Row Align="End"><Text Value="A" Height="Fill"/><Text Value="B"/></Row>`, `
func TestProbeCrossFillBounded(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	var ops op.Ops
	v.Layout(sexact(&ops, 200, 300), th)

	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)

	top := sbbox(img, image.Rect(0, 0, 200, 100))
	bottom := sbbox(img, image.Rect(0, 260, 200, 340))
	t.Logf("A(top)=%v B(bottom)=%v", top, bottom)

	if top.N == 0 {
		t.Fatal("A 未渲染")
	}
	if bottom.N == 0 {
		t.Fatal("B 未出现在底部：交叉轴 Fill 未把 Min.Y 抬到 Max.Y（maxCross 仍是自然高度）")
	}
}
`)
}

// TestSizeCrossFillUnboundedRuntime 无界约束下交叉轴 Fill **不得**抬 Min。
//
// 同一个 GXML，把根约束换成 (0..1<<30)：此时 Max.Y 是无界哨兵值，
// 若照搬「Min=Max」就会把 A 撑成 1073741824px，根尺寸随之为 1e9。
// 因此这个用例同时用两个信号判：
//   - 数值：根返回高度的量级（不依赖像素，最硬的证据）；
//   - 几何：B 仍留在顶部（无界时没有「剩余空间」可撑开）。
func TestSizeCrossFillUnboundedRuntime(t *testing.T) {
	sizeRuntime(t,
		`<Row Align="End"><Text Value="A" Height="Fill"/><Text Value="B"/></Row>`, `
func TestProbeCrossFillUnbounded(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	var ops op.Ops
	d := v.Layout(sg(&ops, image.Pt(1<<30, 1<<30)), th)
	t.Logf("无界约束下根尺寸 = %v", d.Size)
	if d.Size.Y > 1000 {
		t.Fatalf("根高度 = %d，说明无界时仍把 Min.Y 抬成了 Max.Y（应退化为自然尺寸）", d.Size.Y)
	}

	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)

	all := sbbox(img, image.Rect(0, 0, 200, 300))
	t.Logf("全部文本包围盒=%v", all)
	if all.N == 0 {
		t.Fatal("无界约束下没有任何文本被渲染")
	}
	if all.MaxY > 100 {
		t.Fatalf("文本底边 y=%d：B 被撑到了底部，说明无界时仍抬升了 Min.Y", all.MaxY)
	}
}
`)
}

// TestSizeCrossFillXAxisRuntime 交叉轴 Fill 的 X 轴分支必须同样生效。
//
// 这一例专门防「轴向写反 / 写死 .Y」：三层容器把 **X** 变成交叉轴——
// <Page>（等价 Column）→ <Row>（主 X）→ <Column>（该 Column 的 X 来自 Row 的主轴
// 剩余，是宽松的）；再往下的 <Row Width="Fill"> 的 Width 就是交叉轴，对应 X。
//
// 生效 → 内层 Row 的 Min.X=Max.X=200 → Justify=End 把文本推到右边界（x≈189..200）；
// 未生效 → mainMin 仍为 0，Justify 无剩余空间可分配，文本留在 x≈0。
func TestSizeCrossFillXAxisRuntime(t *testing.T) {
	sizeRuntime(t,
		`<Row><Column><Row Width="Fill" Justify="End"><Text Value="A"/></Row></Column></Row>`, `
func TestProbeCrossFillX(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	var ops op.Ops
	v.Layout(sexact(&ops, 200, 300), th)

	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)

	left := sbbox(img, image.Rect(0, 0, 100, 60))
	right := sbbox(img, image.Rect(150, 0, 200, 60))
	t.Logf("左半=%v 右半=%v", left, right)

	if right.N == 0 {
		t.Fatal("文本未靠右：交叉轴 Fill 的 X 分支未抬高 Min.X（Justify=End 没有剩余空间可分配）")
	}
	if left.N != 0 {
		t.Fatalf("左半区仍有文本 (%v)：文本未被推到右边界", left)
	}
}
`)
}

// TestSizeFixedOverflowRuntime 固定尺寸溢出父容器时既不裁剪也不拉回。
//
// GXML: <Page> 的子项 <Row Height="900">，根约束只有 300 高。
// 900 是 Page（垂直）的**主轴**尺寸 → 精确约束 Min=Max=900，
// 于是 Row 之后的 B 被放在 y=900 —— **在父容器 300 之外**。
//
// 三种可能的错误各自对应一个不同的 y：
//   - 正确：B 在 y≈900（帧缓冲 1200 高，能截到）；
//   - 把固定值裁剪/钳制到父容器：B 在 y≈300；
//   - 忽略固定尺寸：B 在 y≈16。
//
// B 额外声明 Height="40"：Row 已吃掉全部主轴空间，remaining 归零，
// 若不显式给 B 一个精确高度，它会拿到 0 高约束而无法验证位置。
func TestSizeFixedOverflowRuntime(t *testing.T) {
	sizeRuntime(t,
		`<Row Height="900"><Text Value="A"/></Row><Text Value="B" Height="40"/>`, `
func TestProbeFixedOverflow(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	var ops op.Ops
	v.Layout(sexact(&ops, 300, 300), th)

	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)

	top := sbbox(img, image.Rect(0, 0, 300, 100))
	overflow := sbbox(img, image.Rect(0, 860, 300, 980))
	pulled := sbbox(img, image.Rect(0, 300, 300, 360))
	t.Logf("A(0..100)=%v B(860..980)=%v B(300..360)=%v", top, overflow, pulled)

	if top.N == 0 {
		t.Fatal("A 未在顶部渲染")
	}
	if overflow.N == 0 {
		t.Fatalf("B 未出现在 y≈900：固定尺寸 900 未生效，或被裁剪到父容器的 300")
	}
	if pulled.N != 0 {
		t.Fatalf("y≈300 处出现文本 (%v)：固定尺寸被拉回到父容器边界", pulled)
	}
}
`)
}

// TestSizesExampleGeneratesSizedChildren 用仓库自带示例 `examples/gxml/valid/sizes.gxml`
// 跑通「有尺寸的容器」这条链路（生成 → 编译）。
//
// 为什么值得单独跑示例：示例是文档与语言规范对外的「可运行承诺」，
// 但它此前没有被任何 codegen 测试覆盖（全仓无 Go 文件引用 sizes.gxml），
// 也就是说尺寸接入改坏了示例，前面所有单元测试都不会报警。
//
// 结构期望（由示例内容推导）：
//   - <Row> 内两个 Width="Fill" 的 <Text> 是**主轴** Fill → 2 个 layout.Flexed；
//   - <Column> 内 Width="Fill" Height="24" 的 <Text> 只有交叉轴 Fill → 1 处有界检查；
//   - Width="80.5" 必须以解析后的十进制字面量出现在 gtx.Dp(...) 内。
//
// 已知缺口（本测试不覆盖、也不假装覆盖）：示例里 <Column Padding="16"> 的
// Padding 目前被 Codegen 忽略（属 TASK-CODEGEN-001-21 范围），因此这里只验证
// 尺寸映射与可编译性，不验证示例的整体视觉。
func TestSizesExampleGeneratesSizedChildren(t *testing.T) {
	path := filepath.Join(repoRootOf(t), "examples", "gxml", "valid", "sizes.gxml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取示例: %v", err)
	}

	p := runFrontend(t, string(raw))
	r := analyzer.Analyze("sizes.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		t.Fatalf("Analyzer 未通过: %v", codes(r))
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatalf("取凭证失败: %v", err)
	}

	out, err := Generate(v, defaultOpts())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}

	flexed, guards := 0, 0
	for _, kids := range flexCallsOf(t, out) {
		for _, k := range kids {
			if k.kind == "Flexed" {
				flexed++
			}
			if k.axis != "" {
				guards++
			}
		}
	}
	if flexed != 2 {
		t.Errorf("layout.Flexed 数量 = %d, want 2（Row 内两个 Width=Fill）", flexed)
	}
	if guards != 1 {
		t.Errorf("有界检查数量 = %d, want 1（Column 内 Width=Fill 的交叉轴 Fill）", guards)
	}
	if !strings.Contains(string(out), "gtx.Dp(80.5)") {
		t.Errorf("Width=\"80.5\" 未生成 gtx.Dp(80.5)")
	}

	buildInTempModule(t, out)
}

// ============================================================================
// 动态路径的真实布局验证（<For>，需要 repo 内临时包以使用 fixture ViewModel）
//
// 与上面的静态用例同样的观测思路：让兄弟节点的落点反映 Flexed 的实际占位。
// 结构断言只能证明「append 时包了 Flexed」，不能证明「Flexed 真的分配到了剩余空间」。
// ============================================================================

// rtSize 在 repo 内临时包中跑像素几何探针。
//
// 无 headless 后端时 skip（不静默 pass）：本测试的结论全部来自像素。
func rtSize(t *testing.T, gxmlBody, probeBody string) {
	t.Helper()
	out := rtProbe(t, gxmlBody, probeBody)
	if strings.Contains(out, "NO_HEADLESS") {
		t.Skipf("本机无 headless 后端，动态路径几何验证跳过:\n%s", out)
	}
	if strings.Contains(out, "--- FAIL") || strings.Contains(out, "FAIL\t") {
		t.Fatalf("探针断言失败:\n%s", out)
	}
	t.Logf("%s", lastLogLines(out, 8))
}

// TestSizeDynamicForMainFillRuntime <For> 内的 Height="Fill" 必须是真 Flexed。
//
// GXML: <For> 两项 + 紧随其后的 Z。Page 是垂直容器，Height 是**主轴**。
// 若每项 Flexed(1)：Rigid 的 Z 先占 16px → 剩余 284 由两项均分（各 142），
//
//	于是 item2 落在 y≈147、Z 落在 y≈284。
//
// 若退化为 Rigid：三项都挤在顶部（y≈5 / 21 / 37），底部区域无像素。
func TestSizeDynamicForMainFillRuntime(t *testing.T) {
	rtSize(t,
		`<For Each="item" In="{Items}"><Text Value="{item.Name}" Height="Fill"/></For><Text Value="Z"/>`, `
type box3 struct{ n, minY, maxY int }

func bboxIn(img *image.RGBA, r image.Rectangle) box3 {
	b := box3{minY: r.Max.Y, maxY: r.Min.Y - 1}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.Pix[y*img.Stride+x*4+3] == 0 {
				continue
			}
			b.n++
			if y < b.minY {
				b.minY = y
			}
			if y > b.maxY {
				b.maxY = y
			}
		}
	}
	return b
}

func TestProbeForMainFill(t *testing.T) {
	const fbW, fbH = 300, 1200
	th := material.NewTheme()
	vm := &fixture.ViewModel{Items: []fixture.Item{{Name: "aa"}, {Name: "bb"}}}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatalf("构造: %v", err)
	}

	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Constraints: layout.Exact(image.Pt(200, 300)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	v.Layout(gtx, th)

	win, err := headless.NewWindow(fbW, fbH)
	if err != nil {
		t.Fatalf("NO_HEADLESS: %v", err)
	}
	defer win.Release()
	if err := win.Frame(&ops); err != nil {
		t.Fatalf("Frame: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, fbW, fbH))
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("Screenshot: %v", err)
	}

	top := bboxIn(img, image.Rect(0, 0, 200, 100))
	mid := bboxIn(img, image.Rect(0, 120, 200, 190))
	bottom := bboxIn(img, image.Rect(0, 260, 200, 340))
	t.Logf("top=%+v mid=%+v bottom=%+v", top, mid, bottom)

	if top.n == 0 {
		t.Fatal("列表项未渲染")
	}
	if mid.n == 0 {
		t.Fatalf("y≈147 处无文本：第二项未被 Flexed 分配 142px 高度（可能退化成了 Rigid）")
	}
	if bottom.n == 0 {
		t.Fatalf("Z 未出现在 y≈284：<For> 内的 Height=Fill 未真正占用剩余空间")
	}
}
`)
}
