package codegen

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// TASK-CODEGEN-001-1（子任务表中的 `<Spacer>` 分支）：`<Spacer>` 生成
//
// 契约依据：规范 §3.1「`<Spacer>`：占用剩余空间的弹性空白」、
// §3.1.1「`<Spacer>` 不允许包含子节点」、属性表（`Width`/`Height` 适用全部标签）。
//
// 两件事必须分开证明，否则会各自假绿：
//  1. **占位原语**用对了（`layout.Spacer`，它会把约束 Constrain 成实际尺寸）；
//  2. **尺寸决策**仍由 `sizeWrap` 负责，且「未给尺寸」时主轴是弹性项而不是 0。
//
// 第 2 条是本标签最容易写错的地方：若照搬其它节点的「自然尺寸」默认，
// `<Spacer/>` 会退化成 0 宽空块——AST 里确实有 `layout.Spacer`，
// 编译也通过，但「占用剩余空间」的语义完全丢失。真实几何证据见
// size_runtime_test.go 的 TestSpacerElasticRuntime / TestSpacerFixedRuntime。
// ============================================================================

// TestSpacerUsesPlaceholderPrimitive 生成物必须是 layout.Spacer 的 Constrain 占位。
func TestSpacerUsesPlaceholderPrimitive(t *testing.T) {
	src := mustGen(t, `<Row><Text Value="a"/><Spacer Width="8"/><Text Value="b"/></Row>`,
		defaultOpts())
	if n := strings.Count(string(src), "layout.Spacer{}.Layout(gtx)"); n != 1 {
		t.Errorf("应恰好一处 layout.Spacer{}.Layout(gtx)，实际 %d 处\n%s", n, src)
	}
	// 尺寸不得写进 Spacer 字面量：那会成为 sizeWrap 之外的第二处尺寸来源。
	if strings.Contains(string(src), "layout.Spacer{Width:") ||
		strings.Contains(string(src), "layout.Spacer{Height:") {
		t.Errorf("尺寸不应写进 Spacer 字面量（必须只由 sizeWrap 决定）\n%s", src)
	}
}

// TestSpacerSizeRouting 逐容器核对 `<Spacer>` 的 FlexChild 形态与约束改写。
//
// 断言的是**具体那一个子项**，不是「文件里出现过 Flexed」——后者既可能来自
// 别的节点，也可能落在错误的层级。
func TestSpacerSizeRouting(t *testing.T) {
	cases := []struct {
		name string
		body string
		// want 按 Flex 出现顺序给出子项；最后一个 Flex 才是被测容器。
		want [][]flexChild
	}{
		{
			// Row 主轴是 X：固定 8 → Rigid + 精确约束（不裁剪、不参与分配）
			name: "Row 内固定宽 8",
			body: `<Row><Text Value="a"/><Spacer Width="8"/><Text Value="b"/></Row>`,
			want: [][]flexChild{
				{{kind: "Rigid"}},
				{
					{kind: "Rigid"},
					{kind: "Rigid", assign: []string{"gtx.Constraints.Min.X", "gtx.Constraints.Max.X"}},
					{kind: "Rigid"},
				},
			},
		},
		{
			// 未给尺寸 → 主轴按 Fill 处理 → Flexed(1)（规范 §3.1「占用剩余空间」）
			name: "Row 内无尺寸 → 弹性",
			body: `<Row><Text Value="a"/><Spacer/><Text Value="b"/></Row>`,
			want: [][]flexChild{
				{{kind: "Rigid"}},
				{{kind: "Rigid"}, {kind: "Flexed"}, {kind: "Rigid"}},
			},
		},
		{
			// Column 主轴是 Y：同样退化为弹性
			name: "Column 内无尺寸 → 弹性",
			body: `<Column><Text Value="a"/><Spacer/><Text Value="b"/></Column>`,
			want: [][]flexChild{
				{{kind: "Rigid"}},
				{{kind: "Rigid"}, {kind: "Flexed"}, {kind: "Rigid"}},
			},
		},
		{
			// 只给 Height="Fill"：Row 的主轴（X）仍按默认弹性，
			// 交叉轴（Y）按 §3.1.7 抬 Min —— 两个方向各自独立解析。
			name: "Row 内 Height=Fill：主轴仍弹性 + 交叉轴抬 Min",
			body: `<Row><Spacer Height="Fill"/></Row>`,
			want: [][]flexChild{
				{{kind: "Rigid"}},
				{{kind: "Flexed", assign: []string{"gtx.Constraints.Min.Y"}, axis: "Y"}},
			},
		},
		{
			// Column 内只给 Width：交叉轴固定 20，主轴（Y）仍按弹性空白
			name: "Column 内只给 Width=20 → 弹性 + 交叉轴精确",
			body: `<Column><Spacer Width="20"/></Column>`,
			want: [][]flexChild{
				{{kind: "Rigid"}},
				{{
					kind: "Flexed",
					assign: []string{
						"gtx.Constraints.Min.X",
						"gtx.Constraints.Max.X",
					},
				}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := mustGen(t, tc.body, defaultOpts())
			got := flexCallsOf(t, src)
			if len(got) != len(tc.want) {
				t.Fatalf("Flex 数量 = %d, want %d\n%s", len(got), len(tc.want), src)
			}
			for i := range tc.want {
				if !flexChildrenEqual(got[i], tc.want[i]) {
					t.Errorf("第 %d 个 Flex 的子项 = %+v\nwant %+v\n%s", i, got[i], tc.want[i], src)
				}
			}
		})
	}
}

// flexChildrenEqual 逐项比较子项；用「格式化后相等」而不是 reflect.DeepEqual，
// 是为了让 nil 与空切片视为相同（两者在本测试里都表示「无赋值/无检查」）。
func flexChildrenEqual(a, b []flexChild) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].kind != b[i].kind || a[i].axis != b[i].axis {
			return false
		}
		if strings.Join(a[i].assign, "|") != strings.Join(b[i].assign, "|") {
			return false
		}
	}
	return true
}

// TestSpacerGeneratedCodeCompiles 真实编译：Spacer 的表达式必须是合法 Gio 调用。
func TestSpacerGeneratedCodeCompiles(t *testing.T) {
	for _, src := range []string{
		`<Row><Text Value="a"/><Spacer/><Text Value="b"/></Row>`,
		`<Row><Text Value="a"/><Spacer Width="8"/><Text Value="b"/></Row>`,
		`<Column><Text Value="a"/><Spacer Height="12"/><Text Value="b"/></Column>`,
		`<Spacer/>`,
	} {
		buildInTempModule(t, mustGen(t, src, defaultOpts()))
	}
}

// TestSpacerRejectsUpstreamViolation 防御上游不变式被破坏。
//
// Parser 已拒绝子节点与非尺寸属性（`<Spacer> cannot contain children`、
// `<Spacer> only accepts Width and Height`）。但 Codegen 不能把「上游一定拦住了」
// 当作理由而静默忽略——那会让一次前端回归变成「属性无声消失」。
func TestSpacerRejectsUpstreamViolation(t *testing.T) {
	g := &generator{imports: newImportSet()}

	if _, err := g.spacerExpr(&contracts.NodeIR{
		Tag:      "Spacer",
		Children: []*contracts.NodeIR{{Tag: "Text"}},
	}); err == nil {
		t.Error("<Spacer> 带子节点必须失败")
	}

	if _, err := g.spacerExpr(&contracts.NodeIR{
		Tag:   "Spacer",
		Attrs: []contracts.Attr{{Name: "Padding"}},
	}); err == nil {
		t.Error("<Spacer> 带非尺寸属性必须失败")
	}
}
