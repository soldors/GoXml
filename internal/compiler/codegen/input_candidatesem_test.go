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
// 不变量记号（与 Input输出契约 §5.4 采纳版对齐；原 I1/I2 表述已由
// zcode-decision-r1/corrected-invariants.md 修正，此处为可证形态）：
//   I1r 逐控件收敛：受控 Input 在其渲染位置完成本帧事件消费与同步后的
//       **下一帧**，E 收敛到该帧它所见的 V；不保证所有 Editor 每帧末
//       相等。例外（均不构成永久分裂，逐例有探针）：
//       a) 投影态——MaxLen/Filter/SingleLine 不可表示模型（A=V 不追赶）；
//       b) 无处理器非受控态——无 ValueChanged 时用户未回写 draft 保留
//          （契约 §5.4"不属于投影例外"）；
//       c) 共享字段同帧先后渲染——后渲染控件改写模型，先渲染控件下一帧
//          收敛。
//   I2r Submit 载荷=事件产生时编辑器原文；帧末归一化模型可以不同
//       （NORMALIZE-RETURN：payload=abc vs E/V=ABC）。
//   I3 用户事件零丢弃；I4 **quiet 帧为有界观察**（每例 3-5 帧）回调增量
//       0 且无 SetText——只断言该窗口内无抖动，不声称永久稳定。
//
// R9-B T1（探针守卫）：所有 wrapper 经 assertProbeHealth 统一出口，拒绝
// 内部 SKIP / 零测试 / 缺完成标记 / 子测试失败——dual-view 死 Skip
// （277e6fd..4fe52f8，编辑阶段被内层 Skip 掩盖、外层 PASS 蒙混）的
// 结构性堵截；守卫矩阵与注入变异红见文件尾两个 Test。
// ============================================================================

// probeHealthIssues 枚举一条探针输出的健康问题（全部 wrapper 共用守卫）。
// 判据（任一存在即红，缺一不可）：
//  1. 子 go test 必须成功（输出含 [err=nil]）；
//  2. 无 "--- FAIL"；
//  3. 无 "--- SKIP"——内部 Skip 不得吞断言（R8 dual-view 死 Skip 教训）；
//  4. 无 "no tests to run"——零测试不得假绿；
//  5. 必须出现该场景最后断言后的整行 "SCEN-DONE <id>"。
//     早期 SCEN 只是观测，不证明场景完成；错误id后缀也不能当完成。
func probeHealthIssues(scenarioID, out string) []string {
	var issues []string
	if !strings.Contains(out, "[err=nil]") {
		issues = append(issues, "子 go test 未成功（缺 [err=nil]）")
	}
	if strings.Contains(out, "--- FAIL") {
		issues = append(issues, "子测试 FAIL")
	}
	if strings.Contains(out, "--- SKIP") {
		issues = append(issues, "内部 SKIP（断言未执行——dual-view 死 Skip 模式）")
	}
	if strings.Contains(out, "no tests to run") {
		issues = append(issues, "零测试（go test 未匹配到任何用例）")
	}
	completed := false
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "SCEN-DONE "+scenarioID {
			completed = true
			break
		}
	}
	if !completed {
		issues = append(issues, "缺完成标记 SCEN-DONE "+scenarioID+"（场景未执行到最后断言）")
	}
	return issues
}

// assertProbeHealth 是每个探针 wrapper 的统一出口：健康问题任一存在即 Fatal。
func assertProbeHealth(t *testing.T, scenarioID, out string) {
	t.Helper()
	if issues := probeHealthIssues(scenarioID, out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（%s）:\n%s\n--- 原始输出 ---\n%s",
			scenarioID, strings.Join(issues, "; "), out)
	}
}

// frameProbePreludeGtx 与 input_divergence_test.go 的 frameProbePrelude 同体，
// 但额外返回 *layout.Context——供 T8/合成 key.FocusCmd 等需要向当前帧
// 注入命令的场景使用（不改动共享 prelude 所在文件）。
const frameProbePreludeGtx = `
// divSetupGtx 装配探测环境并聚焦唯一 Input（额外返回 gtx 供合成聚焦）。
func divSetupGtx(t *testing.T, title string) (*gvm.VM, *GoxmlDashboardView, *input.Router, *layout.Context, func()) {
	th := material.NewTheme()
	obj := &gvm.VM{Title: title}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil { t.Fatal(err) }
	var r input.Router
	gtx := layout.Context{
		Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 40)),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: r.Source(),
	}
	frame := func() { gtx.Reset(); v.Layout(gtx, th); r.Frame(gtx.Ops) }
	frame()
	r.Queue(pointer.Event{Position: f32.Pt(10,10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary})
	r.Queue(pointer.Event{Position: f32.Pt(10,10), Kind: pointer.Release, Source: pointer.Mouse})
	frame()
	if !gtx.Focused(&v.input0) { t.Fatal("no focus") }
	return obj, v, &r, &gtx, frame
}
`

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
	// I2r：载荷=确认事件产生时的编辑器原文。本例 IdentityText 恒等夹具下
	// 恰与最终模型一致；归一化分离对照（Upper+Return 同帧：payload=abc 而
	// 帧末 E/V=ABC）见 R9-A 独立复核 NORMALIZE-RETURN 实测转录——不可简写
	// 回"三者总相等"。
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
	fmt.Println("SCEN-DONE sameframe-ext-edit-return")
}
`)
	if issues := probeHealthIssues("sameframe-ext-edit-return", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（sameframe-ext-edit-return）: %v -- %s", issues, out)
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
	fmt.Println("SCEN-DONE filter-empty-lossy")
}
`)
	if issues := probeHealthIssues("filter-empty-lossy", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（filter-empty-lossy）: %v -- %s", issues, out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen3 MaxLen=2 + 真正非幂等 AppendText（R6 反例形态）。
// 夹具：MaxLen="2"，preset "A"；帧3 用户词尾 EditEvent{"b"}（真追加，
// 编辑器 "Ab" 合法）；帧4 追随帧；帧5-8 quiet。
// 为何 MaxLen=1 不构成真追加（历史教训，勿改回 1）：Gio Editor.replace
// 的截断判据 el-replaceSize+sc >= MaxLen 使词尾插入（replaceSize=0）在
// 满员编辑器上被整体吞掉 → 无 ChangeEvent、无回调——那是"零追加"，不
// 触发 R6 的回显放大路径。MaxLen=2 才能真追加出 3 字符模型（不可表示）。
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
	fmt.Println("SCEN-DONE maxlen-append")
}
`)
	if issues := probeHealthIssues("maxlen-append", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（maxlen-append）: %v -- %s", issues, out)
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
	fmt.Println("SCEN-DONE normalize")
}
`)
	if issues := probeHealthIssues("normalize", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（normalize）: %v -- %s", issues, out)
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
	fmt.Println("SCEN-DONE no-writeback")
}
`)
	if issues := probeHealthIssues("no-writeback", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（no-writeback）: %v -- %s", issues, out)
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
	fmt.Println("SCEN-DONE no-handler")
}
`)
	if issues := probeHealthIssues("no-handler", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（no-handler）: %v -- %s", issues, out)
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
	fmt.Println("SCEN-DONE shared-field")
}
`)
	if issues := probeHealthIssues("shared-field", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（shared-field）: %v -- %s", issues, out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// scen8 不同字段隔离（正对照）。
func TestCandSemDistinctFields(t *testing.T) {
	// T8：OtherText 带独立计数器（OtherCount 包级变量，与 Title 侧
	// ChangeCount 完全隔离）——静默不再靠"回写同值文本"推断。
	vm := probeVMSrc + "\n// T8 独立计数器：OtherText 触发次数。\nvar OtherCount int\n\nfunc (vm *VM) OtherText(s string) { OtherCount++; vm.Other = s }\n"
	out := rtProbeVM(t, vm, `<Input Value="{Title}" ValueChanged="IdentityText"/><Input Value="{Other}" ValueChanged="OtherText"/>`, strings.ReplaceAll(frameProbePreludeGtx, `t.Skip("no focus")`, `t.Fatal("no focus")`)+`
func TestProbeDistinctFields(t *testing.T) {
	obj, v, r, gtx, frame := divSetupGtx(t, "")
	// 帧3：用户编辑 input0 → 只影响 Title；input1（绑定 Other）不动。
	// 隔离断言即夹具自证的一半：两个绑定互不串写。
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "x"})
	frame()
	frame()
	if obj.Title != "x" || obj.Other != "" || v.input1.Text() != "" {
		t.Fatalf("不同字段串写: title=%q other=%q editor1=%q", obj.Title, obj.Other, v.input1.Text())
	}
	fmt.Printf("SCEN distinct-fields frame=4 title=%q other=%q in0=%q in1=%q count=%d ocount=%d\n",
		obj.Title, obj.Other, v.input0.Text(), v.input1.Text(), obj.ChangeCount, gvm.OtherCount)
	// 帧5：夹具自证——外部写 Other，input1 **独占**追随；且业务回调增量 0
	//（OtherCount 不变——程序化写静默，不靠"回写同值"推断）。
	// 若 input1 实际绑的是 {Title}（夹具漂移），这里必然暴露。
	obj.Other = "O"
	frame()
	fmt.Printf("SCEN distinct-fields frame=5 title=%q other=%q in0=%q in1=%q count=%d ocount=%d\n",
		obj.Title, obj.Other, v.input0.Text(), v.input1.Text(), obj.ChangeCount, gvm.OtherCount)
	if v.input1.Text() != "O" {
		t.Fatalf("夹具自证失败: input1 未绑定 {Other}（外部写 Other 后 in1=%q）", v.input1.Text())
	}
	if gvm.OtherCount != 0 {
		t.Fatalf("T8: 外部写 Other 不得触发 OtherText（回显静默）: ocount=%d", gvm.OtherCount)
	}
	if obj.Title != "x" || v.input0.Text() != "x" {
		t.Fatalf("Other 写入串写到 Title: title=%q in0=%q", obj.Title, v.input0.Text())
	}
	// 帧6聚焦、帧7真实编辑 input1——合成 key.FocusCmd 聚焦第二个编辑器（确定性
	// 离屏焦点），词尾追加 "Z"（显式 Range，SetText 后光标在 0）→
	// OtherText("OZ") 恰好 +1。
	gtx.Execute(key.FocusCmd{Tag: &v.input1})
	frame()
	r.Queue(key.EditEvent{Range: key.Range{Start: 1, End: 1}, Text: "Z"})
	frame()
	fmt.Printf("SCEN distinct-fields frame=7 title=%q other=%q in0=%q in1=%q count=%d ocount=%d\n",
		obj.Title, obj.Other, v.input0.Text(), v.input1.Text(), obj.ChangeCount, gvm.OtherCount)
	if gvm.OtherCount != 1 || obj.Other != "OZ" {
		t.Fatalf("T8: 真实编辑该字段应恰 +1: ocount=%d other=%q", gvm.OtherCount, obj.Other)
	}
	// 帧8：quiet（有界观察，I4）——两侧计数及文本均保持。
	frame()
	fmt.Printf("SCEN distinct-fields frame=8 title=%q other=%q in0=%q in1=%q count=%d ocount=%d\n",
		obj.Title, obj.Other, v.input0.Text(), v.input1.Text(), obj.ChangeCount, gvm.OtherCount)
	if obj.ChangeCount != 1 || obj.Title != "x" || v.input0.Text() != "x" {
		t.Fatalf("I1r/隔离破坏: count=%d title=%q in0=%q", obj.ChangeCount, obj.Title, v.input0.Text())
	}
	if gvm.OtherCount != 1 || obj.Other != "OZ" || v.input1.Text() != "OZ" {
		t.Fatalf("I4/Other quiet变化: count=%d model=%q editor=%q", gvm.OtherCount, obj.Other, v.input1.Text())
	}
	fmt.Println("SCEN-DONE distinct-fields")
}
`)
	if issues := probeHealthIssues("distinct-fields", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（distinct-fields）: %v -- %s", issues, out)
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
	// 帧1：两实例各自完成初始静默应用
	fmt.Printf("SCEN dual-view frame=1 v1=%q v2=%q c1=%d c2=%d\n",
		v1.input0.Text(), v2.input0.Text(), obj1.ChangeCount, obj2.ChangeCount)
	if v1.input0.Text() != "one" || v2.input0.Text() != "two" {
		t.Fatalf("初始应用串写: v1=%q v2=%q", v1.input0.Text(), v2.input0.Text())
	}
	if obj1.ChangeCount != 0 || obj2.ChangeCount != 0 {
		t.Fatalf("初始应用应静默: %d/%d", obj1.ChangeCount, obj2.ChangeCount)
	}
	// 聚焦序列：两实例叠放布局时 pointer 命中不可靠（277e6fd 起该探针的
	// 编辑阶段实际一直在 Skip——本次逐帧记录暴露）。改用合成 key.FocusCmd
	// （公开 API，与 EditEvent 同级的合成输入）确定性聚焦 v1 的编辑器。
	gtx.Execute(key.FocusCmd{Tag: &v1.input0})
	frame()
	fmt.Printf("SCEN dual-view frame=2 focusedV1=%v v1=%q v2=%q c1=%d c2=%d\n",
		gtx.Focused(&v1.input0), v1.input0.Text(), v2.input0.Text(), obj1.ChangeCount, obj2.ChangeCount)
	if !gtx.Focused(&v1.input0) {
		t.Fatal("合成 FocusCmd 后仍未获得焦点（环境/契约变化，须复核）")
	}
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "!"})
	frame()
	fmt.Printf("SCEN dual-view frame=3 v1=%q obj1=%q c1=%d | v2=%q obj2=%q c2=%d\n",
		v1.input0.Text(), obj1.Title, obj1.ChangeCount, v2.input0.Text(), obj2.Title, obj2.ChangeCount)
	// 期望校正记录（本次断言补强）：该断言自 277e6fd 起从未真正执行
	// （旧探针在此前 Skip）。首次实际运行实测 Gio 行为：初始 SetText 把
	// 光标复位到 (0,0)，空 Range 的 EditEvent 在光标处插入 → 前缀 "!one"
	// （与 R6 scen 词尾插入需显式 Range 的事实一致）。v1 的回写/隔离语义
	// 不变：只影响 v1 自己的 VM，count=1。
	if obj1.Title != "!one" || obj1.ChangeCount != 1 {
		t.Fatalf("v1 未正确回写: %q %d", obj1.Title, obj1.ChangeCount)
	}
	if obj2.Title != "two" || obj2.ChangeCount != 0 || v2.input0.Text() != "two" {
		t.Fatalf("v2 被串写: %q %d %q", obj2.Title, obj2.ChangeCount, v2.input0.Text())
	}
	// 帧4-5：quiet（有界观察，I4）——逐帧记录，便于复核对齐
	for i := 4; i <= 5; i++ {
		frame()
		fmt.Printf("SCEN dual-view frame=%d v1=%q obj1=%q c1=%d | v2=%q obj2=%q c2=%d\n",
			i, v1.input0.Text(), obj1.Title, obj1.ChangeCount, v2.input0.Text(), obj2.Title, obj2.ChangeCount)
	}
	// 夹具自证：两 View 持有**不同** Editor 实例（同类型指针必不同）；
	// 同一实例的话上面的"v2 不受影响"断言没有意义。
	if fmt.Sprintf("%p", &v1.input0) == fmt.Sprintf("%p", &v2.input0) {
		t.Fatalf("夹具自证失败: 两 View 的 Editor 应为不同实例")
	}
	fmt.Println("SCEN-DONE dual-view")
}
`)
	if issues := probeHealthIssues("dual-view", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（dual-view）: %v -- %s", issues, out)
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
	fmt.Println("SCEN-DONE empty-unicode")
}
`)
	if issues := probeHealthIssues("empty-unicode", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（empty-unicode）: %v -- %s", issues, out)
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
	fmt.Println("SCEN-DONE initial-external")
}
`)
	if issues := probeHealthIssues("initial-external", out); len(issues) > 0 {
		t.Fatalf("探针守卫失败（initial-external）: %v -- %s", issues, out)
	}
	if i := strings.Index(out, "SCEN"); i >= 0 {
		t.Log(out[i:])
	} else {
		t.Log(out)
	}
}

// ============================================================================
// R9-B T1：守卫矩阵与注入变异红。
// ============================================================================

// TestProbeGuardMatrix 守卫矩阵：健康输出零问题；失败/内部skip/零测试/
// 缺完成标记/子测试未成功各报对应原因。
func TestProbeGuardMatrix(t *testing.T) {
	healthy := `--- PASS: TestProbeX
[err=nil]
SCEN normalize frame=3 editor="ABC"
SCEN-DONE normalize
`
	if issues := probeHealthIssues("normalize", healthy); len(issues) != 0 {
		t.Fatalf("健康输出不应有问题: %v", issues)
	}
	matrix := []struct{ name, out, wantContains string }{
		{"subtest-fail", `--- FAIL: TestProbeX
[err=nil]
SCEN normalize x
`, "FAIL"},
		{"internal-skip", `--- SKIP: TestProbeX
[err=nil]
SCEN normalize x
`, "SKIP"},
		{"zero-tests", `[err=nil]
testing: warning: no tests to run
`, "零测试"},
		{"missing-marker", `[err=nil]
SCEN other-scenario x
`, "缺完成标记"},
		{"early-observation-only", `[err=nil]
SCEN normalize frame=3 editor="ABC"
`, "缺完成标记"},
		{"wrong-completion-id", `[err=nil]
SCEN-DONE normalize-extra
`, "缺完成标记"},
		{"subtest-error", `build failed
[err=exit status 1]
`, "未成功"},
	}
	for _, m := range matrix {
		issues := probeHealthIssues("normalize", m.out)
		if len(issues) == 0 {
			t.Errorf("%s: 守卫应报红", m.name)
			continue
		}
		if joined := strings.Join(issues, ";"); !strings.Contains(joined, m.wantContains) {
			t.Errorf("%s: 问题应含 %q, got %v", m.name, m.wantContains, issues)
		}
	}
}

// TestGuardRejectsInjectedSkip T1 变异红：在 normalize 场景的**外部副本**
// 注入可编译的 t.Skip（本文件内构造副本，原探针场景源码不动），真实
// rtProbeVM 运行之——旧双条件守卫（[err=nil]+无FAIL）对该输出是绿的，
// 新守卫必须同时报 SKIP 与缺完成标记。这不是编译失败当红：探针可编译，
// 只是运行时被 Skip 吞掉断言。
func TestGuardRejectsInjectedSkip(t *testing.T) {
	body := frameProbePrelude + `
func TestProbeNormalizeInjectedSkip(t *testing.T) {
	// 注意：本副本刻意不引用 v（编辑器）——注入点在 Skip，Skip 之后的
	// 一切（含编辑器断言）都不执行；可编译性由去引用保证。
	obj, _, r, frame := divSetup(t, "")
	t.Skip("injected: internal skip swallows the assertions below")
	r.Queue(key.EditEvent{Range: key.Range{}, Text: "abc"})
	frame()
	if obj.Title != "ABC" {
		t.Fatalf("never reached: %q", obj.Title)
	}
	// 注：Skip 之后的断言与 SCEN 记录都不会执行——守卫必须识别这一点。
}
`
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="UpperText"/>`, body)
	// 前提：旧双条件守卫对该输出是绿的——这正是要堵的假绿。
	if !strings.Contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") {
		t.Fatalf("前提失效：注入 Skip 的输出应能骗过旧双条件守卫: %s", out)
	}
	issues := probeHealthIssues("normalize", out)
	if len(issues) == 0 {
		t.Fatalf("守卫应拒绝注入的内部 SKIP: %s", out)
	}
	joined := strings.Join(issues, ";")
	if !strings.Contains(joined, "SKIP") || !strings.Contains(joined, "缺完成标记") {
		t.Errorf("应同时报 SKIP 与缺完成标记: %v", issues)
	}
	t.Logf("守卫拒绝原因: %v", issues)
}

// An early observation followed by return used to satisfy the old marker check.
// This is a real compiling/running probe, not a parser or compilation failure.
func TestGuardRejectsEarlyReturnAfterObservation(t *testing.T) {
	body := frameProbePrelude + `
func TestProbeEarlyObservation(t *testing.T) {
    _, v, _, _ := divSetup(t, "abc")
    fmt.Printf("SCEN normalize frame=3 editor=%q\n", v.input0.Text())
    return
}
`
	out := rtProbeVM(t, probeVMSrc, `<Input Value="{Title}" ValueChanged="UpperText"/>`, body)
	if !strings.Contains(out, "[err=nil]") || strings.Contains(out, "--- FAIL") ||
		strings.Contains(out, "--- SKIP") || !strings.Contains(out, "SCEN normalize") {
		t.Fatalf("early-return control must compile and run: %s", out)
	}
	if issues := probeHealthIssues("normalize", out); len(issues) == 0 ||
		!strings.Contains(strings.Join(issues, ";"), "缺完成标记") {
		t.Fatalf("early return must fail completion check: %v -- %s", issues, out)
	}
}
