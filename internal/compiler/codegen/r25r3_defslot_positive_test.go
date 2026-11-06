package codegen

// R25-R3 S1 正例：合法 Def/Slot 实例化 + Expanded + 真实点击。
// 形态来自 R25 协调者复核（coordinator_r2_defslot_runtime_test.go）：
// Def 内 <Row><Expanded><Slot/></Expanded></Row>，Page 以 <Card> 调用并
// 给 Slot 提供 <Button>。协调者已独立验证生成/编译/真实点击 PASS；
// 本守卫将其采纳为持久回归（撤销 R25-R2 "Def/Slot 不支持"的错误结论）。

import (
	"testing"
)

func TestR25R3RealDefSlotRuntime(t *testing.T) {
	full := `<Goxml Version="1" ViewModel="__VMREF__"><Defs><Def Name="Card"><Row Height="60"><Expanded><Slot/></Expanded></Row></Def></Defs><Page Route="/"><Card><Button Text="ok" Click="TapA"/></Card></Page></Goxml>`
	out := r25r2ProbeFull(t, r25r2SharedVM, full, `
func TestProbeR25R3RealDefSlot(t *testing.T) {
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	th := material.NewTheme()
	gtx := layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 60)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: r.Source()}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	r.Queue(
		pointer.Event{Position: f32.Pt(250, 30), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(250, 30), Kind: pointer.Release, Source: pointer.Mouse},
	)
	frame()
	if obj.CountA != 1 {
		t.Fatalf("合法实例化的 Def/Slot Expanded 未命中按钮: %d", obj.CountA)
	}
	fmt.Println("SCEN-DONE r25r3-real-defslot")
}
`)
	assertProbeHealth(t, "r25r3-real-defslot", out)
}
