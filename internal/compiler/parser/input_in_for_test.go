package parser

import (
	"strings"
	"testing"
)

// ============================================================================
// D2：GXML-SCOPE-005（<For> 内含 <Input> 的 v1 拒绝）
//
// 优先级硬约束（Input 输出契约 §7.3）：
//   无 Key + 含 Input  → 只报 SCOPE-003（既有规则优先，不叠加 005）
//   有 Key + 含 Input  → SCOPE-005
// 两条规则不叠加——同一处只报一条。
// ============================================================================

// parseCodes 解析源码并收集诊断码。
func parseCodes(t *testing.T, body string) []string {
	t.Helper()
	src := `<Goxml Version="1"><Page Route="/">` + body + `</Page></Goxml>`
	_, diags := Parse("t.gxml", []byte(src))
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	return codes
}

func hasCode(codes []string, code string) bool {
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

// TestInputInForWithoutKeyReportsSCOPE003 场景 1：无 Key + Input → 只报 SCOPE-003。
func TestInputInForWithoutKeyReportsSCOPE003(t *testing.T) {
	codes := parseCodes(t, `<For Each="i" In="{Items}"><Input Value="x"/></For>`)

	if !hasCode(codes, CodeKey) {
		t.Errorf("应报 SCOPE-003（缺 Key），实际 %v", codes)
	}
	if hasCode(codes, CodeInputInFor) {
		t.Errorf("无 Key 时不得叠加 SCOPE-005，实际 %v", codes)
	}
	if len(codes) != 1 {
		t.Errorf("应只报一条诊断，实际 %d 条: %v", len(codes), codes)
	}
}

// TestInputInForWithKeyReportsSCOPE005 场景 2：有 Key + Input → SCOPE-005。
func TestInputInForWithKeyReportsSCOPE005(t *testing.T) {
	codes := parseCodes(t, `<For Each="i" In="{Items}" Key="{i.ID}"><Input Value="x"/></For>`)

	if !hasCode(codes, CodeInputInFor) {
		t.Errorf("应报 SCOPE-005，实际 %v", codes)
	}
	if hasCode(codes, CodeKey) {
		t.Errorf("有 Key 时不应报 SCOPE-003，实际 %v", codes)
	}
	if len(codes) != 1 {
		t.Errorf("应只报一条诊断，实际 %d 条: %v", len(codes), codes)
	}
}

// TestStatefulWithoutKeyStillReportsSCOPE003 场景 3：其他有状态控件（Button）
// 无 Key 时，保持既有 SCOPE-003 行为（不被本次改动影响）。
func TestStatefulWithoutKeyStillReportsSCOPE003(t *testing.T) {
	codes := parseCodes(t, `<For Each="i" In="{Items}"><Button Text="a" Click="Increment"/></For>`)

	if !hasCode(codes, CodeKey) {
		t.Errorf("Button 无 Key 应报 SCOPE-003，实际 %v", codes)
	}
	if hasCode(codes, CodeInputInFor) {
		t.Errorf("不含 Input 时不得报 SCOPE-005，实际 %v", codes)
	}
}

// TestStatelessWithoutKeyStillParses 场景 4：无状态控件 + 无 Key → 仍通过。
func TestStatelessWithoutKeyStillParses(t *testing.T) {
	codes := parseCodes(t, `<For Each="i" In="{Items}"><Text Value="{i.Name}"/></For>`)

	if len(codes) != 0 {
		t.Errorf("无状态循环无 Key 应通过，实际诊断 %v", codes)
	}
}

// TestInputInNestedContainerInFor 场景 5：Input 藏在嵌套容器中同样拒绝。
func TestInputInNestedContainerInFor(t *testing.T) {
	codes := parseCodes(t,
		`<For Each="i" In="{Items}" Key="{i.ID}"><Column><Row><Input Value="x"/></Row></Column></For>`)

	if !hasCode(codes, CodeInputInFor) {
		t.Errorf("嵌套容器中的 Input 应被检测，实际 %v", codes)
	}
}

// TestInputInIfBranchInsideFor Input 藏在 <If> 分支里同样拒绝
// （containsInputNode 的递归范围必须覆盖 If.Then/If.Else）。
func TestInputInIfBranchInsideFor(t *testing.T) {
	// <If> 必须有 <Then>；测 <Else> 时也要给出合法的 <Then>。
	cases := map[string]string{
		"Then": `<For Each="i" In="{Items}" Key="{i.ID}"><If Condition="{i.Done}">` +
			`<Then><Input Value="x"/></Then></If></For>`,
		"Else": `<For Each="i" In="{Items}" Key="{i.ID}"><If Condition="{i.Done}">` +
			`<Then><Text Value="done"/></Then><Else><Input Value="x"/></Else></If></For>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			codes := parseCodes(t, body)
			if !hasCode(codes, CodeInputInFor) {
				t.Errorf("<%s> 分支中的 Input 应被检测，实际 %v", name, codes)
			}
		})
	}
}

// TestInputInNestedForRejected 嵌套 For 内的 Input 同样拒绝。
func TestInputInNestedForRejected(t *testing.T) {
	codes := parseCodes(t,
		`<For Each="i" In="{Items}" Key="{i.ID}"><For Each="j" In="{i.Tags}" Key="{j}"><Input Value="x"/></For></For>`)

	if !hasCode(codes, CodeInputInFor) {
		t.Errorf("嵌套 For 内的 Input 应被检测，实际 %v", codes)
	}
}

// TestStaticInputNotRejected 静态 Input（不在 For 内）不得报 SCOPE-005。
func TestStaticInputNotRejected(t *testing.T) {
	codes := parseCodes(t, `<Column><Input Value="x"/></Column>`)

	if hasCode(codes, CodeInputInFor) {
		t.Errorf("静态 Input 不应报 SCOPE-005，实际 %v", codes)
	}
}

// TestInputInForMessageMentionsV1AndStatic 诊断消息必须说明 v1 不支持与静态要求。
func TestInputInForMessageMentionsV1AndStatic(t *testing.T) {
	src := `<Goxml Version="1"><Page Route="/">` +
		`<For Each="i" In="{Items}" Key="{i.ID}"><Input Value="x"/></For>` +
		`</Page></Goxml>`
	_, diags := Parse("t.gxml", []byte(src))
	if len(diags) == 0 {
		t.Fatal("应产生诊断")
	}
	msg := diags[0].Message
	for _, want := range []string{"not supported in v1", "static", "outside"} {
		if !strings.Contains(msg, want) {
			t.Errorf("消息应包含 %q: %q", want, msg)
		}
	}
}
