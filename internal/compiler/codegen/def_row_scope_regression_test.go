package codegen

import (
	"bytes"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// verifiedFull 与 verified 相同，但接受完整文档源码（含 <Defs>）。
//
// 动机：Def/Slot 展开发生在 Expander，宏体与调用点的关系只有走完整
// 前端才能观察到；仅包 body 的 verified 无法表达 <Defs>。
func verifiedFull(t *testing.T, src string) *analyzer.VerifiedDoc {
	t.Helper()
	p := runFrontend(t, src)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		t.Fatalf("Analyzer 未通过: %v", codes(r))
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatalf("取凭证失败: %v", err)
	}
	return v
}

// TestForRowScopeAfterDefExpansion 验证 2f003066 引入的行作用域机制
// 在 Def/Slot 宏展开之后仍然成立（任务书 PRODUCT-R2-ZCODE 切片 1）：
//
//  1. 同一个 <Def> 被实例化两次，每个实例内的 <For> 行池互相独立且都已接线
//     （点击两个实例的行都能把各自的 item.ID 传给处理器）；
//  2. 列表收缩后，**两个**实例的过期行池都被清理，不残留悬挂 Clickable；
//  3. 生成字节确定。
//
// 无窗口：合成 input.Router + pointer 队列，真实 go build + 运行生成的代码。
func TestForRowScopeAfterDefExpansion(t *testing.T) {
	src := `<Goxml Version="1" ViewModel="` + vmPath + `">
  <Defs>
    <Def Name="RowCard">
      <For Each="item" In="{Items}" Key="{item.ID}">
        <Button Text="choose" Click="RecordNum(item.ID)"/>
      </For>
    </Def>
  </Defs>
  <Page Route="/">
    <RowCard/>
    <RowCard/>
  </Page>
</Goxml>`

	v := verifiedFull(t, src)
	first, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	second, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("重复生成失败: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("两次生成字节必须一致")
	}
	runScopeProbe(t, first, `
func TestProbeDefRowScope(t *testing.T) {
	vm := &fixture.ViewModel{Items: []fixture.Item{{ID: 12}, {ID: 34}}}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil { t.Fatal(err) }
	var router input.Router
	gtx := layout.Context{
		Ops: new(op.Ops),
		Source: router.Source(),
		Constraints: layout.Exact(image.Pt(200, 400)),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	th := material.NewTheme()
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		router.Frame(gtx.Ops)
	}
	frame()
	click := func(y float32) {
		router.Queue(
			pointer.Event{Position: f32.Pt(20, y), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(20, y), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
	}
	click(15)  // 实例 1 第 1 行
	if vm.LastNum != 12 { t.Fatalf("实例1第1行 = %d, want 12", vm.LastNum) }
	click(50)  // 实例 1 第 2 行
	if vm.LastNum != 34 { t.Fatalf("实例1第2行 = %d, want 34", vm.LastNum) }
	click(85)  // 实例 2 第 1 行：同一宏的第二次实例化必须有自己的行池
	if vm.LastNum != 12 { t.Fatalf("实例2第1行 = %d, want 12（池串扰或未接线）", vm.LastNum) }
	click(120) // 实例 2 第 2 行
	if vm.LastNum != 34 { t.Fatalf("实例2第2行 = %d, want 34", vm.LastNum) }
	// 收缩前：两个实例的行池各含全部 Key（12、34）。
	if len(v.rowClicks0) != 2 || v.rowClicks0[12] == nil || v.rowClicks0[34] == nil {
		t.Fatalf("实例1收缩前行池 = %v, want 键 12 与 34", v.rowClicks0)
	}
	if len(v.rowClicks1) != 2 || v.rowClicks1[12] == nil || v.rowClicks1[34] == nil {
		t.Fatalf("实例2收缩前行池 = %v, want 键 12 与 34", v.rowClicks1)
	}
	// 列表收缩：两个实例的过期行（键 34）都必须清理，存活键 12 保留。
	vm.Items = vm.Items[:1]
	frame()
	if len(v.rowClicks0) != 1 || v.rowClicks0[12] == nil || v.rowClicks0[34] != nil {
		t.Fatalf("实例1收缩后行池 = %v, want 仅键 12", v.rowClicks0)
	}
	if len(v.rowClicks1) != 1 || v.rowClicks1[12] == nil || v.rowClicks1[34] != nil {
		t.Fatalf("实例2收缩后行池 = %v, want 仅键 12", v.rowClicks1)
	}
}
`)
}
