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
