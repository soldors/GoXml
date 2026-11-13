package widgets

import (
	"reflect"
	"testing"

	"gioui.org/io/key"
	"gioui.org/layout"
)

// R29-B R1 键盘兼容正反例（协调者 b-probe 反例的回归扩展）：
//   - Shift 导航只在多选开启时被消费；单选模式 Shift+方向键不被网格消费、
//     可被同帧外部事件消费者接收（R28 行为与外部消费者合同不变）；
//   - 多选普通导航在首尾钳制处仍收拢为当前锚行（不重滚、不伪造 Clicked）；
//     单选边界保持「无操作」；
//   - 跨轴：键盘导航与边界收拢不影响横向偏移；
//   - 非空选择与失效锚点：Shift 扩展锚点失效时退化为单行。
// R28 原守卫（TestKBOptInOutDefaultOff/TestKBEditorFocusIsolation 等）全部保留。

// externalKeyConsumer 是同帧注册的外部按键消费者（焦点标签独立），
// 用于断言网格未消费的修饰键事件仍可被其它消费者接收。
type externalKeyConsumer struct {
	seen []key.Name
	tag  struct{}
}

func (c *externalKeyConsumer) poll(gtx layout.Context) {
	for {
		// 测试助手用全局 catch-all（无 Focus）：网格未消费时事件仍在
		// 队列里，可被任意后注册消费者接收。
		ev, ok := gtx.Source.Event(key.Filter{
			Name:     key.NameDownArrow,
			Required: key.ModShift,
		})
		if !ok {
			return
		}
		if e, isKey := ev.(key.Event); isKey && e.State == key.Press {
			c.seen = append(c.seen, e.Name)
		}
	}
}

func TestR1ShiftNotConsumedSingleSelectExternalConsumer(t *testing.T) {
	g, h := kbSetupFocused(t, 100)
	kbKey(h, key.NameDownArrow)
	first, ok := g.SelectedRowID()
	if !ok {
		t.Fatal("plain Down did not select")
	}

	ext := &externalKeyConsumer{}
	h.overlay = func(gtx layout.Context) {
		g.Layout(gtx, h.th)
		ext.poll(gtx) // 后于网格注册：网格若消费则收不到
	}
	// Gio 语义：排队键事件按「上一帧注册的持久过滤器」把关后才进入
	// 当帧分发——外部消费者的过滤器必须先于事件帧注册一帧。
	h.frame()
	h.frame(key.Event{Name: key.NameDownArrow, State: key.Press, Modifiers: key.ModShift})
	if after, _ := g.SelectedRowID(); after != first {
		t.Fatalf("单选模式 Shift+Down 不得改变选择：%q → %q", first, after)
	}
	if len(ext.seen) != 1 {
		t.Fatalf("外部消费者应收到 Shift+Down：%v", ext.seen)
	}
	// Ctrl+Down：不在网格 Optional 集内，不消费、不改变选择。
	h.frame(key.Event{Name: key.NameDownArrow, State: key.Press, Modifiers: key.ModCtrl})
	if after, _ := g.SelectedRowID(); after != first {
		t.Fatalf("单选模式 Ctrl+Down 不得改变选择：%q → %q", first, after)
	}
}

func TestR1ShiftExtendsOnlyWhenMultiSelectEnabled(t *testing.T) {
	g, h := msSetup(t, 100)
	h.frame()
	kbFocus(h)
	kbKey(h, key.NameDownArrow) // row0，锚点 row0
	// 多选开启：Shift+Down 扩展 {0,1}。
	msKeyShift(h, key.NameDownArrow)
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(0), sortID(1)}) {
		t.Fatalf("多选 Shift+Down 应扩展：%v", got)
	}
	// 多选开启：Ctrl+Down 不消费（不在 Optional 集）→ 选择不变。
	h.frame(key.Event{Name: key.NameDownArrow, State: key.Press, Modifiers: key.ModCtrl})
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(0), sortID(1)}) {
		t.Fatalf("Ctrl+Down 不应改变多选：%v", got)
	}
	// 关闭多选后 Shift+Down 不再消费：单选 selID（最后锚行）不变。
	g.DisableMultiSelect()
	before, _ := g.SelectedRowID()
	msKeyShift(h, key.NameDownArrow)
	if after, _ := g.SelectedRowID(); after != before {
		t.Fatalf("关闭多选后 Shift+Down 不得改变选择：%q → %q", before, after)
	}
}

func TestR1BoundaryPlainNavCollapsesMultiSelect(t *testing.T) {
	g, h := msSetup(t, 8)
	h.frame()
	msCtrlClick(h, 50, msRowY(2))
	msCtrlClick(h, 50, msRowY(0)) // 锚点=ROW-00000（lead）
	if got := msIDs(t, g); len(got) != 2 {
		t.Fatalf("前置两行选择：%v", got)
	}
	g.Clicked() // 消费指针点击残留；此后键盘导航不得再产生 Clicked
	// 顶部无修饰 Up：收拢为当前锚行；scrollOff 不变。
	offBefore := g.ScrollOffset()
	kbKey(h, key.NameUpArrow)
	after := g.SelectedRowIDs()
	if len(after) != 1 || after[0] != sortID(0) {
		t.Fatalf("边界 Up 应收拢为锚行：%v", after)
	}
	if g.ScrollOffset() != offBefore {
		t.Fatalf("边界收拢不得重滚：%d → %d", offBefore, g.ScrollOffset())
	}
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("键盘导航不得伪造 Clicked")
	}
	// 尾部对称：连续 Down 到底（钳制处普通导航收拢为末行），Shift+Up
	// 扩展一段后再普通 Down 与边界 Down——收拢保持幂等。
	kbKey(h, key.NameDownArrow) // 0→1（收拢 {1}）
	for i := 0; i < 20; i++ {
		kbKey(h, key.NameDownArrow) // 到底并钳制
	}
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(7)}) {
		t.Fatalf("到底应收拢为末行：%v", got)
	}
	msKeyShift(h, key.NameUpArrow) // 锚点 7，扩展 {6,7}
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(6), sortID(7)}) {
		t.Fatalf("尾部扩展：%v", got)
	}
	kbKey(h, key.NameDownArrow) // 6→7：普通提交收拢 {7}
	kbKey(h, key.NameDownArrow) // 7→7：边界钳制收拢 {7}（幂等）
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(7)}) {
		t.Fatalf("底部边界收拢：%v", got)
	}
	if g.ScrollOffset() != 8*24-(200-24) {
		t.Fatalf("底部偏移应停在 maxOff：%d", g.ScrollOffset())
	}
}

func TestR1BoundarySingleSelectIsNoOp(t *testing.T) {
	// R28 原守卫：单选模式边界按键=无操作（选择停在首行、偏移不重滚、
	// 不伪造 Clicked）。
	g, h := sortSetup(t, 8)
	h.frame()
	kbFocus(h)
	h.click(50, msRowY(3))
	g.Clicked() // 消费点击残留
	h.frame()
	for i := 0; i < 5; i++ {
		kbKey(h, key.NameUpArrow) // 3→2→1→0→(0)→(0)
	}
	if after, _ := g.SelectedRowID(); after != sortID(0) {
		t.Fatalf("连续 Up 应停在首行：%q", after)
	}
	if off := g.ScrollOffset(); off != 0 {
		t.Fatalf("边界 Up 后偏移应稳定在 0：%d", off)
	}
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("键盘导航不得伪造 Clicked（单选）")
	}
}

func TestR1KeyboardNavPreservesHScrollOffset(t *testing.T) {
	// 跨轴：横向滚动后键盘导航（含边界收拢）不得改变横向偏移。
	g, h := comboSetup(t, 100)
	hsScrollX(h, 120)
	if off := g.HScrollOffset(); off != 120 {
		t.Fatalf("前置横滚：%d", off)
	}
	h.click(50, msRowY(1))
	h.frame()
	kbKey(h, key.NameDownArrow)
	if off := g.HScrollOffset(); off != 120 {
		t.Fatalf("Down 后横向偏移不得变化：%d", off)
	}
	// 顶部边界收拢同样不动横轴（两次 Up：0←1 后 0←0 命中钳制路径）。
	kbKey(h, key.NameHome)
	msKeyShift(h, key.NameDownArrow) // {0,1}
	kbKey(h, key.NameUpArrow)        // 1→0：普通提交收拢 {0}
	kbKey(h, key.NameUpArrow)        // 0→0：边界钳制收拢 {0}
	if off := g.HScrollOffset(); off != 120 {
		t.Fatalf("边界收拢后横向偏移不得变化：%d", off)
	}
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(0)}) {
		t.Fatalf("边界收拢结果：%v", got)
	}
}

func TestR1ShiftExtendInvalidAnchorDegrades(t *testing.T) {
	// 失效锚点（正例）：锚点行被 SetRows 删除后，Shift+Down 退化为单行。
	g, h := msSetup(t, 100)
	h.frame()
	kbFocus(h)
	kbKey(h, key.NameDownArrow) // row0 锚点
	msKeyShift(h, key.NameDownArrow)
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(0), sortID(1)}) {
		t.Fatalf("前置扩展：%v", got)
	}
	// 替换数据：ROW-00000（锚点）与 ROW-00001 均被删除；选择集清空。
	if rep := g.SetRows(sortTestRows(0)); !rep.OK {
		t.Fatal("clear failed")
	}
	if rep := g.SetRows(sortTestRows(50)); !rep.OK {
		t.Fatal("refill failed")
	}
	h.frame()
	if got := msIDs(t, g); len(got) != 0 {
		t.Fatalf("全替换后选择应清空：%v", got)
	}
	// 锚点失效 + 无选择：Shift+Down 从可见首行开始（退化单行）。
	msKeyShift(h, key.NameDownArrow)
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(0)}) {
		t.Fatalf("失效锚点 Shift+Down 应退化单行：%v", got)
	}
	// 反例：锚点仍在时不得退化。
	msKeyShift(h, key.NameDownArrow)
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(0), sortID(1)}) {
		t.Fatalf("有效锚点应扩展：%v", got)
	}
}

func TestR1ClickFocusesUnderModifiers(t *testing.T) {
	// Ctrl/Shift 点击同样请求焦点（Press 路径不区分修饰键），
	// Shift+点击（范围选择）后键盘立即可用。
	g, h := msSetup(t, 100)
	h.click(50, msRowY(2)) // 普通点击建立锚点 ROW-00002
	msShiftClick(h, 50, msRowY(4))
	h.frame()
	if !g.Focused() {
		t.Fatal("Shift+点击后应持焦")
	}
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(2), sortID(3), sortID(4)}) {
		t.Fatalf("Shift 范围：%v", got)
	}
	kbKey(h, key.NameDownArrow) // 普通导航收拢
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(5)}) {
		t.Fatalf("点击后普通导航收拢：%v", got)
	}
}
