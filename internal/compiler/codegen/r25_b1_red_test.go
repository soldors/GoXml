package codegen

// R25-B B1 反例（基线红）/ candidateRef 绿证：规范 §3.1.1.2 已列出
// <Expanded>、Parser 已准入，基线 Codegen 明确拒绝（输出契约 §4.6）；
// candidateRef 实现后翻转断言为 Flexed 弹性包装。

import (
	"strings"
	"testing"
)

// 【分支差异】基线断言拒绝；candidateRef 断言已实现并产出 Flexed 弹性包装。
func TestR25B1ExpandedRejectedByCodegen(t *testing.T) {
	v := verified(t, `<Row Height="80"><Text Value="左"/><Expanded><Text Value="{Title}"/></Expanded><Text Value="右"/></Row>`)
	out, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("candidateRef 应实现 <Expanded>（规范 §3.1.1.2），实际: %v", err)
	}
	if !strings.Contains(string(out), ".Flexed(1, func(gtx ") {
		t.Fatalf("Expanded 子项应为 Flexed(1) 弹性包装: %.600s", out)
	}
}
