package codegen

import (
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// `<Grid>` 生成专项测试（规范 §3.1.6）
//
// 断言口径与既有实现一致：
//   - 成功路径：`mustGen` 产出可 `go/format` 的源码；
//   - 失败路径：`gen` 返回 error **且源码为空**（`binding_failure_test.go:66-68`
//     的既定契约：失败不得产出部分源码）。
//
// 这里只覆盖 Codegen 侧；Parser 侧（Columns 必填、子项 Width 拒绝）已有
// `parser/grid_test.go` 与 `parser/diagcode_test.go`，不重复。
// ============================================================================

// gridBody 包一个 `<Grid>` 片段。
func gridBody(inner string) string {
	return `<Grid Columns="72 * 96" Gap="8">` + inner + `</Grid>`
}

// TestGridGeneratesFlexNesting 验证主分派已接入，且产出纵向 Flex + 横向 Flex 的嵌套形态。
func TestGridGeneratesFlexNesting(t *testing.T) {
	out := string(mustGen(t, gridBody(
		`<Text Value="a"/><Text Value="b"/><Text Value="c"/>`), defaultOpts()))

	for _, want := range []string{
		"layout.Flex{Axis: layout.Vertical",   // 外层：行
		"layout.Flex{Axis: layout.Horizontal", // 内层：列
	} {
		if !strings.Contains(out, want) {
			t.Errorf("生成的 Grid 缺少 %q\n源码:\n%s", want, out)
		}
	}
}

// TestGridFixedColumnExactConstraint 验证固定列生成精确宽度约束（规范 :290 前提 1）。
//
// 固定列必须是「Min==Max==列宽」，否则子项可以改变宽度，最后一行会错位。
func TestGridFixedColumnExactConstraint(t *testing.T) {
	out := string(mustGen(t, gridBody(
		`<Text Value="a"/><Text Value="b"/><Text Value="c"/>`), defaultOpts()))

	if !strings.Contains(out, "gtx.Constraints.Min.X = gtx.Dp(72)") {
		t.Errorf("固定列 72dp 未生成精确 Min 约束\n源码:\n%s", out)
	}
	if !strings.Contains(out, "gtx.Constraints.Max.X = gtx.Constraints.Min.X") {
		t.Errorf("固定列未把 Max 收成与 Min 相等（宽度可由子项改变）\n源码:\n%s", out)
	}
}

// TestGridFlexColumnWeight 验证弹性列按权重生成 Flexed（规范 §3.1.6 列定义）。
//
// Columns="72 * 96" 中 `*` 的权重是 1。
func TestGridFlexColumnWeight(t *testing.T) {
	out := string(mustGen(t, gridBody(
		`<Text Value="a"/><Text Value="b"/><Text Value="c"/>`), defaultOpts()))

	if !strings.Contains(out, "layout.Flexed(1,") {
		t.Errorf("弹性列 `*` 未生成 layout.Flexed(1, ...)\n源码:\n%s", out)
	}
}

// TestGridWeightedFlexColumn 验证 `N*` 的权重 N 被如实输出。
func TestGridWeightedFlexColumn(t *testing.T) {
	out := string(mustGen(t,
		`<Grid Columns="100 2*"><Text Value="a"/><Text Value="b"/></Grid>`, defaultOpts()))

	if !strings.Contains(out, "layout.Flexed(2,") {
		t.Errorf("`2*` 未生成 layout.Flexed(2, ...)\n源码:\n%s", out)
	}
}

// TestGridPadCellsKeepGapCount 验证最后一行补位是真实子项（规范 :307）。
//
// Columns 有 3 列、只有 4 个子项 ⇒ 第 2 行需补 2 个占位；
// 若补位不是真实 Flex 子项，该行少扣 2 个 Gap，弹性列会变宽。
func TestGridPadCellsKeepGapCount(t *testing.T) {
	out := string(mustGen(t, gridBody(
		`<Text Value="a"/><Text Value="b"/><Text Value="c"/><Text Value="d"/>`), defaultOpts()))

	// 3 个真实单元格 + 2 个补位 = 5 个 Flex 子项（Rigid/Flexed 各算一次出现）。
	childCount := strings.Count(out, "layout.Rigid(") + strings.Count(out, "layout.Flexed(")
	// 外层每行各 1 个 Rigid 包装（2 行）+ 单元格子项 5 个 = 7。
	if childCount < 5 {
		t.Fatalf("Flex 子项数 = %d，期望至少 5（3 单元格 + 2 补位）\n源码:\n%s", childCount, out)
	}
	if !strings.Contains(out, "layout.Spacer{}.Layout(gtx)") {
		t.Errorf("补位未生成 Spacer 占位\n源码:\n%s", out)
	}
}

// TestGridAlignMapsToRowAlignment 验证 `Grid Align` 映射到每行横向 Flex（规范 :316）。
func TestGridAlignMapsToRowAlignment(t *testing.T) {
	out := string(mustGen(t,
		`<Grid Columns="* *" Align="Center"><Text Value="a"/><Text Value="b"/></Grid>`, defaultOpts()))

	if !strings.Contains(out, "Alignment: layout.Middle") {
		t.Errorf("Align=Center 未映射到横向 Flex 的 Middle\n源码:\n%s", out)
	}
}

// TestGridGapOnBothAxes 验证一个 Gap 同时用于横向与纵向（规范 :300-302）。
func TestGridGapOnBothAxes(t *testing.T) {
	out := string(mustGen(t, gridBody(
		`<Text Value="a"/><Text Value="b"/><Text Value="c"/>`), defaultOpts()))

	if got := strings.Count(out, "Gap: gtx.Dp(8)"); got < 2 {
		t.Errorf("Gap 应同时出现在纵向与横向 Flex（期望 >=2 次，实际 %d）\n源码:\n%s", got, out)
	}
}

// TestGridChildHeightPreserved 验证直接子项的 Height 不被静默丢弃（R-002 要求 3）。
func TestGridChildHeightPreserved(t *testing.T) {
	out := string(mustGen(t,
		`<Grid Columns="* *"><Text Value="a" Height="40"/><Text Value="b"/></Grid>`, defaultOpts()))

	if !strings.Contains(out, "gtx.Constraints.Min.Y = gtx.Dp(40)") {
		t.Errorf("子项 Height=40 未生成垂直精确约束（被静默丢弃）\n源码:\n%s", out)
	}
}

// TestGridRejectsIfForInside 覆盖显式拒绝：Grid 单元格内出现 <If>/<For> 时报错。
//
// 理由：循环项个数编译期未知 ⇒ 无法确定落在哪一列，补位与 Gap 计数失去依据。
func TestGridRejectsIfForInside(t *testing.T) {
	// Parser 允许 <If> 作为子节点，故这里能进入 Codegen 的拒绝分支。
	_, err := gen(t,
		`<Grid Columns="* *"><If Condition="{Dirty}"><Then><Text Value="a"/></Then></If></Grid>`,
		defaultOpts())
	if err == nil {
		t.Fatal("Grid 内的 <If> 应被显式拒绝")
	}
	if !strings.Contains(err.Error(), "inside <Grid> is not supported") {
		t.Errorf("错误信息应说明 Grid 内不支持该节点，实际: %v", err)
	}
}

// TestGridGeneratedSourceIsFormatted 验证生成源码可被 go/format 解析（即语法合法）。
func TestGridGeneratedSourceIsFormatted(t *testing.T) {
	out := mustGen(t, gridBody(
		`<Text Value="a"/><Text Value="b"/><Text Value="c"/><Text Value="d"/>`), defaultOpts())

	if _, err := format.Source(out); err != nil {
		t.Fatalf("生成的 Grid 源码不是合法 Go：%v\n源码:\n%s", err, out)
	}
}

// TestGridGeneratedCodeCompiles 是**类型契约**的机器可验证形式。
//
// 为什么必须有这条：`sizeWrap` 的产物是 `layout.FlexChild`，不是
// `layout.Dimensions`。若把它直接放在单元格闭包的 `return` 之后，生成的源码
// 语法合法（`go/format` 能过）但**类型错误**——只有真实 `go build` 才能捕获。
// ZCode 复审正是发现了这类缺陷。
//
// 这里同时覆盖三种 Height 模态（Natural / Exact / Fill），
// 因为三者在 sizeWrap 内部走不同分支，任一分支放错位置都会编译失败。
func TestGridGeneratedCodeCompiles(t *testing.T) {
	repoRoot := repoRootOf(t)

	forms := map[string]string{
		"Natural": `<Grid Columns="72 * 96" Gap="8"><Text Value="a"/><Text Value="b"/><Text Value="c"/><Text Value="d"/></Grid>`,
		"Exact":   `<Grid Columns="72 * 96" Gap="8"><Text Value="a" Height="40"/><Text Value="b"/><Text Value="c"/></Grid>`,
		"Fill":    `<Grid Columns="* *"><Text Value="a" Height="Fill"/><Text Value="b"/></Grid>`,
	}

	for name, body := range forms {
		t.Run(name, func(t *testing.T) {
			out := mustGen(t, body, defaultOpts())

			dir := filepath.Join(repoRoot, "internal", "compiler", "codegen",
				"testdata", "tmpgrid"+randomSuffix())
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)

			if err := os.WriteFile(filepath.Join(dir, "gen.go"), out, 0o644); err != nil {
				t.Fatal(err)
			}

			rel, err := filepath.Rel(repoRoot, dir)
			if err != nil {
				t.Fatal(err)
			}
			// 显式路径（不带 ...）：匹配不到包时 go build 会报错，
			// 不会出现「warning: matched no packages + exit 0」的假绿。
			cmd := exec.Command("go", "build", "./"+filepath.ToSlash(rel))
			cmd.Dir = repoRoot
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
			bout, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Grid 生成代码无法编译（Height=%s）:\n%s\n%v\n源码:\n%s",
					name, bout, err, out)
			}
		})
	}
}

// TestGridCellWrapsFlexChildInsideFlex 是上述类型契约的**静态**伴随断言。
//
// 真实编译已能捕获错误，但那条测试依赖外部 `go build`（几十毫秒）。
// 这里用结构断言把「sizeWrap 的结果必须出现在 Flex.Layout 的实参位」锁住，
// 让未来重构在纯单元测试层就能得到提示。
func TestGridCellWrapsFlexChildInsideFlex(t *testing.T) {
	out := string(mustGen(t,
		`<Grid Columns="* *"><Text Value="a" Height="40"/><Text Value="b"/></Grid>`, defaultOpts()))

	// 单元格闭包体必须是纵向 Flex 调用，而不是直接 return sizeWrap 的产物。
	const flexAnchor = "return layout.Flex{Axis: layout.Vertical}.Layout(gtx,"
	if !strings.Contains(out, flexAnchor) {
		t.Errorf("单元格内缺少纵向 Flex 包裹（sizeWrap 产物是 FlexChild，不能直接 return）\n源码:\n%s", out)
	}
	// Height 的精确约束必须出现在该 Flex 调用**之后**（即作为其实参）：
	// 只匹配内容片段，不写死缩进——缩进随外层嵌套变化，锁死缩进会让断言
	// 因无关的格式调整而误红。
	idxFlex := strings.Index(out, flexAnchor)
	idxWrap := strings.Index(out, "gtx.Constraints.Min.Y = gtx.Dp(40)")
	if idxWrap < 0 {
		t.Fatalf("未找到 Height 的 sizeWrap 产物\n源码:\n%s", out)
	}
	if idxFlex < 0 || idxWrap < idxFlex {
		t.Errorf("sizeWrap 产物出现在 Flex.Layout 之前（位置错误，会变成 return 一个 FlexChild）\n源码:\n%s", out)
	}
}

// TestGridHeightFillDegradesToNatural 是 §3.1.7 的回归锁。
//
// 规范 :454：「子项 Height="Fill" 退化为自然高度：行高本身由该行内容决定，
// 用它作为填充基准会形成循环」。若生成纵向 `Flexed(1, ...)`，Gio 会把该单元格的
// Min.Y/Max.Y 撑到剩余高度 ⇒ 第一行吃掉整个 Grid 高度、后续行布局错位。
//
// 断言两点：① 不得出现纵向 Flexed(1)；② 与 Natural 高度生成结果一致。
func TestGridHeightFillDegradesToNatural(t *testing.T) {
	outFill := string(mustGen(t,
		`<Grid Columns="* *"><Text Value="a" Height="Fill"/><Text Value="b"/></Grid>`, defaultOpts()))

	// ① Grid 的**列**本身可能是 Flexed(权重)，因此只针对"纵向撑满"这一形态断言：
	//    sizeWrap 为垂直主轴 Fill 生成的片段带 `gtx.Constraints.Min.Y = gtx.Constraints.Max.Y`。
	if strings.Contains(outFill, "gtx.Constraints.Min.Y = gtx.Constraints.Max.Y") {
		t.Errorf("Height=Fill 生成了纵向撑满（应退化为自然高度）\n源码:\n%s", outFill)
	}

	// ② 与 Natural 的比较：用 go/format 规范化后再比较。
	//
	// 不比较原始片段的原因：生成器内部的缩进随嵌套层级变化，
	// 锁死缩进会让断言因无关格式调整而误红（R-003 已踩过这个坑）。
	// go/format 会把两者归一为同一份规范格式，差异只可能来自语义。
	outNatural := string(mustGen(t,
		`<Grid Columns="* *"><Text Value="a"/><Text Value="b"/></Grid>`, defaultOpts()))

	fillFmt, err := format.Source(mustGen(t,
		`<Grid Columns="* *"><Text Value="a" Height="Fill"/><Text Value="b"/></Grid>`, defaultOpts()))
	if err != nil {
		t.Fatalf("Fill 形态生成源码不可格式化: %v", err)
	}
	natFmt, err := format.Source([]byte(outNatural))
	if err != nil {
		t.Fatalf("Natural 形态生成源码不可格式化: %v", err)
	}
	if string(fillFmt) != string(natFmt) {
		t.Errorf("Height=Fill 与 Natural 的产物不同（Fill 未退化为自然高度）\nFill:\n%s\nNatural:\n%s",
			fillFmt, natFmt)
	}
}

// TestGridSpacerDoesNotFillRowHeight 锁住「Grid 内 Spacer 不得撑满行高」。
//
// `sizePlanFor` 对 Tag=="Spacer" 且 Height 为 Natural 时会**隐式提升为 Fill**
// （为 `<Column><Spacer/></Column>` 设计）。在 Grid 单元格里这会产生纵向
// Flexed(1)，把行高撑满。修复方式是在尺寸规划副本上抑制该特例。
func TestGridSpacerDoesNotFillRowHeight(t *testing.T) {
	out := string(mustGen(t,
		`<Grid Columns="* *"><Spacer/><Text Value="a"/></Grid>`, defaultOpts()))

	if strings.Contains(out, "gtx.Constraints.Min.Y = gtx.Constraints.Max.Y") {
		t.Errorf("Grid 内 Spacer 生成了纵向撑满\n源码:\n%s", out)
	}
	// 纵向 Flexed 只应来自列定义（列宽），不应来自单元格内层。
	// 本 Grid 两列都是 `*` ⇒ 恰好 2 个 Flexed(1)（每行两个单元格各一次外层列宽）。
	// 若 Spacer 又加了内层 Flexed(1)，计数会 > 2。
	if got := strings.Count(out, "layout.Flexed(1,"); got != 2 {
		t.Errorf("Flexed(1) 出现 %d 次，期望 2 次（仅列宽）；Spacer 不应再产生纵向 Flexed(1)\n源码:\n%s", got, out)
	}
}
