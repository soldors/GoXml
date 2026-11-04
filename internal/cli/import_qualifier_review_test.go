package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewImportQualifiersCompile(t *testing.T) {
	for _, tt := range []struct {
		name, decl, page string
	}{
		{"input-collision", "type layout struct{}\n", `<Input Value="{Title}"/><Text Value="{Title}"/>`},
		{"input-only", "", `<Input Value="{Title}"/>`},
		{"row-alias-shadow", "type layout struct{}\ntype material struct{}\ntype op struct{}\n",
			`<For Each="layout_2" Index="op_2" In="{Items}" Key="{layout_2.ID}"><Button Text="{layout_2.Name}" Click="Tap(layout_2.ID, op_2)"/></For>`},
		{"display-only-for", "", `<For Each="item" In="{Items}"><Text Value="{item.Name}"/></For>`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := standaloneCLIProject(t)
			writeCLIFile(t, filepath.Join(root, "vm.go"), []byte(`package demo
type Item struct { ID int; Name string }
type VM struct { Title string; Items []Item }
func (v *VM) Tap(id, index int) {}
`+tt.decl))
			source := filepath.Join(root, "home.gxml")
			writeCLIFile(t, source, []byte(`<Goxml Version="1" ViewModel="VM"><Page Route="/"><Column>`+tt.page+`</Column></Page></Goxml>`))
			code, stdout, stderr := runStandaloneCLI(t, source, "--check")
			if code != ExitOK || !strings.Contains(stdout, "提交前检查通过") {
				t.Fatalf("generated page must compile: rc=%d stdout=%s stderr=%s", code, stdout, stderr)
			}
		})
	}
}
