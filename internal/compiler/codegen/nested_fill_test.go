package codegen

import (
	"reflect"
	"strings"
	"testing"
)

// ============================================================================
// TASK-CODEGEN-001-24 / -25：嵌套双 Fill 与固定尺寸溢出（卡内命名用例）
//
// 这两项的语义此前已有等价证据（size_runtime_test.go 的
// TestSizeCrossFillXAxisRuntime / TestSizeFixedOverflowRuntime），但用例名与
// 任务卡的验收命令不一致——按卡里的 `-run` 命令跑不到任何用例，
// 门禁等于空转。这里补齐卡内命名的用例，并让它们各自**同时**给出
// 结构断言与真实几何断言，而不是只换个名字。
// ============================================================================

// TestNestedFill 两级 Fill 各走自己的主轴。
//
// 结构：
//
//	<Column>                      主轴 Y
//	└── <Row Height="Fill">       Height 在 Column 上是主轴 → Flexed(1)
//	    ├── <Text Width="Fill"/>  Width 在 Row 上是主轴   → Flexed(1)
//	    └── <Text/>
//
// 关键点：同一个 `Fill` 字面量在两层里角色不同（外层是 Y、内层是 X），
// 这正是"按父容器方向区分"要证明的东西。若实现只认 `Flexed(1)` 而不看轴向，
// 结构上可能碰巧通过——所以必须配合几何证据（见 TestNestedFillRuntime）。
func TestNestedFill(t *testing.T) {
	src := mustGen(t,
		`<Column><Row Height="Fill"><Text Value="A" Width="Fill"/><Text Value="B"/></Row>`+
			`<Text Value="C"/></Column>`, defaultOpts())

	got := flexCallsOf(t, src)
	if len(got) != 3 {
		t.Fatalf("Flex 数量 = %d, want 3（Page / Column / Row）\n%s", len(got), src)
	}
	// Page 的子项是 Column 本身，无尺寸
	if !reflect.DeepEqual(got[0], []flexChild{{kind: "Rigid"}}) {
		t.Errorf("Page 子项 = %+v, want Rigid", got[0])
	}
	// Column：Row 主轴 Fill → Flexed；C 自然尺寸 → Rigid
	wantCol := []flexChild{{kind: "Flexed"}, {kind: "Rigid"}}
	if !reflect.DeepEqual(got[1], wantCol) {
		t.Errorf("Column 子项 = %+v, want %+v", got[1], wantCol)
	}
	// Row：A 主轴 Fill → Flexed；B 自然尺寸 → Rigid
	wantRow := []flexChild{{kind: "Flexed"}, {kind: "Rigid"}}
	if !reflect.DeepEqual(got[2], wantRow) {
		t.Errorf("Row 子项 = %+v, want %+v", got[2], wantRow)
	}
	// 主轴 Fill 不应产生任何有界检查：那是交叉轴的事
	for i, kids := range got {
		for _, k := range kids {
			if k.axis != "" {
				t.Errorf("第 %d 个 Flex 出现有界检查（轴 %s）：主轴 Fill 不应生成 if", i, k.axis)
			}
		}
	}
}

// TestFixedOverflow 父容器 Max=150、子 Width=200 必须生成 Exact(200)，且不裁剪。
//
// 规范 §3.1.5：固定尺寸超出父容器可用空间时**允许溢出**，不裁剪、不报错。
// 生成物必须只是把 Min/Max 都设成 200，不得出现任何 Clip/Constrain 调用
// ——后者会把 200 悄悄拉回 150，而"拉回"在视觉上表现为内容被压扁，
// 很难与"内容本身就这么宽"区分。
func TestFixedOverflow(t *testing.T) {
	src := mustGen(t, `<Row><Text Value="A" Width="200"/><Text Value="B"/></Row>`, defaultOpts())

	got := flexCallsOf(t, src)
	if len(got) != 2 {
		t.Fatalf("Flex 数量 = %d, want 2（Page / Row）\n%s", len(got), src)
	}
	// Page 的交叉轴约束来自根 ctx，不参与本用例
	want := []flexChild{
		{kind: "Rigid", assign: []string{"gtx.Constraints.Min.X", "gtx.Constraints.Max.X"}},
		{kind: "Rigid"},
	}
	if !reflect.DeepEqual(got[1], want) {
		t.Errorf("Row 子项 = %+v, want %+v", got[1], want)
	}
	// 溢出值必须原样是 200，且经 gtx.Dp 换算
	if !strings.Contains(string(src), "gtx.Dp(200)") {
		t.Errorf("固定宽度应生成为 gtx.Dp(200)\n%s", src)
	}
	for _, bad := range []string{"Clip", "ClipRect"} {
		if strings.Contains(string(src), "layout."+bad) {
			t.Errorf("固定尺寸溢出时不得生成裁剪 %s（规范 §3.1.5）\n%s", bad, src)
		}
	}
}

// TestNestedFillRuntime 两级 `Fill` 各自生效的真实证据（-24 的几何面）。
//
// 判据选得很硬：外层 `Height="Fill"` 若失效，`C` 会紧跟在 Row 的自然高度之后
// （y≈16）；生效则被推到 y≈284。内层 `Width="Fill"` 若失效，`B` 会紧跟在
// A 之后（x≈11）；生效则停在右边距处（x≈290）。两个坐标互不干扰——
// 一个是 Y、一个是 X，正好对应"两级的 Fill 走的是不同的轴"。
func TestNestedFillRuntime(t *testing.T) {
	sizeRuntime(t,
		`<Column><Row Height="Fill"><Text Value="A" Width="Fill"/><Text Value="B"/></Row>`+
			`<Text Value="C"/></Column>`, `
func TestProbeNestedFill(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	var ops op.Ops
	d := v.Layout(sexact(&ops, 300, 300), th)
	if d.Size != image.Pt(300, 300) {
		t.Errorf("根尺寸 = %v, 期望 (300,300)", d.Size)
	}

	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)

	row := sbbox(img, image.Rect(0, 0, 300, 60))     // A 与 B 在 Row 内
	bottom := sbbox(img, image.Rect(0, 260, 300, 340)) // C 应被推到容器底部
	right := sbbox(img, image.Rect(250, 0, 300, 60))   // B 应被推到右边
	t.Logf("row=%v bottom=%v right=%v", row, bottom, right)

	if row.N == 0 {
		t.Fatal("Row 内没有任何文本")
	}
	if bottom.N == 0 {
		t.Fatal("C 未出现在底部：外层 Height=\"Fill\" 未生效（Row 未占满剩余高度）")
	}
	if right.N == 0 {
		t.Fatal("B 未出现在右侧：内层 Width=\"Fill\" 未生效（A 未占满 Row 的宽度）")
	}
}
`)
}

// TestFixedOverflowRuntime 固定宽度超出父容器时不裁剪（-25 的几何面）。
//
// 父容器 Max=150、子 `Width="200"`：允许溢出意味着 A 占满 200px，
// 其后的 B 落在 x≈200——即在父容器 150 之外、但仍在 400px 帧缓冲之内
// （窗口刻意大于约束，否则"溢出"与"被裁剪"在截图里无法区分）。
// 若被裁剪/拉回，B 会落在 x≈150。
//
// B 必须自己也给一个固定宽度：A 吃掉 200 之后 Row 的 remaining 被钳到 0，
// 兄弟子项拿到的主轴上限就是 0；不给 B 尺寸它会被压成 0 宽而**看不见**，
// 于是"B 消失"与"B 被裁剪"会被混为一谈。
func TestFixedOverflowRuntime(t *testing.T) {
	sizeRuntime(t, `<Row><Text Value="A" Width="200"/><Text Value="B" Width="40"/></Row>`, `
func colRuns(img *image.RGBA, r image.Rectangle) [][2]int {
	var runs [][2]int
	inRun, start := false, 0
	for x := r.Min.X; x < r.Max.X; x++ {
		any := false
		for y := r.Min.Y; y < r.Max.Y; y++ {
			if img.Pix[y*img.Stride+x*4+3] != 0 {
				any = true
				break
			}
		}
		if any && !inRun {
			inRun, start = true, x
		}
		if !any && inRun {
			inRun = false
			runs = append(runs, [2]int{start, x - 1})
		}
	}
	if inRun {
		runs = append(runs, [2]int{start, r.Max.X - 1})
	}
	return runs
}

func TestProbeFixedOverflow(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}

	var ops op.Ops
	v.Layout(sexact(&ops, 150, 40), th)

	win := snew(t)
	defer win.Release()
	img := sshot(t, win, &ops)

	runs := colRuns(img, image.Rect(0, 0, 400, 40))
	t.Logf("文本列区间 = %v", runs)
	if len(runs) != 2 {
		t.Fatalf("应有两段文本（A/B），实际 %d 段：%v", len(runs), runs)
	}
	start := runs[1][0]
	if start < 180 {
		t.Fatalf("B 起点 x=%d：固定宽度 200 被裁剪/拉回到父容器 150", start)
	}
	if start > 230 {
		t.Fatalf("B 起点 x=%d：固定宽度不是 200（dp→px 或数值来源有误）", start)
	}
}
`)
}
