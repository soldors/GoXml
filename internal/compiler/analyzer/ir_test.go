package analyzer

import (
	"reflect"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

// newNode 构造一个最小节点，用于旁表索引。
func newNode(tag string) *contracts.NodeIR {
	return &contracts.NodeIR{Kind: contracts.KindElement, Tag: tag}
}

// okResult 构造一个可通过校验的 Result。
func okResult() *Result {
	f := newFacts()
	n := newNode("Text")
	f.set(n, &NodeFacts{
		attrs: []ResolvedAttr{{
			Name:   "Value",
			Target: contracts.PropString,
			Path: &ResolvedPath{
				RootKind: RootViewModel,
				Steps:    []PathStep{{Name: "User"}, {Name: "Name"}},
				Type:     TypeInfo{Display: "string", Kind: ValueString},
			},
		}},
	})
	return &Result{
		Doc:    &contracts.DocumentIR{Version: "1"},
		Facts:  f,
		Status: StatusOK,
	}
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		StatusOK:      "OK",
		StatusPartial: "Partial",
		StatusBlocked: "Blocked",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", uint8(s), got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Verified：凭证发放条件
// ---------------------------------------------------------------------------

func TestVerifiedSucceedsOnOK(t *testing.T) {
	v, err := okResult().Verified()
	if err != nil {
		t.Fatalf("StatusOK 应发放凭证: %v", err)
	}
	if v == nil {
		t.Fatal("凭证不应为 nil")
	}
	if v.Document() == nil {
		t.Error("Document() 不应为 nil")
	}
	if v.Facts() == nil {
		t.Error("Facts() 不应为 nil")
	}
}

func TestVerifiedRejectsNonOK(t *testing.T) {
	for _, st := range []Status{StatusPartial, StatusBlocked} {
		r := okResult()
		r.Status = st
		// 给一个诊断，模拟真实情形。
		r.Diags = []contracts.CompilerError{{Code: "GXML-BIND-001", Message: "x"}}
		if v, err := r.Verified(); err == nil || v != nil {
			t.Errorf("%s 不应发放凭证（v=%v, err=%v）", st, v, err)
		}
	}
}

func TestVerifiedRejectsInconsistentState(t *testing.T) {
	// Status 说 OK，但有诊断——状态与诊断不一致本身就是缺陷。
	r := okResult()
	r.Diags = []contracts.CompilerError{{Code: "GXML-BIND-001", Message: "x"}}
	if _, err := r.Verified(); err == nil {
		t.Error("OK 与诊断并存时应拒绝发放凭证")
	}
}

func TestVerifiedRejectsNilParts(t *testing.T) {
	t.Run("nil Result", func(t *testing.T) {
		var r *Result
		if _, err := r.Verified(); err == nil {
			t.Error("nil Result 应报错")
		}
	})
	t.Run("nil Doc", func(t *testing.T) {
		r := okResult()
		r.Doc = nil
		if _, err := r.Verified(); err == nil {
			t.Error("Doc 为 nil 时应拒绝")
		}
	})
	t.Run("nil Facts", func(t *testing.T) {
		r := okResult()
		r.Facts = nil
		if _, err := r.Verified(); err == nil {
			t.Error("Facts 为 nil 时应拒绝")
		}
	})
}

// ---------------------------------------------------------------------------
// VerifiedDoc 防伪：字段不可导出
// ---------------------------------------------------------------------------

func TestVerifiedDocHasNoExportedFields(t *testing.T) {
	// 这是防伪机制的核心：字段未导出 + 只能由 Verified() 构造，
	// 因此包外无法凭空创建凭证。反射检查确保将来不会有人
	// 为了调试方便把字段改成导出。
	typ := reflect.TypeOf(VerifiedDoc{})
	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i); f.IsExported() {
			t.Errorf("VerifiedDoc.%s 被导出，会破坏凭证防伪", f.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Facts 冻结：发放凭证后不得再写
// ---------------------------------------------------------------------------

func TestFactsFrozenAfterVerified(t *testing.T) {
	r := okResult()
	if r.Facts.Frozen() {
		t.Fatal("验证前不应冻结")
	}
	if _, err := r.Verified(); err != nil {
		t.Fatalf("应通过: %v", err)
	}
	if !r.Facts.Frozen() {
		t.Error("验证后应冻结")
	}
}

func TestFactsSetPanicsAfterFreeze(t *testing.T) {
	r := okResult()
	if _, err := r.Verified(); err != nil {
		t.Fatalf("应通过: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Error("冻结后写入应 panic——这是内部不变式违反，不应静默忽略")
		}
	}()
	r.Facts.set(newNode("Row"), &NodeFacts{})
}

func TestFactsFreezeIsIdempotent(t *testing.T) {
	f := newFacts()
	f.freeze()
	f.freeze()
	if !f.Frozen() {
		t.Error("重复冻结应保持冻结")
	}
}

// ---------------------------------------------------------------------------
// Facts 只读：访问器返回副本
// ---------------------------------------------------------------------------

func TestNodeFactsAttrsReturnsCopy(t *testing.T) {
	f := newFacts()
	n := newNode("Text")
	f.set(n, &NodeFacts{attrs: []ResolvedAttr{{Name: "Value"}}})

	nf, ok := f.Lookup(n)
	if !ok {
		t.Fatal("应可查到")
	}

	// 修改返回的切片，不应影响内部状态。
	got := nf.Attrs()
	got[0].Name = "Mutated"

	again := nf.Attrs()
	if again[0].Name != "Value" {
		t.Errorf("内部状态被外部修改: %q", again[0].Name)
	}
}

func TestNodeFactsEventsReturnsCopy(t *testing.T) {
	f := newFacts()
	n := newNode("Button")
	f.set(n, &NodeFacts{events: []ResolvedEvent{{Name: "Click"}}})

	nf, _ := f.Lookup(n)
	got := nf.Events()
	got[0].Name = "Mutated"

	again := nf.Events()
	if again[0].Name != "Click" {
		t.Errorf("内部状态被外部修改: %q", again[0].Name)
	}
}

func TestNodeFactsForReturnsValue(t *testing.T) {
	f := newFacts()
	n := newNode("For")
	f.set(n, &NodeFacts{forCl: &ResolvedFor{Each: "item"}})

	nf, _ := f.Lookup(n)
	got, ok := nf.For()
	if !ok || got.Each != "item" {
		t.Fatalf("For() = %+v, %v", got, ok)
	}
	// 返回值拷贝：修改不影响内部。
	got.Each = "changed"
	again, _ := nf.For()
	if again.Each != "item" {
		t.Error("For() 应返回副本")
	}
}

func TestNodeFactsNilSafety(t *testing.T) {
	var nf *NodeFacts
	if nf.Attrs() != nil {
		t.Error("nil NodeFacts 的 Attrs 应返回 nil")
	}
	if nf.Events() != nil {
		t.Error("nil NodeFacts 的 Events 应返回 nil")
	}
	if _, ok := nf.For(); ok {
		t.Error("nil NodeFacts 的 For 应返回 false")
	}
}

func TestFactsLookupMissing(t *testing.T) {
	f := newFacts()
	if _, ok := f.Lookup(newNode("Text")); ok {
		t.Error("未记录的节点不应命中")
	}
	if _, ok := f.Lookup(nil); ok {
		t.Error("nil 节点不应命中")
	}
	var nilFacts *Facts
	if _, ok := nilFacts.Lookup(newNode("Text")); ok {
		t.Error("nil Facts 不应命中")
	}
}

func TestFactsLen(t *testing.T) {
	f := newFacts()
	if f.Len() != 0 {
		t.Errorf("空旁表 Len = %d", f.Len())
	}
	f.set(newNode("Text"), &NodeFacts{})
	f.set(newNode("Row"), &NodeFacts{})
	if f.Len() != 2 {
		t.Errorf("Len = %d, 期望 2", f.Len())
	}
	var nilFacts *Facts
	if nilFacts.Len() != 0 {
		t.Error("nil Facts 的 Len 应为 0")
	}
}

// ---------------------------------------------------------------------------
// 失败节点不进事实表
// ---------------------------------------------------------------------------

func TestFailedNodeHasNoFacts(t *testing.T) {
	// 约定：校验失败的属性只产出诊断，不产出事实。
	// 因此一个「有诊断的节点」在旁表中应当不存在或为空。
	f := newFacts()
	okNode := newNode("Text")
	badNode := newNode("Text")
	f.set(okNode, &NodeFacts{attrs: []ResolvedAttr{{Name: "Value"}}})

	if _, exists := f.Lookup(badNode); exists {
		t.Error("未校验通过的节点不应有事实")
	}
}

// ---------------------------------------------------------------------------
// 枚举可读性
// ---------------------------------------------------------------------------

func TestRootKindString(t *testing.T) {
	cases := map[RootKind]string{
		RootViewModel: "ViewModel",
		RootForVar:    "ForVar",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("RootKind(%d) = %q, want %q", uint8(k), got, want)
		}
	}
}

func TestValueKindString(t *testing.T) {
	cases := map[ValueKind]string{
		ValueString: "string", ValueBool: "bool", ValueInt: "int",
		ValueFloat: "float", ValueStruct: "struct", ValueSlice: "slice",
		ValueMap: "map", ValuePointer: "pointer", ValueInterface: "interface",
		ValueOther: "other", ValueUnknown: "unknown",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("ValueKind(%d) = %q, want %q", uint8(k), got, want)
		}
	}
}

func TestParamOriginString(t *testing.T) {
	cases := map[ParamOrigin]string{
		FromPayload: "payload",
		FromCallArg: "callarg",
	}
	for o, want := range cases {
		if got := o.String(); got != want {
			t.Errorf("ParamOrigin(%d) = %q, want %q", uint8(o), got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// 事件映射的长度不变量
// ---------------------------------------------------------------------------

func TestEventMappingMatchesMethodParams(t *testing.T) {
	// 契约 §8.4 R2：Mapping 长度必须等于方法形参数量。
	// 这里用一个符合约定的事件验证结构与不变量自洽。
	ev := ResolvedEvent{
		Name: "ValueChanged",
		Payload: []ResolvedParam{
			{Name: "value", Type: TypeInfo{Kind: ValueString}, Span: contracts.SourceSpan{}},
		},
		CallArgs: nil,
		Method: ResolvedMethod{
			Name:   "OnNameChanged",
			Params: []ResolvedParam{{Name: "value", Type: TypeInfo{Kind: ValueString}}},
		},
		Mapping: []ParamOrigin{FromPayload},
	}
	if len(ev.Mapping) != len(ev.Method.Params) {
		t.Errorf("Mapping 长度 %d ≠ 形参数 %d", len(ev.Mapping), len(ev.Method.Params))
	}
	if ev.Mapping[0] != FromPayload {
		t.Errorf("Mapping[0] = %v, 期望 payload", ev.Mapping[0])
	}
}

func TestEventCombinedMappingOrder(t *testing.T) {
	// 载荷在前、实参在后（契约 §8.4 R1）。
	ev := ResolvedEvent{
		Payload:  []ResolvedParam{{Name: "id", Type: TypeInfo{Kind: ValueString}}},
		CallArgs: []ResolvedParam{{Name: "", Type: TypeInfo{Kind: ValueInt}}},
		Method: ResolvedMethod{
			Name: "OnOpen",
			Params: []ResolvedParam{
				{Name: "id", Type: TypeInfo{Kind: ValueString}},
				{Name: "", Type: TypeInfo{Kind: ValueInt}},
			},
		},
		Mapping: []ParamOrigin{FromPayload, FromCallArg},
	}
	if len(ev.Mapping) != 2 {
		t.Fatalf("Mapping = %v", ev.Mapping)
	}
	if ev.Mapping[0] != FromPayload || ev.Mapping[1] != FromCallArg {
		t.Errorf("顺序应为 payload→callarg, 实际 %v", ev.Mapping)
	}
}

// ---------------------------------------------------------------------------
// nil 安全与未知枚举值
// ---------------------------------------------------------------------------

func TestFactsNilSafety(t *testing.T) {
	// 只读方法必须对 nil 接收者安全；set 是内部构建器，
	// nil 上调用属于程序员错误，不在契约内。
	var f *Facts
	if f.Frozen() {
		t.Error("nil Facts 不应报告已冻结")
	}
	f.freeze() // 不应 panic
	if f.Frozen() {
		t.Error("nil Facts 冻结后仍应为未冻结")
	}
	if f.Len() != 0 {
		t.Error("nil Facts 的 Len 应为 0")
	}
	if _, ok := f.Lookup(newNode("Text")); ok {
		t.Error("nil Facts 不应命中")
	}
}

func TestUnknownEnumStrings(t *testing.T) {
	// 未知值必须有可读输出，不能直接 panic 或返回空串混淆。
	if got := Status(99).String(); got == "" {
		t.Error("未知 Status 应有可读输出")
	}
	if got := RootKind(99).String(); got == "" {
		t.Error("未知 RootKind 应有可读输出")
	}
	if got := ParamOrigin(99).String(); got == "" {
		t.Error("未知 ParamOrigin 应有可读输出")
	}
}
