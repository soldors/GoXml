package codegen

// R15-B FOR-IDENTIFIER-HYGIENE：For 循环变量（Each/Index）与生成代码
// 标识符的名称卫生。
//
// 反例（基线 90f62dc 真实 CLI 复现）：`<For Each="append" ...>` 生成成功，
// 但 go build 失败——For 内联展开在**同一作用域**生成
// `children = append(children, sizeWrap(...))`，循环变量遮蔽内置 append：
//
//	invalid operation: cannot call append (variable of struct type Row)
//
// 既有机制：checkLoopVarName 已拒绝 {v,th,gtx,items,children,rec,k,ops}
// （契约 §6.1：循环变量不得与生成代码使用的标识符冲突）；R14 的
// reserveLoopNames 让 import 别名让位 Each/Index。本轮补上该作用域内
// 生成代码实际调用的内置 append。
//
// 判据边界（契约 §6.1，非内置函数一刀切）：len/make/new 在该作用域内
// 未被生成代码使用 → 合法输入，生成与构建正常。

import (
	"strings"
	"testing"
)

// forGen 以 For 片段跑 verified 链（Parser→Expander→Analyzer）+ 生成，
// 返回 (源码, 生成错误)。循环变量保留名检查在 Codegen，Analyzer 应通过。
func forGen(t *testing.T, gxmlBody string) (string, error) {
	t.Helper()
	v := verified(t, gxmlBody)
	out, err := Generate(v, bindingOpts())
	return string(out), err
}

// TestForBuiltinAppendRejected 缺口判据：Each/Index 取 append 时必须被
// codegen 显式拒绝（既有保留名诊断同形态），不得生成必编译失败的代码。
func TestForBuiltinAppendRejected(t *testing.T) {
	for _, tc := range []struct{ name, attr, each, arg string }{
		{"Each", `Each="append"`, "append", "append.ID"},
		{"Index", `Each="row" Index="append"`, "row", "append"},
	} {
		_, err := forGen(t, `<For `+tc.attr+` In="{Items}" Key="{`+tc.each+`.ID}"><Button Text="{`+tc.each+`.Name}" Click="RecordNum(`+tc.arg+`)"/></For>`)
		if err == nil {
			t.Fatalf("%s=append 应被拒绝（R15-B 反例：循环体 append 调用被遮蔽）", tc.name)
		}
		if !strings.Contains(err.Error(), "collides with an identifier used by generated code") {
			t.Fatalf("%s=append 的诊断应与既有保留名同形态，实际: %v", tc.name, err)
		}
	}
}

// TestForRowIdentifiersBehaviorEachLen 行为级验收（真实链路 Analyzer→Codegen→
// go test→离屏 Router）：合法内置名 Each="len" + Index="i" 的页面——普通渲染、
// For 内 If 分支、行事件实参、Key 重排与删除——全部与 Each="item" 基准一致。
func TestForRowIdentifiersBehaviorEachLen(t *testing.T) {
	vmSrc := `package vm

// VM 是 R15-B 行为验收用 ViewModel。
type VM struct {
	Rows       []Row
	LastTapped string
	ShowDetail bool
}

// Row 是 <For> 行数据。
type Row struct {
	ID   string
	Name string
}

// TapRow 是行事件处理器（实参 row.ID）。
func (vm *VM) TapRow(id string) { vm.LastTapped = id }
`
	gxml := `<For Each="len" Index="i" In="{Rows}" Key="{len.ID}">
  <If Condition="{ShowDetail}">
    <Then><Button Text="{len.Name}" Click="TapRow(len.ID)"/></Then>
    <Else><Button Text="{len.ID}" Click="TapRow(len.ID)"/></Else>
  </If>
</For>`
	probe := `
func TestProbeForRowHygiene(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{Rows: []gvm.Row{
		{ID: "A", Name: "行A"},
		{ID: "B", Name: "行B"},
		{ID: "C", Name: "行C"},
	}}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Constraints{Max: image.Point{X: 300, Y: 100000}},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	lay := func() int {
		gtx.Reset()
		d := v.Layout(gtx, th)
		r.Frame(gtx.Ops)
		return d.Size.Y
	}
	h3 := lay()
	obj.Rows = obj.Rows[:2]
	h2 := lay()
	rowH := h3 - h2
	if rowH <= 0 {
		t.Fatalf("行高推导失败: h3=%d h2=%d", h3, h2)
	}
	obj.Rows = []gvm.Row{{ID: "A", Name: "行A"}, {ID: "B", Name: "行B"}, {ID: "C", Name: "行C"}}
	y0 := h3 - 3*rowH
	lay()
	click := func(y float32) {
		obj.LastTapped = ""
		r.Queue(
			pointer.Event{Position: f32.Pt(30, y), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(30, y), Kind: pointer.Release, Source: pointer.Mouse},
		)
		lay()
	}
	yOf := func(i int) float32 { return float32(y0 + i*rowH + rowH/2) }

	click(yOf(1))
	if obj.LastTapped != "B" {
		t.Fatalf("点第2行: LastTapped=%q, want B", obj.LastTapped)
	}
	// Key 重排 [C,A,B]：第 1 行位置现在是 C
	obj.Rows = []gvm.Row{{ID: "C", Name: "行C"}, {ID: "A", Name: "行A"}, {ID: "B", Name: "行B"}}
	lay()
	click(yOf(0))
	if obj.LastTapped != "C" {
		t.Fatalf("重排后点第1行: LastTapped=%q, want C", obj.LastTapped)
	}
	// 删除头部 C：原第 1 行位置现在是 A；被删行状态不得复活
	obj.Rows = []gvm.Row{{ID: "A", Name: "行A"}, {ID: "B", Name: "行B"}}
	lay()
	click(yOf(0))
	if obj.LastTapped != "A" {
		t.Fatalf("删除后点第1行: LastTapped=%q, want A", obj.LastTapped)
	}
	// For 内 If 分支切换：分支按钮同键同名——事件仍按 Key 归属
	obj.ShowDetail = true
	lay()
	click(yOf(0))
	if obj.LastTapped != "A" {
		t.Fatalf("If 分支切换后点第1行: LastTapped=%q, want A", obj.LastTapped)
	}
	fmt.Println("SCEN-DONE for-hygiene-eachlen")
}
`
	out := rtProbeVM(t, vmSrc, gxml, probe)
	assertProbeHealth(t, "for-hygiene-eachlen", out)
}

// TestForExistingReservedNamesCompatible 既有保留名诊断保持兼容：
// children/v/items 的拒绝形态与消息不变。
func TestForExistingReservedNamesCompatible(t *testing.T) {
	for _, name := range []string{"children", "v", "items", "gtx"} {
		_, err := forGen(t, `<For Each="`+name+`" In="{Items}" Key="{`+name+`.ID}"><Button Text="选" Click="RecordNum(`+name+`.ID)"/></For>`)
		if err == nil || !strings.Contains(err.Error(), "collides with an identifier used by generated code") {
			t.Fatalf("既有保留名 %q 应保持原拒绝形态，实际 err=%v", name, err)
		}
	}
}

// TestForLegalBuiltinLikeNamesGenerate 合法相近名对照：
// len/make/new 在该作用域内未被生成代码使用 → 合法；appendx/lenn 相近名合法。
// 同一页面覆盖普通渲染、If 分支、行事件实参三种发射路径。
func TestForLegalBuiltinLikeNamesGenerate(t *testing.T) {
	for _, each := range []string{"len", "make", "new", "appendx", "lenn"} {
		out, err := forGen(t, `<For Each="`+each+`" In="{Items}" Key="{`+each+`.ID}">
  <If Condition="{Dirty}">
    <Then><Button Text="{`+each+`.Name}" Click="RecordNum(`+each+`.ID)"/></Then>
  </If>
</For>`)
		if err != nil {
			t.Fatalf("Each=%q 是合法输入，生成不应失败: %v", each, err)
		}
		for _, want := range []string{"for _, " + each + " := range items", each + ".Name", each + ".ID"} {
			if !strings.Contains(out, want) {
				t.Fatalf("Each=%q 生成缺 %q:\n%s", each, want, out[:600])
			}
		}
	}
}
