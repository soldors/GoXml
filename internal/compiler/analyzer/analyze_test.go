package analyzer

import (
	"go/types"
	"path/filepath"
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/parser"
)

// ============================================================================
// 第 2 步：绑定路径解析
//
// 范围：BIND-001（首段）/ BIND-002（中间段）/ BIND-003（未导出）
// 以及建立 <For> 作用域。
//
// 不在本轮：BIND-004 类型匹配、事件签名校验。
// ============================================================================

// analyzeSrc 走完整管线（Parser → Expander → Analyze）。
func analyzeSrc(t *testing.T, src string) *Result {
	t.Helper()
	env := newTestEnv(t, ".")

	doc, pdiags := parser.Parse("case.gxml", []byte(src))
	if len(pdiags) > 0 {
		t.Fatalf("Parser 诊断:\n%s", pdiags[0].Error())
	}
	r := expander.Expand("case.gxml", doc)
	if len(r.Diags) > 0 {
		t.Fatalf("Expander 诊断:\n%s", r.Diags[0].Error())
	}
	return Analyze("case.gxml", r.Doc, r.Schemas, env)
}

// codesOf 汇总诊断码。
func codesOf(r *Result) map[string]int {
	out := make(map[string]int)
	for _, d := range r.Diags {
		out[d.Code]++
	}
	return out
}

// vmSrc 用夹具 ViewModel 包构造文档。
func vmSrc(body string) string {
	return `<Goxml Version="1" ViewModel="internal/compiler/analyzer/testdata/fixture.ViewModel">
  <Page Route="/">` + body + `</Page>
</Goxml>`
}

// ---------------------------------------------------------------------------
// 正常路径
// ---------------------------------------------------------------------------

func TestBindSingleSegment(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Title}"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
	if r.Facts.Len() != 1 {
		t.Fatalf("Facts.Len = %d", r.Facts.Len())
	}
}

func TestBindMultiSegment(t *testing.T) {
	// 三段路径：每一段都基于上一段解析出的实际类型。
	r := analyzeSrc(t, vmSrc(`<Text Value="{User.Profile.Name}"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}

	nf := onlyFacts(t, r)
	attrs := nf.Attrs()
	if len(attrs) != 1 {
		t.Fatalf("Attrs = %d", len(attrs))
	}
	p := attrs[0].Path
	if len(p.Steps) != 3 {
		t.Fatalf("Steps = %d, 期望 3", len(p.Steps))
	}
	if p.Steps[0].Name != "User" || p.Steps[1].Name != "Profile" || p.Steps[2].Name != "Name" {
		t.Errorf("Steps = %+v", p.Steps)
	}
	if p.Type.Kind != ValueString {
		t.Errorf("最终类型 Kind = %v, 期望 string", p.Type.Kind)
	}
	if p.RootKind != RootViewModel {
		t.Errorf("RootKind = %v", p.RootKind)
	}
}

// onlyFacts 返回唯一的节点事实。
func onlyFacts(t *testing.T, r *Result) *NodeFacts {
	t.Helper()
	if r.Facts.Len() != 1 {
		t.Fatalf("Facts.Len = %d, 期望 1", r.Facts.Len())
	}
	for _, n := range collectNodes(r.Doc.Page) {
		if nf, ok := r.Facts.Lookup(n); ok {
			return nf
		}
	}
	t.Fatal("找不到任何语义事实")
	return nil
}

// collectNodes 深度优先收集节点。
func collectNodes(n *contracts.NodeIR) []*contracts.NodeIR {
	if n == nil {
		return nil
	}
	out := []*contracts.NodeIR{n}
	for _, c := range n.Children {
		out = append(out, collectNodes(c)...)
	}
	if n.If != nil {
		for _, c := range n.If.Then {
			out = append(out, collectNodes(c)...)
		}
		for _, c := range n.If.Else {
			out = append(out, collectNodes(c)...)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 三类失败的精确映射
// ---------------------------------------------------------------------------

func TestBindRootNotFound(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Nope}"/>`))
	if r.Status != StatusPartial {
		t.Fatalf("Status = %v", r.Status)
	}
	if codesOf(r)[CodePathRoot] != 1 {
		t.Errorf("期望 1 条 BIND-001，实际 %v", codesOf(r))
	}
	// 失败绑定不得进入事实表。
	if r.Facts.Len() != 0 {
		t.Errorf("失败绑定不应产生事实, Len = %d", r.Facts.Len())
	}
}

func TestBindMiddleFieldNotFound(t *testing.T) {
	// 首段 User 存在，中间段 Nmae 不存在 → BIND-002，不是 BIND-001。
	r := analyzeSrc(t, vmSrc(`<Text Value="{User.Nmae}"/>`))
	codes := codesOf(r)
	if codes[CodePathField] != 1 {
		t.Errorf("期望 1 条 BIND-002，实际 %v", codes)
	}
	if codes[CodePathRoot] != 0 {
		t.Errorf("不应报 BIND-001，实际 %v", codes)
	}
}

func TestBindUnexportedRoot(t *testing.T) {
	// secret 存在于 ViewModel 根上但未导出 → BIND-003，不是 BIND-001（不存在）。
	r := analyzeSrc(t, vmSrc(`<Text Value="{secret}"/>`))
	codes := codesOf(r)
	if codes[CodePathUnexported] != 1 {
		t.Errorf("期望 1 条 BIND-003，实际 %v", codes)
	}
	if codes[CodePathRoot] != 0 {
		t.Errorf("字段存在时不应报「不存在」，实际 %v", codes)
	}
}

func TestBindUnexportedNested(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{User.note}"/>`))
	codes := codesOf(r)
	if codes[CodePathUnexported] != 1 {
		t.Errorf("期望 1 条 BIND-003，实际 %v", codes)
	}
	if codes[CodePathField] != 0 {
		t.Errorf("不应报 BIND-002，实际 %v", codes)
	}
	// 诊断消息应说明「存在但不可访问」，而不是「不存在」。
	msg := r.Diags[0].Message
	if !contains(msg, "not exported") {
		t.Errorf("消息应说明未导出: %q", msg)
	}
}

func TestBindNonStructSegment(t *testing.T) {
	// User.Age 是 int，再取字段属于不支持的表达式形式 → BIND-005。
	r := analyzeSrc(t, vmSrc(`<Text Value="{User.Age.Foo}"/>`))
	codes := codesOf(r)
	if codes[CodeBindUnsupported] != 1 {
		t.Errorf("期望 1 条 BIND-005，实际 %v", codes)
	}
	if codes[CodePathField] != 0 {
		t.Errorf("非结构体取字段不应报「字段不存在」，实际 %v", codes)
	}
}

// ---------------------------------------------------------------------------
// <For> 作用域
// ---------------------------------------------------------------------------

func TestForVarIsBindingRoot(t *testing.T) {
	// item 来自 For 作用域，不应被当成 ViewModel 字段。
	src := vmSrc(`
    <For Each="item" In="{Items}" Key="{item.ID}">
      <Text Value="{item.Name}"/>
    </For>`)
	r := analyzeSrc(t, src)
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
	if codesOf(r)[CodePathRoot] != 0 {
		t.Errorf("循环变量不应触发 BIND-001: %v", codesOf(r))
	}
}

func TestForScopeResolvedFor(t *testing.T) {
	src := vmSrc(`
    <For Each="item" In="{Items}" Key="{item.ID}">
      <Text Value="{item.Name}"/>
    </For>`)
	r := analyzeSrc(t, src)

	var forNode *contracts.NodeIR
	for _, n := range collectNodes(r.Doc.Page) {
		if n.Kind == contracts.KindFor {
			forNode = n
			break
		}
	}
	if forNode == nil {
		t.Fatal("找不到 For 节点")
	}
	nf, ok := r.Facts.Lookup(forNode)
	if !ok {
		t.Fatal("For 节点应有事实")
	}
	rf, ok := nf.For()
	if !ok {
		t.Fatal("For() 应返回 true")
	}
	if rf.Each != "item" {
		t.Errorf("Each = %q", rf.Each)
	}
	if rf.Elem.Kind != ValueStruct {
		t.Errorf("Elem.Kind = %v, 期望 struct", rf.Elem.Kind)
	}
	if rf.Key == nil {
		t.Error("Key 应已解析")
	}
}

func TestForVarOutOfScope(t *testing.T) {
	// 作用域外的 item 应报 BIND-001。
	r := analyzeSrc(t, vmSrc(`<Text Value="{item.Name}"/>`))
	if codesOf(r)[CodePathRoot] != 1 {
		t.Errorf("作用域外应报 BIND-001: %v", codesOf(r))
	}
}

func TestForNestedScopes(t *testing.T) {
	// 内层可访问外层变量。
	src := vmSrc(`
    <For Each="group" In="{Items}" Key="{group.ID}">
      <For Each="item" In="{Items}" Key="{item.ID}">
        <Text Value="{group.Name}"/>
      </For>
    </For>`)
	r := analyzeSrc(t, src)
	if r.Status != StatusOK {
		t.Fatalf("嵌套作用域应通过, Status = %v, 诊断: %v", r.Status, r.Diags)
	}
}

func TestForNotIterable(t *testing.T) {
	// Title 是 string，不可迭代 → SCOPE-004。
	src := vmSrc(`
    <For Each="ch" In="{Title}">
      <Text Value="x"/>
    </For>`)
	r := analyzeSrc(t, src)
	if codesOf(r)[CodeNotIterable] != 1 {
		t.Errorf("期望 SCOPE-004: %v", codesOf(r))
	}
}

func TestForMapIterable(t *testing.T) {
	// map 也是可迭代的（Lookup map[string]int）。
	src := vmSrc(`
    <For Each="v" In="{Lookup}" Index="k">
      <Text Value="{v}"/>
    </For>`)
	r := analyzeSrc(t, src)
	if codesOf(r)[CodeNotIterable] != 0 {
		t.Errorf("map 应可迭代: %v", codesOf(r))
	}
}

// ---------------------------------------------------------------------------
// <If Condition>
// ---------------------------------------------------------------------------

func TestIfConditionResolved(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`
    <If Condition="{Dirty}"><Then><Text Value="a"/></Then></If>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
}

func TestIfConditionMissingField(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`
    <If Condition="{Nope}"><Then><Text Value="a"/></Then></If>`))
	if codesOf(r)[CodePathRoot] != 1 {
		t.Errorf("期望 BIND-001: %v", codesOf(r))
	}
}

// ---------------------------------------------------------------------------
// 失败隔离
// ---------------------------------------------------------------------------

func TestFailureDoesNotPolluteFacts(t *testing.T) {
	// 一个坏绑定 + 一个好绑定：坏的不进事实，好的仍然进。
	src := vmSrc(`
    <Column>
      <Text Value="{User.Nmae}"/>
      <Text Value="{Title}"/>
    </Column>`)
	r := analyzeSrc(t, src)

	if codesOf(r)[CodePathField] != 1 {
		t.Fatalf("期望 1 条 BIND-002: %v", codesOf(r))
	}
	// 只有成功的那条产生事实。
	total := 0
	for _, n := range collectNodes(r.Doc.Page) {
		if nf, ok := r.Facts.Lookup(n); ok {
			total += len(nf.Attrs())
		}
	}
	if total != 1 {
		t.Errorf("成功绑定数 = %d, 期望 1", total)
	}
}

func TestMultipleIndependentFailures(t *testing.T) {
	// 多个独立错误都应报出，不因第一个失败而中止。
	src := vmSrc(`
    <Column>
      <Text Value="{Nope1}"/>
      <Text Value="{Nope2}"/>
      <Text Value="{User.Nmae}"/>
    </Column>`)
	r := analyzeSrc(t, src)
	codes := codesOf(r)
	if codes[CodePathRoot] != 2 {
		t.Errorf("BIND-001 数 = %d, 期望 2: %v", codes[CodePathRoot], codes)
	}
	if codes[CodePathField] != 1 {
		t.Errorf("BIND-002 数 = %d, 期望 1", codes[CodePathField])
	}
}

// ---------------------------------------------------------------------------
// 阻断：ViewModel 无法解析
// ---------------------------------------------------------------------------

func TestBlockedOnBadViewModel(t *testing.T) {
	src := `<Goxml Version="1" ViewModel="nosuch.Pkg">
  <Page Route="/"><Text Value="{X}"/></Page>
</Goxml>`
	r := analyzeSrc(t, src)

	if r.Status != StatusBlocked {
		t.Fatalf("Status = %v, 期望 Blocked", r.Status)
	}
	// 阻断后不得产生派生错误——只报 ViewModel 一条。
	if len(r.Diags) != 1 {
		t.Errorf("诊断数 = %d, 期望 1（不得产生派生错误）: %v", len(r.Diags), codesOf(r))
	}
	if r.Diags[0].Code != CodeViewModelType {
		t.Errorf("码 = %s", r.Diags[0].Code)
	}
}

func TestBlockedCannotGetVerifiedDoc(t *testing.T) {
	src := `<Goxml Version="1" ViewModel="nosuch.Pkg">
  <Page Route="/"><Text Value="x"/></Page>
</Goxml>`
	r := analyzeSrc(t, src)
	if _, err := r.Verified(); err == nil {
		t.Error("Blocked 不应发放凭证")
	}
}

func TestPartialCannotGetVerifiedDoc(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Nope}"/>`))
	if r.Status != StatusPartial {
		t.Fatalf("Status = %v", r.Status)
	}
	if _, err := r.Verified(); err == nil {
		t.Error("Partial 不应发放凭证")
	}
}

func TestOKCanGetVerifiedDoc(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Title}"/>`))
	v, err := r.Verified()
	if err != nil {
		t.Fatalf("OK 应发放凭证: %v", err)
	}
	if v.Document() == nil || v.Facts() == nil {
		t.Error("凭证内容不应为 nil")
	}
}

// ---------------------------------------------------------------------------
// 静态页面（无 ViewModel）
// ---------------------------------------------------------------------------

func TestStaticPageNoViewModel(t *testing.T) {
	src := `<Goxml Version="1">
  <Page Route="/"><Text Value="纯静态"/></Page>
</Goxml>`
	r := analyzeSrc(t, src)
	if r.Status != StatusOK {
		t.Fatalf("静态页面应通过, Status = %v, 诊断: %v", r.Status, r.Diags)
	}
}

func TestStaticPageBindingFails(t *testing.T) {
	// 没有 ViewModel 时，任何非循环变量的绑定都无法解析。
	src := `<Goxml Version="1">
  <Page Route="/"><Text Value="{Title}"/></Page>
</Goxml>`
	r := analyzeSrc(t, src)
	if codesOf(r)[CodePathRoot] != 1 {
		t.Errorf("期望 BIND-001: %v", codesOf(r))
	}
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// 确保 filepath 被使用（newTestEnv 依赖它）。
var _ = filepath.Join

// ---------------------------------------------------------------------------
// 方法作为路径段（规范 §4 v1 允许「方法结果字段」）
// ---------------------------------------------------------------------------

func TestBindMethodAsSegment(t *testing.T) {
	// 零参单返回值方法可作路径段。
	r := analyzeSrc(t, vmSrc(`<Text Value="{Display}"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
	nf := onlyFacts(t, r)
	p := nf.Attrs()[0].Path
	if p.Type.Kind != ValueString {
		t.Errorf("方法结果类型 = %v, 期望 string", p.Type.Kind)
	}
}

func TestBindMethodWithParamsRejected(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Format}"/>`))
	codes := codesOf(r)
	if codes[CodeBindUnsupported] != 1 {
		t.Errorf("带参方法应报 BIND-005: %v", codes)
	}
	if !contains(r.Diags[0].Message, "takes parameters") {
		t.Errorf("消息应说明带参数: %q", r.Diags[0].Message)
	}
}

func TestBindMethodMultiReturnRejected(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Pair}"/>`))
	if codesOf(r)[CodeBindUnsupported] != 1 {
		t.Errorf("多返回值方法应报 BIND-005: %v", codesOf(r))
	}
}

func TestBindUnexportedMethodRejected(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{hiddenGetter}"/>`))
	// 未导出方法在 pkg=nil 查找下不可见，按「无法解析」处理。
	codes := codesOf(r)
	if codes[CodePathRoot] == 0 && codes[CodePathUnexported] == 0 {
		t.Errorf("未导出方法应被拒绝: %v", codes)
	}
}

// ---------------------------------------------------------------------------
// 指针、嵌入、数组
// ---------------------------------------------------------------------------

func TestBindThroughPointer(t *testing.T) {
	// Owner 是 *User，字段访问需解引用。
	r := analyzeSrc(t, vmSrc(`<Text Value="{Owner.Name}"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
	p := onlyFacts(t, r).Attrs()[0].Path
	if len(p.Steps) != 2 {
		t.Fatalf("Steps = %d", len(p.Steps))
	}
	if p.Type.Kind != ValueString {
		t.Errorf("Kind = %v", p.Type.Kind)
	}
}

func TestBindEmbeddedField(t *testing.T) {
	// Meta 嵌入 ViewModel，Version 是提升字段。
	r := analyzeSrc(t, vmSrc(`<Text Value="{Version}"/>`))
	if r.Status != StatusOK {
		t.Fatalf("嵌入字段应可访问, Status = %v, 诊断: %v", r.Status, r.Diags)
	}
}

func TestForOverArray(t *testing.T) {
	// Codes 是 [3]int，可迭代。
	src := vmSrc(`
    <For Each="c" In="{Codes}">
      <Text Value="x"/>
    </For>`)
	r := analyzeSrc(t, src)
	if codesOf(r)[CodeNotIterable] != 0 {
		t.Errorf("数组应可迭代: %v", codesOf(r))
	}
}

// ---------------------------------------------------------------------------
// TypeInfo 的底层类别与 Stringer
// ---------------------------------------------------------------------------

func TestTypeInfoNamedStringer(t *testing.T) {
	// Status 是命名类型，底层 string，且实现 fmt.Stringer。
	r := analyzeSrc(t, vmSrc(`<Text Value="{State}"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v, 诊断: %v", r.Status, r.Diags)
	}
	ti := onlyFacts(t, r).Attrs()[0].Path.Type
	if ti.Kind != ValueString {
		t.Errorf("Kind = %v, 期望 string（底层类别）", ti.Kind)
	}
	if !ti.Named {
		t.Error("Named 应为 true")
	}
	if !ti.Stringer {
		t.Error("Stringer 应为 true")
	}
	if ti.Display == "" {
		t.Error("Display 不应为空")
	}
}

func TestTypeInfoKinds(t *testing.T) {
	// 直接检查字段类型的换算结果，而不是走绑定管线。
	//
	// 原因：走管线时 `{Count}`(int) 绑到 Text.Value(string) 会被
	// BIND-004 正确拒绝，无法据以观察 TypeInfo。类型换算本身是独立
	// 的可测试单元。
	env := newTestEnv(t, ".")
	named, err := env.LookupNamed(fixturePkgPath, []string{"ViewModel"})
	if err != nil {
		t.Fatalf("加载 ViewModel: %v", err)
	}
	st, ok := named[0].Underlying().(*types.Struct)
	if !ok {
		t.Fatal("ViewModel 不是结构体")
	}
	got := make(map[string]ValueKind, st.NumFields())
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		got[f.Name()] = valueKind(f.Type())
	}

	want := map[string]ValueKind{
		"Count":  ValueInt,
		"Dirty":  ValueBool,
		"Title":  ValueString,
		"Items":  ValueSlice,
		"Lookup": ValueMap,
		"User":   ValueStruct,
		"Owner":  ValuePointer,
		"Codes":  ValueOther, // 数组：v1 无专门类别
		"State":  ValueString,
	}
	for field, w := range want {
		if got[field] != w {
			t.Errorf("%s: Kind = %v, want %v", field, got[field], w)
		}
	}
}

// ---------------------------------------------------------------------------
// 诊断去重
// ---------------------------------------------------------------------------

func TestSameSpanSameCodeReportedOnce(t *testing.T) {
	// 同一属性只解析一次，不应产生重复诊断。
	r := analyzeSrc(t, vmSrc(`<Text Value="{Nope}"/>`))
	if len(r.Diags) != 1 {
		t.Errorf("诊断数 = %d, 期望 1: %v", len(r.Diags), codesOf(r))
	}
}

// fixturePkgPath 是夹具 ViewModel 包的导入路径。
const fixturePkgPath = "github.com/goxml/goxml/internal/compiler/analyzer/testdata/fixture"
