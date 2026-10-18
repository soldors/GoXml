package analyzer

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/parser"
)

// ============================================================================
// Analyzer 整体验收：Status 传播与多错优先级
//
// 这些测试不针对单个功能，而是锁定「总入口的行为」：
// 状态如何传播、多个错误同时出现时的优先级、凭证发放条件、
// 以及「Analyze 不修改输入」这类结构性不变量。
// ============================================================================

// pipeline 是一次完整前端的产物。
type pipeline struct {
	Doc     *contracts.DocumentIR
	Schemas expander.SchemaRegistry
	Env     *TypeEnv
}

// runPipeline 对 .gxml 源码跑 Parser → Expander。
func runPipeline(t *testing.T, file string, src []byte) pipeline {
	t.Helper()
	env := newTestEnv(t, ".")

	doc, pdiags := parser.Parse(file, src)
	if len(pdiags) > 0 {
		t.Fatalf("Parser 诊断: %s", pdiags[0].Error())
	}
	er := expander.Expand(file, doc)
	if len(er.Diags) > 0 {
		t.Fatalf("Expander 诊断: %s", er.Diags[0].Error())
	}
	return pipeline{Doc: er.Doc, Schemas: er.Schemas, Env: env}
}

// analyzeOne 按夹具名跑完整管线并分析。
func analyzeOne(t *testing.T, name string) *Result {
	t.Helper()
	p := runPipeline(t, name+".gxml", readCase(t, name))
	return Analyze(name+".gxml", p.Doc, p.Schemas, p.Env)
}

// ---------------------------------------------------------------------------
// 不变量 1：StatusOK ⇔ 无诊断 ⇔ 可取得凭证
// ---------------------------------------------------------------------------

func TestStatusInvariantOK(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Title}"/>`))
	if r.Status != StatusOK {
		t.Fatalf("Status = %v", r.Status)
	}
	if len(r.Diags) != 0 {
		t.Errorf("OK 时不应有诊断: %v", codesOf(r))
	}
	if _, err := r.Verified(); err != nil {
		t.Errorf("OK 应可取得凭证: %v", err)
	}
}

// TestStatusInvariantNoOKWithDiagnostics 遍历所有失败夹具，确认
// 「有诊断却仍报 OK」不会发生——这是最容易破坏的不变量。
func TestStatusInvariantNoOKWithDiagnostics(t *testing.T) {
	for name, want := range expectationByCase {
		if want.status == StatusOK {
			continue
		}
		t.Run(name, func(t *testing.T) {
			r := analyzeOne(t, name)
			if r.Status == StatusOK {
				t.Fatalf("有诊断却报 OK: %v", codesOf(r))
			}
			if len(r.Diags) == 0 {
				t.Fatalf("状态为 %v 但没有任何诊断", r.Status)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 不变量 2：Blocked 优先于所有局部错误
// ---------------------------------------------------------------------------

func TestBlockedTakesPriorityOverLocalErrors(t *testing.T) {
	// 同时存在：ViewModel 不可解析 + 绑定路径错误 + 方法不存在 + For 不可迭代。
	// 期望只看得到 ViewModel 一条，且状态为 Blocked。
	src := `<Goxml Version="1" ViewModel="nosuch.Pkg">
  <Page Route="/">
    <Column>
      <Text Value="{Nope.Deep.Path}"/>
      <Button Text="x" Click="NoSuchMethod(1)"/>
      <For Each="i" In="{AlsoNope}"><Text Value="{i.X}"/></For>
    </Column>
  </Page>
</Goxml>`
	r := analyzeSrc(t, src)

	if r.Status != StatusBlocked {
		t.Fatalf("Status = %v, 期望 Blocked", r.Status)
	}
	if len(r.Diags) != 1 {
		t.Fatalf("诊断数 = %d, 期望 1（阻断后不得产生派生错误）: %v", len(r.Diags), codesOf(r))
	}
	if r.Diags[0].Code != CodeViewModelType {
		t.Errorf("唯一诊断应为 %s, 实际 %s", CodeViewModelType, r.Diags[0].Code)
	}
	if r.Facts.Len() != 0 {
		t.Errorf("阻断时 Facts 应为空, Len = %d", r.Facts.Len())
	}
	if _, err := r.Verified(); err == nil {
		t.Error("Blocked 不应发放凭证")
	}
}

// ---------------------------------------------------------------------------
// 不变量 3：Partial 保留独立成功项
// ---------------------------------------------------------------------------

func TestPartialKeepsIndependentSuccesses(t *testing.T) {
	src := vmSrc(`
    <Column>
      <Text Value="{Title}"/>
      <Text Value="{Nope}"/>
      <Input Value="{User.Name}" ValueChanged="OnNameChanged"/>
      <Button Text="x" Click="MissingMethod"/>
    </Column>`)
	r := analyzeSrc(t, src)

	if r.Status != StatusPartial {
		t.Fatalf("Status = %v", r.Status)
	}
	var attrs, events int
	for _, n := range collectNodes(r.Doc.Page) {
		if nf, ok := r.Facts.Lookup(n); ok {
			attrs += len(nf.Attrs())
			events += len(nf.Events())
		}
	}
	// 两个成功绑定：{Title} 与 {User.Name}。
	if attrs != 2 {
		t.Errorf("成功属性绑定数 = %d, 期望 2", attrs)
	}
	if events != 1 {
		t.Errorf("成功事件数 = %d, 期望 1", events)
	}
	if _, err := r.Verified(); err == nil {
		t.Error("Partial 不应发放凭证")
	}
}

// ---------------------------------------------------------------------------
// 不变量 4：跨类诊断不级联
// ---------------------------------------------------------------------------

func TestNoCascadeAcrossDiagnosticKinds(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		mustHave string
		mustNot  []string
	}{
		{
			name:     "路径失败不报类型不匹配",
			body:     `<Text Value="{Nope}"/>`,
			mustHave: CodePathRoot,
			mustNot:  []string{CodeBindTypeMismatch},
		},
		{
			name:     "取反失败不报类型不匹配",
			body:     `<If Condition="{!Nope}"><Then><Text Value="a"/></Then></If>`,
			mustHave: CodePathRoot,
			mustNot:  []string{CodeBindTypeMismatch},
		},
		{
			name:     "事件实参路径失败不报事件类型错误",
			body:     `<Button Text="x" Click="Delete(Nope)"/>`,
			mustHave: CodePathRoot,
			mustNot:  []string{CodeMethodArgType, CodeMethodArity},
		},
		{
			name:     "For 不可迭代不误报循环变量根错误",
			body:     `<For Each="ch" In="{Title}"><Text Value="{ch}"/></For>`,
			mustHave: CodeNotIterable,
			mustNot:  []string{CodePathRoot},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := analyzeSrc(t, vmSrc(c.body))
			codes := codesOf(r)
			if codes[c.mustHave] == 0 {
				t.Errorf("应报 %s: %v", c.mustHave, codes)
			}
			for _, bad := range c.mustNot {
				if codes[bad] != 0 {
					t.Errorf("不应级联报 %s: %v", bad, codes)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 不变量 5：Analyze 不修改输入 DocumentIR
// ---------------------------------------------------------------------------

func TestAnalyzeDoesNotMutateDocument(t *testing.T) {
	p := runPipeline(t, "case.gxml", []byte(vmSrc(`
    <Column>
      <Text Value="{Title}"/>
      <Text Value="{Nope}"/>
      <For Each="i" In="{Items}" Key="{i.ID}"><Text Value="{i.Name}"/></For>
      <Input Value="{User.Name}" ValueChanged="OnNameChanged"/>
    </Column>`)))

	before := snapshotDoc(p.Doc)
	_ = Analyze("case.gxml", p.Doc, p.Schemas, p.Env)
	after := snapshotDoc(p.Doc)

	if !reflect.DeepEqual(before, after) {
		t.Error("Analyze 修改了输入 DocumentIR；结构 IR 必须保持只读")
	}
}

// snapshotDoc 生成可比较的文档快照。
func snapshotDoc(doc *contracts.DocumentIR) []string {
	out := []string{}
	var walk func(n *contracts.NodeIR, depth int)
	walk = func(n *contracts.NodeIR, depth int) {
		if n == nil {
			return
		}
		out = append(out, fmt.Sprintf("%d|%v|%s|%d|%d|%v|%v|%d",
			depth, n.Kind, n.Tag, len(n.Attrs), len(n.Events),
			n.Background != nil, n.HasGap, len(n.Children)))
		for _, c := range n.Children {
			walk(c, depth+1)
		}
		if n.If != nil {
			for _, c := range n.If.Then {
				walk(c, depth+1)
			}
			for _, c := range n.If.Else {
				walk(c, depth+1)
			}
		}
	}
	if doc != nil {
		out = append(out, "vm="+doc.ViewModel.Value)
		walk(doc.Page, 0)
	}
	return out
}

// ---------------------------------------------------------------------------
// 不变量 6：同一位置同一码只报一次
// ---------------------------------------------------------------------------

func TestDiagnosticsDeduplicated(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Nope}"/>`))
	if len(r.Diags) != 1 {
		t.Errorf("诊断数 = %d, 期望 1: %v", len(r.Diags), codesOf(r))
	}
}

// ---------------------------------------------------------------------------
// 不变量 7：诊断位置指向源码
// ---------------------------------------------------------------------------

func TestDiagnosticSpanPointsAtSource(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Nope}"/>`))
	if len(r.Diags) == 0 {
		t.Fatal("应有诊断")
	}
	d := r.Diags[0]
	if d.Line == 0 && d.Col == 0 {
		t.Error("诊断缺少源码位置")
	}
	if d.File != "case.gxml" {
		t.Errorf("文件名 = %q, 期望 case.gxml", d.File)
	}
}

// ---------------------------------------------------------------------------
// 不变量 8：凭证签发后 Facts 冻结
// ---------------------------------------------------------------------------

func TestVerifiedFreezesFactsThroughPublicAPI(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Title}"/>`))
	v, err := r.Verified()
	if err != nil {
		t.Fatalf("应通过: %v", err)
	}
	if !v.Facts().Frozen() {
		t.Error("凭证签发后 Facts 应已冻结")
	}
	if _, err := r.Verified(); err != nil {
		t.Errorf("重复签发应成功（freeze 幂等）: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 不变量 9：事实中的 TypeInfo 与真实类型一致
// ---------------------------------------------------------------------------

func TestFactsTypeInfoConsistent(t *testing.T) {
	cases := []struct {
		body string
		want ValueKind
	}{
		{`<Text Value="{Title}"/>`, ValueString},
		{`<If Condition="{Dirty}"><Then><Text Value="a"/></Then></If>`, ValueBool},
		{`<Text Value="{State}"/>`, ValueString}, // 命名类型，底层 string
	}
	for _, c := range cases {
		r := analyzeSrc(t, vmSrc(c.body))
		if r.Status != StatusOK {
			t.Errorf("%s: Status = %v（%v）", c.body, r.Status, r.Diags)
			continue
		}
		var got ValueKind
		var found bool
		for _, n := range collectNodes(r.Doc.Page) {
			if nf, ok := r.Facts.Lookup(n); ok {
				if as := nf.Attrs(); len(as) > 0 {
					got = as[0].Path.Type.Kind
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s: 未找到事实", c.body)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Kind = %v, want %v", c.body, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// 不变量 10：失败节点不留半成品事实
// ---------------------------------------------------------------------------

func TestFailedNodesLeaveNoPartialFacts(t *testing.T) {
	// 同一节点上：一个属性成功、一个属性失败。
	// 期望事实里只有成功的那一个。
	src := vmSrc(`<Input Value="{Title}" Enabled="{Nope}"/>`)
	r := analyzeSrc(t, src)

	if codesOf(r)[CodePathRoot] != 1 {
		t.Fatalf("期望 BIND-001: %v", codesOf(r))
	}
	for _, n := range collectNodes(r.Doc.Page) {
		nf, ok := r.Facts.Lookup(n)
		if !ok {
			continue
		}
		for _, a := range nf.Attrs() {
			if a.Name == "Enabled" {
				t.Error("失败属性不应留下事实")
			}
		}
	}
}
