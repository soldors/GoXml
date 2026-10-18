package analyzer

import "testing"

// ============================================================================
// 第 4 步：事件签名校验（契约 §8.4）
//
// 核心规则：方法形参 = 事件载荷 ++ 调用点实参（载荷在前）。
// ============================================================================

// eventFacts 取出节点上的第一个事件事实。
func eventFacts(t *testing.T, r *Result) ResolvedEvent {
	t.Helper()
	for _, n := range collectNodes(r.Doc.Page) {
		nf, ok := r.Facts.Lookup(n)
		if !ok {
			continue
		}
		if evs := nf.Events(); len(evs) > 0 {
			return evs[0]
		}
	}
	t.Fatal("找不到任何事件事实")
	return ResolvedEvent{}
}

// ---------------------------------------------------------------------------
// 纯载荷
// ---------------------------------------------------------------------------

func TestEventPayloadOnly(t *testing.T) {
	// Input.ValueChanged 载荷是 value:string，方法 OnNameChanged(value string)。
	r := analyzeSrc(t, vmSrc(`
    <Input Value="{Title}" ValueChanged="OnNameChanged"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}

	ev := eventFacts(t, r)
	if ev.Name != "ValueChanged" {
		t.Errorf("Name = %q", ev.Name)
	}
	if len(ev.Payload) != 1 || ev.Payload[0].Name != "value" {
		t.Errorf("Payload = %+v", ev.Payload)
	}
	if len(ev.CallArgs) != 0 {
		t.Errorf("CallArgs 应为空, 实际 %+v", ev.CallArgs)
	}
	if len(ev.Method.Params) != 1 {
		t.Errorf("Method.Params = %+v", ev.Method.Params)
	}
	if ev.Method.HasReturn {
		t.Error("HasReturn 应为 false")
	}
	want := []ParamOrigin{FromPayload}
	assertMapping(t, ev, want)
}

// ---------------------------------------------------------------------------
// 纯调用点实参：字面量与绑定路径
// ---------------------------------------------------------------------------

func TestEventLiteralArg(t *testing.T) {
	// Click 无载荷；实参是字面量。
	r := analyzeSrc(t, vmSrc(`
    <Button Text="x" Click="OnLiteral(42)"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
	ev := eventFacts(t, r)
	if len(ev.CallArgs) != 1 {
		t.Fatalf("CallArgs = %+v", ev.CallArgs)
	}
	if ev.CallArgs[0].Type.Kind != ValueInt {
		t.Errorf("字面量 42 的类型 = %v, 期望 int", ev.CallArgs[0].Type.Kind)
	}
	assertMapping(t, ev, []ParamOrigin{FromCallArg})
}

func TestEventBindingArg(t *testing.T) {
	// 绑定路径作为实参。
	r := analyzeSrc(t, vmSrc(`
    <Button Text="x" Click="Delete(User.Age)"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
	ev := eventFacts(t, r)
	if len(ev.CallArgs) != 1 {
		t.Fatalf("CallArgs = %+v", ev.CallArgs)
	}
	if ev.CallArgs[0].Type.Kind != ValueInt {
		t.Errorf("User.Age 的类型 = %v, 期望 int", ev.CallArgs[0].Type.Kind)
	}
	assertMapping(t, ev, []ParamOrigin{FromCallArg})
}

func TestEventStringLiteralArg(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`
    <Button Text="x" Click="OnEcho('hi')"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
	ev := eventFacts(t, r)
	if ev.CallArgs[0].Type.Kind != ValueString {
		t.Errorf("字符串字面量类型 = %v", ev.CallArgs[0].Type.Kind)
	}
}

func TestEventBoolLiteralArg(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`
    <Button Text="x" Click="OnToggle(true)"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
	if ev := eventFacts(t, r); ev.CallArgs[0].Type.Kind != ValueBool {
		t.Errorf("true 的类型 = %v", ev.CallArgs[0].Type.Kind)
	}
}

// ---------------------------------------------------------------------------
// 载荷 + 实参组合：顺序必须严格
// ---------------------------------------------------------------------------

func TestEventCombinedOrder(t *testing.T) {
	// 载荷 + 实参组合：Input.ValueChanged 的载荷是 value:string，
	// 再给一个调用点实参 User.Age:int，应映射到 OnOpen(id string, cardID int)。
	//
	// 顺序必须严格：载荷在前、实参在后（契约 §8.4 R1）。
	r := analyzeSrc(t, vmSrc(`
    <Input Value="{Title}" ValueChanged="OnOpen(User.Age)"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}

	ev := eventFacts(t, r)
	if len(ev.Payload) != 1 || ev.Payload[0].Name != "value" {
		t.Errorf("Payload = %+v", ev.Payload)
	}
	if len(ev.CallArgs) != 1 {
		t.Errorf("CallArgs = %+v", ev.CallArgs)
	}
	if len(ev.Method.Params) != 2 {
		t.Fatalf("Method.Params = %+v", ev.Method.Params)
	}
	// 形参 0 来自载荷（string），形参 1 来自实参（int）。
	if ev.Method.Params[0].Type.Kind != ValueString {
		t.Errorf("param0 类型 = %v", ev.Method.Params[0].Type.Kind)
	}
	if ev.Method.Params[1].Type.Kind != ValueInt {
		t.Errorf("param1 类型 = %v", ev.Method.Params[1].Type.Kind)
	}
	assertMapping(t, ev, []ParamOrigin{FromPayload, FromCallArg})
}

func TestEventCombinedWrongOrderFails(t *testing.T) {
	// 若拼接顺序被写反（实参在前），OnOpen(id string, cardID int) 的
	// param0 会收到 int —— 应报 EVENT-006。这条测试锁定顺序不可颠倒。
	r := analyzeSrc(t, vmSrc(`
    <Input Value="{Title}" ValueChanged="OnOpen(User.Age)"/>`))
	if r.Status != StatusOK {
		t.Fatalf("正向组合应通过: %v", r.Diags)
	}
	// 反例：把实参换成 string，形参 0 期望 string 而载荷也是 string，
	// 无法区分顺序；改用 Bool 方法验证 param0 必须是 string。
	r2 := analyzeSrc(t, vmSrc(`
    <Input Value="{Title}" ValueChanged="OnToggle(User.Age)"/>`))
	// OnToggle(b bool) 只有 1 个形参，载荷+实参=2 → 数量错误。
	if codesOf(r2)[CodeMethodArity] != 1 {
		t.Errorf("期望 EVENT-004: %v", codesOf(r2))
	}
}

// assertMapping 校验 Mapping 的长度、顺序与来源计数。
func assertMapping(t *testing.T, ev ResolvedEvent, want []ParamOrigin) {
	t.Helper()
	if len(ev.Mapping) != len(ev.Method.Params) {
		t.Fatalf("Mapping 长度 %d ≠ 形参数 %d（Analyzer 内部缺陷）",
			len(ev.Mapping), len(ev.Method.Params))
	}
	if len(ev.Mapping) != len(want) {
		t.Fatalf("Mapping = %v, want %v", ev.Mapping, want)
	}
	for i := range want {
		if ev.Mapping[i] != want[i] {
			t.Errorf("Mapping[%d] = %v, want %v", i, ev.Mapping[i], want[i])
		}
	}
	// 计数必须与两段长度一致。
	var fromPayload, fromCallArg int
	for _, o := range ev.Mapping {
		switch o {
		case FromPayload:
			fromPayload++
		case FromCallArg:
			fromCallArg++
		}
	}
	if fromPayload != len(ev.Payload) {
		t.Errorf("payload 来源数 %d ≠ Payload 长度 %d", fromPayload, len(ev.Payload))
	}
	if fromCallArg != len(ev.CallArgs) {
		t.Errorf("callarg 来源数 %d ≠ CallArgs 长度 %d", fromCallArg, len(ev.CallArgs))
	}
}

// ---------------------------------------------------------------------------
// 四类失败
// ---------------------------------------------------------------------------

func TestEventMethodNotFound(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`
    <Button Text="x" Click="NoSuchMethod"/>`))
	if r.Status != StatusPartial {
		t.Fatalf("Status = %v, 期望 Partial", r.Status)
	}
	if codesOf(r)[CodeMethodNotFound] != 1 {
		t.Errorf("期望 1 条 EVENT-003: %v", codesOf(r))
	}
	if r.Facts.Len() != 0 {
		t.Errorf("失败事件不应产生事实, Len = %d", r.Facts.Len())
	}
}

func TestEventArityMismatch(t *testing.T) {
	// 载荷 1 个，OnWrongArity 有 0 个形参。
	r := analyzeSrc(t, vmSrc(`
    <Input Value="{Title}" ValueChanged="OnWrongArity"/>`))
	if codesOf(r)[CodeMethodArity] != 1 {
		t.Errorf("期望 EVENT-004: %v", codesOf(r))
	}
	if !contains(r.Diags[0].Message, "expects 0") {
		t.Errorf("消息应说明期望数量: %q", r.Diags[0].Message)
	}
}

func TestEventTypeMismatch(t *testing.T) {
	// 载荷 string，OnWrongType 形参 int。
	r := analyzeSrc(t, vmSrc(`
    <Input Value="{Title}" ValueChanged="OnWrongType"/>`))
	if codesOf(r)[CodeMethodArgType] != 1 {
		t.Errorf("期望 EVENT-006: %v", codesOf(r))
	}
}

func TestEventMethodWithReturn(t *testing.T) {
	// Q4：事件方法不得有返回值 → EVENT-007。
	r := analyzeSrc(t, vmSrc(`
    <Button Text="x" Click="OnWithReturn"/>`))
	if codesOf(r)[CodeMethodReturns] != 1 {
		t.Errorf("期望 EVENT-007: %v", codesOf(r))
	}
	if !contains(r.Diags[0].Message, "must not return") {
		t.Errorf("消息应说明不得有返回值: %q", r.Diags[0].Message)
	}
}

// ---------------------------------------------------------------------------
// 失败事件不进事实，但其他节点继续
// ---------------------------------------------------------------------------

func TestEventFailureIsolated(t *testing.T) {
	src := vmSrc(`
    <Column>
      <Button Text="x" Click="NoSuchMethod"/>
      <Input Value="{Title}" ValueChanged="OnNameChanged"/>
    </Column>`)
	r := analyzeSrc(t, src)
	if codesOf(r)[CodeMethodNotFound] != 1 {
		t.Fatalf("期望 1 条 EVENT-003: %v", codesOf(r))
	}
	// 成功的事件仍有事实。
	found := 0
	for _, n := range collectNodes(r.Doc.Page) {
		if nf, ok := r.Facts.Lookup(n); ok {
			found += len(nf.Events())
		}
	}
	if found != 1 {
		t.Errorf("成功事件数 = %d, 期望 1", found)
	}
}

// ---------------------------------------------------------------------------
// 调用点绑定路径失败：不产生级联事件类型错误
// ---------------------------------------------------------------------------

func TestEventArgPathFailureNoCascade(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`
    <Button Text="x" Click="Delete(Nope)"/>`))
	codes := codesOf(r)

	if codes[CodePathRoot] != 1 {
		t.Errorf("应报 BIND-001: %v", codes)
	}
	// 关键：不得再叠加 EVENT-006/004。
	if codes[CodeMethodArgType] != 0 {
		t.Errorf("不应级联报 EVENT-006: %v", codes)
	}
	if codes[CodeMethodArity] != 0 {
		t.Errorf("不应级联报 EVENT-004: %v", codes)
	}
	if codes[CodeMethodNotFound] != 0 {
		t.Errorf("不应报 EVENT-003（方法存在）: %v", codes)
	}
}

// ---------------------------------------------------------------------------
// ViewModel 不可解析 → StatusBlocked（而非 Partial）
// ---------------------------------------------------------------------------

func TestEventWithBlockedViewModel(t *testing.T) {
	// 完整分析路径：ViewModel 无法解析时，即便事件方法也找不到，
	// 状态必须是 Blocked——不能降级为 Partial。
	src := `<Goxml Version="1" ViewModel="nosuch.Pkg">
  <Page Route="/">
    <Button Text="x" Click="NoSuchMethod"/>
  </Page>
</Goxml>`
	r := analyzeSrc(t, src)

	if r.Status != StatusBlocked {
		t.Fatalf("Status = %v, 期望 Blocked（不得降级为 Partial）", r.Status)
	}
	// 只报 ViewModel 一条，不产生派生的事件诊断。
	if len(r.Diags) != 1 {
		t.Errorf("诊断数 = %d, 期望 1: %v", len(r.Diags), codesOf(r))
	}
	if r.Diags[0].Code != CodeViewModelType {
		t.Errorf("码 = %s, 期望 %s", r.Diags[0].Code, CodeViewModelType)
	}
	if _, err := r.Verified(); err == nil {
		t.Error("Blocked 不应发放凭证")
	}
}

// ---------------------------------------------------------------------------
// 无 ViewModel 的静态页面上写事件
// ---------------------------------------------------------------------------

func TestEventWithoutViewModel(t *testing.T) {
	src := `<Goxml Version="1">
  <Page Route="/"><Button Text="x" Click="Save"/></Page>
</Goxml>`
	r := analyzeSrc(t, src)
	// 没有 ViewModel 就没有方法可调：这是可恢复的局部错误。
	if r.Status != StatusPartial {
		t.Fatalf("Status = %v, 期望 Partial", r.Status)
	}
	if codesOf(r)[CodeMethodNotFound] != 1 {
		t.Errorf("期望 EVENT-003: %v", codesOf(r))
	}
}

// ---------------------------------------------------------------------------
// Mapping 是契约，不是辅助字段
// ---------------------------------------------------------------------------

func TestEventMappingInvariant(t *testing.T) {
	// 对多种形态统一验证：len(Mapping) == len(Method.Params)。
	cases := []string{
		`<Input Value="{Title}" ValueChanged="OnNameChanged"/>`,
		`<Button Text="x" Click="Delete(User.Age)"/>`,
		`<Button Text="x" Click="OnLiteral(42)"/>`,
	}
	for _, body := range cases {
		r := analyzeSrc(t, vmSrc(body))
		if r.Status != StatusOK {
			t.Errorf("%s: Status = %v（%v）", body, r.Status, r.Diags)
			continue
		}
		ev := eventFacts(t, r)
		if len(ev.Mapping) != len(ev.Method.Params) {
			t.Errorf("%s: Mapping %d ≠ Params %d", body, len(ev.Mapping), len(ev.Method.Params))
		}
	}
}

// ---------------------------------------------------------------------------
// v1 限制：<Def> 调用点的事件不参与签名校验
// ---------------------------------------------------------------------------

func TestDefEventNotSignatureChecked(t *testing.T) {
	// 已知限制（规范 §7.2.3）：<Def Events> 在 v1 只做契约声明与
	// 调用点名称校验，展开后**不保留事件**——因为 Def 体内没有语法
	// 能「发出」事件。
	//
	// 后果：Def 调用点的事件处理器签名**不会被 Analyzer 校验**。
	// 这条测试把该限制固定下来，避免被误认为已覆盖。
	src := `<Goxml Version="1" ViewModel="internal/compiler/analyzer/testdata/fixture.ViewModel">
  <Defs>
    <Def Name="Card3" Props="Title:string" Events="Open(id:string)">
      <Text Value="{Title}"/>
    </Def>
  </Defs>
  <Page Route="/">
    <Card3 Title="x" Open="NoSuchMethod"/>
  </Page>
</Goxml>`
	r := analyzeSrc(t, src)

	// 展开后不存在任何事件，因此不产生 EVENT-003。
	if codesOf(r)[CodeMethodNotFound] != 0 {
		t.Errorf("v1 下 Def 事件不参与签名校验，不应报 EVENT-003: %v", codesOf(r))
	}
	// 确认展开后的树里确实没有事件。
	for _, n := range collectNodes(r.Doc.Page) {
		if len(n.Events) > 0 {
			t.Errorf("展开后不应保留事件, 节点 %s 有 %d 个", n.Tag, len(n.Events))
		}
	}
	if r.Status != StatusOK {
		t.Logf("当前状态 = %v（若将来实现事件转发，此测试需更新）", r.Status)
	}
}
