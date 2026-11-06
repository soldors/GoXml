package codegen

// R25-B B3：<Expanded> 离屏判别矩阵（真实链路 Analyzer→Codegen→go test→
// 离屏 Layout + 真实指针事件）。弹性有效性以**事件命中**判据验证——
// Expanded 拉伸占据剩余宽度后，x=250 的点击才可能命中其内部按钮；
// 弹性失效（退化 Rigid 自然尺寸）时按钮脱靶。变异在独立 overlay。

import (
	"testing"
)

func TestR25ExpandedRowFlexMatrix(t *testing.T) {
	vmSrc := `package vm

// VM 是 R25-B 弹性命中验收用 ViewModel。
type VM struct{ TapCount int }

// Tap 是 Expanded 内按钮的处理器。
func (vm *VM) Tap() { vm.TapCount++ }
`
	out := rtProbeVM(t, vmSrc, `<Row Height="60">
  <Text Value="左"/>
  <Expanded><Button Text="点我" Click="Tap"/></Expanded>
</Row>`, `
func TestProbeR25ExpandedRowFlex(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 60)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	click := func(x float32) {
		r.Queue(
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
	}
	// 弹性拉伸判据：Expanded 占据「左」之后到行尾的全部宽度——x=250 必命中。
	click(250)
	if obj.TapCount != 1 {
		t.Fatalf("Expanded 应拉伸占据剩余宽度：x=250 命中 %d 次（弹性失效则收缩为自然宽度而脱靶）", obj.TapCount)
	}
	fmt.Println("SCEN-DONE r25-expanded-row-flex")
}
`)
	assertProbeHealth(t, "r25-expanded-row-flex", out)
}
