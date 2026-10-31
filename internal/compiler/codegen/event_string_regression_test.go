package codegen

import (
	"bytes"
	"testing"
)

func TestEventStringCommaGeneratedCode(t *testing.T) {
	// Commas, quotes, backslashes and XML-decoded newlines must remain data.
	body := `<For Each="item" In="{Items}" Key="{item.ID}"><Button Text="choose" Click="RecordText('a, item.Name &quot;);&#10;\tail')"/></For>`
	v := verified(t, body)
	first, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(v, bindingOpts())
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("generation must be deterministic: %v", err)
	}
	runScopeProbe(t, first, `
func TestProbeStringData(t *testing.T) {
	vm := &fixture.ViewModel{Items: []fixture.Item{{ID: 12}}}
	probeClick(t, vm, 15)
	const want = "a, item.Name \");\n\\tail"
	if vm.LastText != want {
		t.Fatalf("string event argument changed: %q", vm.LastText)
	}
}`)
}

// TestEventEmptyStringArgGeneratedCode 锁定空字符串实参的端到端行为：
// 合法、生成可编译、运行期值不变（空串既不被丢弃也不被当成缺省实参）。
// R2-zcode 切片 2 的边界之一。
func TestEventEmptyStringArgGeneratedCode(t *testing.T) {
	body := `<Button Text="send" Click="RecordText('')"/>`
	v := verified(t, body)
	first, err := Generate(v, bindingOpts())
	if err != nil {
		t.Fatalf("空字符串实参必须合法: %v", err)
	}
	second, err := Generate(v, bindingOpts())
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("generation must be deterministic: %v", err)
	}
	if !bytes.Contains(first, []byte(`vm.RecordText("")`)) {
		t.Fatalf("生成代码应传空字符串字面量:\n%s", first)
	}
	runScopeProbe(t, first, `
func TestProbeEmptyStringArg(t *testing.T) {
	vm := &fixture.ViewModel{LastText: "sentinel"}
	probeClick(t, vm, 15)
	if vm.LastText != "" {
		t.Fatalf("empty string arg = %q, want \"\"", vm.LastText)
	}
}`)
}
