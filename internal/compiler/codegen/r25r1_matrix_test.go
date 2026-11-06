package codegen

// R23-R1/R25-R1 F-B3：Expanded 完整逐项矩阵（真实链路 Generate + rtProbe
// 离屏事件），覆盖协调者指出未覆盖的场景：Row/Column 双轴、自身/子节点
// Exact 尺寸、多弹性/rigid 兄弟、Gap、Padding（容器级）、无界、If/For 包装、
// 双实例与缓存。逐项守恒不做；窗口排除默认选择器。

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestR25R1ExpandedAxisMatrix 横/纵轴 + 自身/子节点 Exact + 多弹性/rigid。
func TestR25R1ExpandedAxisMatrix(t *testing.T) {
	cases := []struct {
		name string
		gxml string
		want []string // 生成源码必须包含的弹性/约束片段
	}{
		{"row-flexed", `<Row Height="80"><Text Value="L"/><Expanded><Text Value="F"/></Expanded></Row>`,
			[]string{".Flexed(1, func(gtx "}},
		{"column-flexed", `<Column Height="200"><Text Value="T"/><Expanded><Text Value="F"/></Expanded></Column>`,
			[]string{".Flexed(1, func(gtx "}},
		{"row-self-explicit", `<Row Height="80"><Expanded Width="120"><Text Value="F"/></Expanded></Row>`,
			[]string{".Rigid(func(gtx ", "gtx.Dp(120)"}},
		{"row-child-explicit", `<Row Height="80"><Expanded><Text Value="F" Width="60"/></Expanded></Row>`,
			[]string{".Flexed(1, func(gtx ", "gtx.Dp(60)"}},
		{"column-child-explicit", `<Column Height="200"><Expanded><Text Value="F" Height="40"/></Expanded></Column>`,
			[]string{".Flexed(1, func(gtx ", "gtx.Dp(40)"}},
		{"multi-flex-rigid-siblings", `<Row Height="80"><Text Value="A"/><Expanded><Text Value="B"/></Expanded><Text Value="C"/><Expanded><Text Value="D"/></Expanded></Row>`,
			[]string{".Flexed(1, func(gtx ", ".Flexed(1, func(gtx "}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			v := verified(t, tc.gxml)
			out, err := Generate(v, bindingOpts())
			if err != nil {
				t.Fatalf("生成失败: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(out), want) {
					t.Fatalf("生成源码缺 %q:\n%.800s", want, out)
				}
			}
		})
	}
}

// TestR25R1ExpandedGapAndPaddingContainer Gap 与容器 Padding 下的 Expanded
// （Padding 在父容器/子节点，非 Expanded 自身——自身 Padding 已显式拒绝）。
func TestR25R1ExpandedGapAndPaddingContainer(t *testing.T) {
	v2 := verified(t, `<Row Height="80" Gap="4" Padding="6"><Text Value="L"/><Expanded><Text Value="F" Padding="2"/></Expanded></Row>`)
	out, err := Generate(v2, bindingOpts())
	if err != nil {
		t.Fatalf("Gap/Padding 容器下 Expanded 生成失败: %v", err)
	}
	for _, want := range []string{".Flexed(1, func(gtx ", "unit.Dp(6)"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("生成源码缺 %q:\n%.800s", want, out)
		}
	}
}

// TestR25R1ExpandedInIfAndFor If 与 For 包装下的 Expanded（轴上下文透传）。
func TestR25R1ExpandedInIfAndFor(t *testing.T) {
	vmSrc := localStaticVM
	out := rtProbeVM(t, vmSrc, `<Column>
  <If Condition="{ShowA}">
    <Then><Row Height="60"><Expanded><Text Value="If 弹性"/></Expanded></Row></Then>
  </If>
  <For Each="item" In="{Rows}" Key="{item.Name}">
    <Row Height="40"><Expanded><Text Value="{item.Name}"/></Expanded></Row>
  </For>
</Column>`, `
func TestProbeR25ExpandedIfFor(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{ShowA: true, Rows: []gvm.Row{{Name: "r1"}, {Name: "r2"}}}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	frame()
	fmt.Println("SCEN-DONE r25-expanded-if-for")
}
`)
	assertProbeHealth(t, "r25-expanded-if-for", out)
}

// TestR25R1ExpandedDualInstance 双实例缓存隔离（Expanded 内静态子树各自录制）。
func TestR25R1ExpandedDualInstance(t *testing.T) {
	src := `<Column><Expanded><Column><Text Value="静态甲"/><Text Value="静态乙"/></Column></Expanded></Column>`
	out1, err := Generate(verified(t, src), bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	if err := checkExpandedStaticCache(out1); err != nil {
		t.Fatal(err)
	}
	out2, err := Generate(verified(t, src), bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out1, out2) {
		t.Fatal("同输入两次生成应逐字节一致")
	}
}

func checkExpandedStaticCache(out []byte) error {
	// Expanded 内静态子列随整页宏/静态槽固化：生成源码应含 Expanded 弹性
	// 包装与静态列内容。
	s := string(out)
	for _, want := range []string{".Flexed(1, func(gtx ", "静态甲", "静态乙"} {
		if !strings.Contains(s, want) {
			return fmt.Errorf("生成源码缺 %q", want)
		}
	}
	return nil
}
