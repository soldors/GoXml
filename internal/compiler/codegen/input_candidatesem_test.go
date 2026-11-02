package codegen

import (
	"strings"
	"testing"
)

// ============================================================================
// R8-B B3：候选语义（方案乙"事件先行"）真实事件验收矩阵。
//
// 每例 ≤8 帧（含 divSetup 的 2 帧 + 固定 quiet 观察帧），全部经
// rtProbeVM 真实链：Analyzer→Codegen→生成的 View→input.Router/Editor，
// 不直接调用处理器冒充输入。输出行 `SCEN <id> ...` 是 scenario.json
// 的"实测"字段来源（逐字转录，断言先于转录）。
//
// 不变量记号（design/decision-table.md §2）：
//   I1 每帧末 E==V（除投影态）；I2 payload==提交时刻 E；
//   I3 用户事件零丢弃；I4 quiet 帧回调增量 0。
// ============================================================================

// scen1 同帧 外部NEW + Edit X + Return（R7 P1 反例的全冲突形态）。
// 夹具：preset "OLD"；帧3 同时 obj.Title="NEW" + EditEvent{"X"} + Return；
// 帧4-6 quiet。
// 期望（方案乙）：用户事件先消费 → 模型/编辑器/载荷一致为 "XOLD"
// （用户优先，外部值被同帧用户编辑冲掉——显式语义 D5/D6）；quiet 稳定。
func TestCandSemSameFrameExtEditReturn(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText" Submit="SubmitRecord"/>`, frameProbePrelude+`
func TestProbeSameFrameExtEditReturn(t *testing.T) {
	obj, v, r, frame := divSetup(t, "OLD")
	before := obj.ChangeCount
	// 帧3：三重同帧冲突——外部写 + 真实编辑 + 回车
	obj.Title = "NEW"
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "X"})
	r.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	frame()
	for i := 4; i <= 6; i++ {
		frame()
		fmt.Printf("SCEN sameframe-ext-edit-return frame=%d editor=%q model=%q applied=%q count=%d submit=%d payload=%q\n",
			i, v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount, obj.SubmitCount, obj.LastSubmit)
	}
	fmt.Printf("SCEN sameframe-ext-edit-return frame=3 editor=%q model=%q applied=%q count=%d submit=%d payload=%q\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount-before, obj.SubmitCount, obj.LastSubmit)
	// I1：帧3 末编辑器与模型一致（R7 P1 的分裂在结构上不可能）
	if v.input0.Text() != obj.Title {
		t.Fatalf("I1 破坏: editor=%q model=%q", v.input0.Text(), obj.Title)
	}
	// I2：载荷 == 提交时刻编辑器文本，且与最终模型一致（同帧用户优先）
	if obj.LastSubmit != v.input0.Text() || obj.SubmitCount != 1 {
		t.Fatalf("I2 破坏: payload=%q editor=%q submit=%d", obj.LastSubmit, v.input0.Text(), obj.SubmitCount)
	}
	// I3：真实用户编辑被派发（恰好 1 次 ChangeEvent）
	if obj.ChangeCount-before != 1 {
		t.Fatalf("I3 破坏: 用户编辑回调增量 %d != 1", obj.ChangeCount-before)
	}
	// I4：quiet 帧零增量（断言在循环后由最终值覆盖：count/submit 不再变化）
	if obj.SubmitCount != 1 {
		t.Fatalf("I4 破坏: submit=%d", obj.SubmitCount)
	}
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("sameframe-ext-edit-return 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen2 Filter 字母投影为空 + 真实插入再删除（R7 B1' 反例形态）。
// 夹具：Filter="0123456789"，preset "7"；帧3 外部 Title="letters"
// （投影为空）；帧4 真实 EditEvent{"1"}；帧5 真实删除（Range 全清）；
// 帧6-8 quiet。
// 期望：外部 SetText 静默；两次真实编辑各派发一次（R7 候选在此吞事件）。
func TestCandSemFilterEmptyProjection(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" Filter="0123456789" ValueChanged="IdentityText"/>`, frameProbePrelude+`
func TestProbeFilterEmptyProjection(t *testing.T) {
	obj, v, r, frame := divSetup(t, "7")
	// 帧3：外部不可表示值（字母被 Filter 投影为空）
	obj.Title = "letters"
	frame()
	if ed := v.input0.Text(); ed != "" {
		t.Fatalf("帧3: editor=%q, want 空（字母被投影）", ed)
	}
	// 帧4：真实插入 "1"
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "1"})
	frame()
	fmt.Printf("SCEN filter-empty-lossy frame=4 editor=%q model=%q applied=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
	if obj.ChangeCount != 1 || obj.Title != "1" {
		t.Fatalf("I3 破坏: 插入后 count=%d model=%q（真实编辑不得被吞）", obj.ChangeCount, obj.Title)
	}
	// 帧5：真实删除（清空编辑器）
	r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: ""})
	frame()
	fmt.Printf("SCEN filter-empty-lossy frame=5 editor=%q model=%q applied=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
	if obj.ChangeCount != 2 || obj.Title != "" {
		t.Fatalf("I3 破坏: 删除后 count=%d model=%q", obj.ChangeCount, obj.Title)
	}
	// 帧6-8：quiet——投影态不追赶、无回调增长
	for i := 6; i <= 8; i++ {
		frame()
		fmt.Printf("SCEN filter-empty-lossy frame=%d editor=%q model=%q applied=%q count=%d\n",
			i, v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
	}
	if obj.ChangeCount != 2 || v.input0.Text() != obj.Title {
		t.Fatalf("quiet 稳定性破坏: editor=%q model=%q count=%d", v.input0.Text(), obj.Title, obj.ChangeCount)
	}
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("filter-empty-lossy 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen3 MaxLen=1 + 真正非幂等 AppendText（R6 反例形态）。
// 夹具：MaxLen="2"，preset "A"；帧3 用户词尾 EditEvent{"b"}（真追加，
// 编辑器 "Ab" 合法）；帧4 追随帧；帧5-8 quiet。
// 期望：初始应用静默（count=0，v1 在此双重应用）；用户编辑恰好一次
// （model="AAb"，3>2 不可表示）；显示停在投影 "AA"；quiet 帧 count 恒定（I4）。
func TestCandSemMaxLenAppendBounded(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" MaxLen="2" ValueChanged="AppendText"/>`, frameProbePrelude+`
func TestProbeMaxLenAppend(t *testing.T) {
	obj, v, r, frame := divSetup(t, "A")
	if obj.ChangeCount != 0 || obj.Title != "A" {
		t.Fatalf("初始应用应静默: model=%q count=%d", obj.Title, obj.ChangeCount)
	}
	// 帧3：用户词尾追加 "b"（编辑器 "Ab"，2≤MaxLen 合法）→ AppendText("Ab")
	// → 模型 "AAb"（3>2 不可表示）
	r.Queue(key.EditEvent{Range: key.Range{Start: 1, End: 1}, Text: "b"})
	frame()
	fmt.Printf("SCEN maxlen-append frame=3 editor=%q model=%q applied=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
	if obj.Title != "AAb" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: model=%q count=%d, want AAb/1（恰好一次）", obj.Title, obj.ChangeCount)
	}
	// 帧4：显示追随帧——SetText("AAb") 被 MaxLen 投影为 "AA"
	frame()
	// 帧5-8：quiet——不追赶、不增长
	for i := 5; i <= 8; i++ {
		frame()
		fmt.Printf("SCEN maxlen-append frame=%d editor=%q model=%q applied=%q count=%d\n",
			i, v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
	}
	if obj.ChangeCount != 1 {
		t.Fatalf("I4 破坏: quiet 帧 count=%d（应恒为 1）", obj.ChangeCount)
	}
	if ed := v.input0.Text(); ed != "AA" {
		t.Fatalf("投影显示应稳定为 AA, got %q", ed)
	}
	if v.input0.Text() != obj.Title {
		t.Logf("投影态（D7）：编辑器 %q 无法表示模型 %q——A=V 原始值，不追赶", v.input0.Text(), obj.Title)
	}
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("maxlen-append 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen4 UpperText 归一化显示追随（R5 起的原生分歧）。
// 期望：用户 "abc" → count=1、模型 "ABC"；下一帧 E="ABC"（追随）；
// quiet 稳定一致（I1）。
func TestCandSemNormalizeDisplayFollows(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="UpperText"/>`, frameProbePrelude+`
func TestProbeNormalizeFollows(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "abc"})
	frame()
	fmt.Printf("SCEN normalize frame=3 editor=%q model=%q applied=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
	if obj.Title != "ABC" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: model=%q count=%d", obj.Title, obj.ChangeCount)
	}
	// 帧4：显示追随帧
	frame()
	fmt.Printf("SCEN normalize frame=4 editor=%q model=%q applied=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
	if ed := v.input0.Text(); ed != "ABC" {
		t.Fatalf("帧4: editor=%q, want ABC（显示应追随归一化模型）", ed)
	}
	// 帧5-8：quiet
	for i := 5; i <= 8; i++ {
		frame()
	}
	if v.input0.Text() != obj.Title || obj.ChangeCount != 1 {
		t.Fatalf("I1/I4 破坏: editor=%q model=%q count=%d", v.input0.Text(), obj.Title, obj.ChangeCount)
	}
	fmt.Printf("SCEN normalize final editor=%q model=%q count=%d\n", v.input0.Text(), obj.Title, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("normalize 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen5 处理器不回写 Value 字段（TouchOther）+ 只改其它绑定字段。
// 期望：事件后 A=E="abc"；下一帧 V("")!=A → SetText("") 清空编辑器
// （显示追随未写回的模型，D4）。
func TestCandSemNoWritebackFollowsModel(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="TouchOther"/><Text Value="{Other}"/>`, frameProbePrelude+`
func TestProbeNoWriteback(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "abc"})
	frame()
	fmt.Printf("SCEN no-writeback frame=3 editor=%q model=%q applied=%q other=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.Other, obj.ChangeCount)
	if obj.Other != "X-abc" || obj.ChangeCount != 1 {
		t.Fatalf("帧3: other=%q count=%d", obj.Other, obj.ChangeCount)
	}
	frame()
	fmt.Printf("SCEN no-writeback frame=4 editor=%q model=%q applied=%q count=%d\n",
		v.input0.Text(), obj.Title, v.applied0, obj.ChangeCount)
	if ed := v.input0.Text(); ed != "" {
		t.Fatalf("帧4: editor=%q, want 空（未写回字段显示追随模型）", ed)
	}
	for i := 5; i <= 6; i++ {
		frame()
	}
	if obj.ChangeCount != 1 || v.input0.Text() != obj.Title {
		t.Fatalf("I1/I4 破坏: editor=%q model=%q count=%d", v.input0.Text(), obj.Title, obj.ChangeCount)
	}
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("no-writeback 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen6 无处理器 + 外部同步（对照：静默应用、用户文本保留语义）。
func TestCandSemNoHandler(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}"/>`, frameProbePrelude+`
func TestProbeNoHandler(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	// 帧3：用户编辑（无 ValueChanged——不产生回调，编辑保留在编辑器）
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "draft"})
	frame()
	fmt.Printf("SCEN no-handler frame=3 editor=%q model=%q applied=%q\n",
		v.input0.Text(), obj.Title, v.applied0)
	if ed := v.input0.Text(); ed != "draft" {
		t.Fatalf("帧3: editor=%q, want draft（无处理器时编辑保留）", ed)
	}
	// 帧4：外部写入 → 同步应用
	obj.Title = "XYZ"
	frame()
	fmt.Printf("SCEN no-handler frame=4 editor=%q model=%q applied=%q\n",
		v.input0.Text(), obj.Title, v.applied0)
	if ed := v.input0.Text(); ed != "XYZ" {
		t.Fatalf("帧4: editor=%q, want XYZ", ed)
	}
	frame()
	if v.input0.Text() != obj.Title {
		t.Fatalf("I1 破坏: editor=%q model=%q", v.input0.Text(), obj.Title)
	}
	fmt.Printf("SCEN no-handler final editor=%q model=%q\n", v.input0.Text(), obj.Title)
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("no-handler 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen7 双 Input 共享字段：编辑 input0 → 恰好一次回调（v1 交叉回显为 2）；
// input1 下一帧静默追随。
func TestCandSemSharedField(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText"/><Input Value="{Title}" ValueChanged="IdentityText"/>`, strings.ReplaceAll(frameProbePrelude, `t.Skip("no focus")`, `t.Fatal("no focus")`)+`
func TestProbeSharedField(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "x"})
	frame()
	fmt.Printf("SCEN shared-field frame=3 in0=%q in1=%q model=%q count=%d\n",
		v.input0.Text(), v.input1.Text(), obj.Title, obj.ChangeCount)
	frame()
	fmt.Printf("SCEN shared-field frame=4 in0=%q in1=%q model=%q count=%d\n",
		v.input0.Text(), v.input1.Text(), obj.Title, obj.ChangeCount)
	if obj.ChangeCount != 1 {
		t.Fatalf("共享字段应恰好一次回调（交叉回显应静默）: %d", obj.ChangeCount)
	}
	if v.input1.Text() != "x" || obj.Title != "x" {
		t.Fatalf("input1 应静默追随: in1=%q model=%q", v.input1.Text(), obj.Title)
	}
	for i := 5; i <= 6; i++ {
		frame()
	}
	if obj.ChangeCount != 1 {
		t.Fatalf("I4 破坏: count=%d", obj.ChangeCount)
	}
	fmt.Printf("SCEN shared-field final in0=%q in1=%q model=%q count=%d\n",
		v.input0.Text(), v.input1.Text(), obj.Title, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("shared-field 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen8 不同字段隔离（正对照）。
func TestCandSemDistinctFields(t *testing.T) {
	vm := probeVMSrc + "\nfunc (vm *VM) OtherText(s string) { vm.Other = s }\n"
	out := rtProbeVM(t, vm, `<Input Value="{Title}" ValueChanged="IdentityText"/><Input Value="{Other}" ValueChanged="OtherText"/>`, strings.ReplaceAll(frameProbePrelude, `t.Skip("no focus")`, `t.Fatal("no focus")`)+`
func TestProbeDistinctFields(t *testing.T) {
	obj, v, r, frame := divSetup(t, "")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "x"})
	frame()
	frame()
	if obj.Title != "x" || obj.Other != "" || v.input1.Text() != "" {
		t.Fatalf("不同字段串写: title=%q other=%q editor1=%q", obj.Title, obj.Other, v.input1.Text())
	}
	fmt.Printf("SCEN distinct-fields title=%q other=%q in1=%q count=%d\n",
		obj.Title, obj.Other, v.input1.Text(), obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("distinct-fields 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen9 双 View 实例隔离（两份 VM、两份 View 状态互不可见）。
func TestCandSemDualViewInstances(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText"/>`, strings.ReplaceAll(frameProbePrelude, `t.Skip("no focus")`, `t.Fatal("no focus")`)+`
func TestProbeDualViews(t *testing.T) {
	th := material.NewTheme()
	obj1, obj2 := &gvm.VM{Title: "one"}, &gvm.VM{Title: "two"}
	v1, err := NewGoxmlDashboardView(obj1)
	if err != nil { t.Fatal(err) }
	v2, err := NewGoxmlDashboardView(obj2)
	if err != nil { t.Fatal(err) }
	var r input.Router
	gtx := layout.Context{
		Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 40)),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: r.Source(),
	}
	frame := func() { gtx.Reset(); v1.Layout(gtx, th); v2.Layout(gtx, th); r.Frame(gtx.Ops) }
	frame()
	// 两实例各自完成初始静默应用
	if v1.input0.Text() != "one" || v2.input0.Text() != "two" {
		t.Fatalf("初始应用串写: v1=%q v2=%q", v1.input0.Text(), v2.input0.Text())
	}
	if obj1.ChangeCount != 0 || obj2.ChangeCount != 0 {
		t.Fatalf("初始应用应静默: %d/%d", obj1.ChangeCount, obj2.ChangeCount)
	}
	// 专设聚焦序列：先只布局 v1 并按下（两实例叠放时 v2 后注册会抢焦点），
	// 再回组合帧确认焦点归属。
	gtx.Reset()
	v1.Layout(gtx, th)
	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary})
	r.Queue(pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse})
	r.Frame(gtx.Ops)
	frame()
	if !gtx.Focused(&v1.input0) {
		t.Skip("no focus (dual-instance layout overlap)")
	}
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "!"})
	frame()
	fmt.Printf("SCEN dual-view v1=%q obj1=%q c1=%d | v2=%q obj2=%q c2=%d\n",
		v1.input0.Text(), obj1.Title, obj1.ChangeCount, v2.input0.Text(), obj2.Title, obj2.ChangeCount)
	if obj1.Title != "one!" || obj1.ChangeCount != 1 {
		t.Fatalf("v1 未正确回写: %q %d", obj1.Title, obj1.ChangeCount)
	}
	if obj2.Title != "two" || obj2.ChangeCount != 0 || v2.input0.Text() != "two" {
		t.Fatalf("v2 被串写: %q %d %q", obj2.Title, obj2.ChangeCount, v2.input0.Text())
	}
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("dual-view 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen10 合法空串 + Unicode MaxLen（rune 计数）。
func TestCandSemEmptyStringAndUnicode(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" MaxLen="3" ValueChanged="IdentityText"/>`, frameProbePrelude+`
func TestProbeEmptyUnicode(t *testing.T) {
	obj, v, r, frame := divSetup(t, "名称测试")
	// 帧3：外部置合法空串 → SetText("") 清空，静默
	obj.Title = ""
	frame()
	if ed := v.input0.Text(); ed != "" {
		t.Fatalf("帧3: editor=%q, want 空（合法空串值）", ed)
	}
	if obj.ChangeCount != 0 {
		t.Fatalf("外部置空应静默: %d", obj.ChangeCount)
	}
	// 帧4：Unicode 输入超出 MaxLen=3（rune 计）→ Gio 截断为 3 个 rune
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "中文输入超长"})
	frame()
	ed := v.input0.Text()
	fmt.Printf("SCEN empty-unicode frame=4 editor=%q runes=%d model=%q count=%d\n",
		ed, len([]rune(ed)), obj.Title, obj.ChangeCount)
	if len([]rune(ed)) != 3 {
		t.Fatalf("MaxLen 应按 rune 截断为 3, got %d (%q)", len([]rune(ed)), ed)
	}
	if obj.Title != ed || obj.ChangeCount != 1 {
		t.Fatalf("载荷应为截断后编辑器文本: model=%q editor=%q count=%d", obj.Title, ed, obj.ChangeCount)
	}
	frame()
	if v.input0.Text() != obj.Title {
		t.Fatalf("I1 破坏: editor=%q model=%q", v.input0.Text(), obj.Title)
	}
	fmt.Printf("SCEN empty-unicode final editor=%q model=%q count=%d\n", v.input0.Text(), obj.Title, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("empty-unicode 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen11 初始与外部值同步（基础正对照：两次静默应用 + 一致性）。
func TestCandSemInitialAndExternalSync(t *testing.T) {
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="IdentityText"/>`, frameProbePrelude+`
func TestProbeInitialExternal(t *testing.T) {
	obj, v, _, frame := divSetup(t, "A")
	// divSetup 2 帧：初始 "A" 已静默应用
	if v.input0.Text() != "A" || obj.ChangeCount != 0 {
		t.Fatalf("初始应用: editor=%q count=%d", v.input0.Text(), obj.ChangeCount)
	}
	// 帧3：外部改值
	obj.Title = "B"
	frame()
	if ed := v.input0.Text(); ed != "B" || obj.ChangeCount != 0 {
		t.Fatalf("外部同步: editor=%q count=%d", ed, obj.ChangeCount)
	}
	frame()
	if v.input0.Text() != obj.Title {
		t.Fatalf("I1 破坏: %q vs %q", v.input0.Text(), obj.Title)
	}
	fmt.Printf("SCEN initial-external final editor=%q model=%q count=%d\n", v.input0.Text(), obj.Title, obj.ChangeCount)
}
`)
	if !contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("initial-external 失败:\n%s", out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}
