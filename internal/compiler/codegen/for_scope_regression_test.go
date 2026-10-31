package codegen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runScopeProbe compiles and executes generated code without creating a window
// or a GPU context. Keep temporary packages under testdata so ./... ignores them.
func runScopeProbe(t *testing.T, source []byte, probe string) {
	t.Helper()
	root := repoRootOf(t)
	base := filepath.Join(root, "internal", "compiler", "codegen", "testdata")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "scopeprobe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for name, content := range map[string][]byte{"gen.go": source, "probe_test.go": []byte(scopeProbeHeader + probe)} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-p", "2", "-count=1", "-timeout", "60s", "-run", "^TestProbe", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated code probe failed: %v\n%s", err, out)
	}
}

const scopeProbeHeader = `package demo

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/goxml/goxml/internal/compiler/analyzer/testdata/fixture"
)

func probeClick(t *testing.T, vm *fixture.ViewModel, y float32) {
	t.Helper()
	v, err := NewGoxmlDashboardView(vm)
	if err != nil { t.Fatal(err) }
	var router input.Router
	gtx := layout.Context{
		Ops: new(op.Ops),
		Source: router.Source(),
		Constraints: layout.Exact(image.Pt(200, 200)),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	th := material.NewTheme()
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		router.Frame(gtx.Ops)
	}
	frame()
	router.Queue(
		pointer.Event{Position: f32.Pt(20, y), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(20, y), Kind: pointer.Release, Source: pointer.Mouse},
	)
	frame()
}
`

func TestForRowScopeGeneratedCode(t *testing.T) {
	cases := []struct {
		name, body, probe string
	}{
		{"else_guard", `<For Each="item" In="{Items}" Key="{item.ID}"><If Condition="{item.Done}"><Then><Text Value="done"/></Then><Else><Button Text="choose" Click="RecordNum(item.ID)"/></Else></If></For>`, `
func TestProbeElse(t *testing.T) {
	vm := &fixture.ViewModel{Items: []fixture.Item{{ID: 73}}}
	probeClick(t, vm, 15)
	if vm.LastNum != 73 { t.Fatalf("else click = %d, want 73", vm.LastNum) }
}`},
		{"negated_guard", `<For Each="item" In="{Items}" Key="{item.ID}"><If Condition="{!item.Done}"><Then><Button Text="choose" Click="RecordNum(item.ID)"/></Then></If></For>`, `
func TestProbeNegated(t *testing.T) {
	vm := &fixture.ViewModel{Items: []fixture.Item{{ID: 84}}}
	probeClick(t, vm, 15)
	if vm.LastNum != 84 { t.Fatalf("negated click = %d, want 84", vm.LastNum) }
}`},
		{"index_in_branch_event", `<For Each="item" Index="i" In="{Items}" Key="{item.ID}"><If Condition="{item.Done}"><Then><Text Value="done"/></Then><Else><Button Text="choose" Click="RecordNum(i)"/></Else></If></For>`, `
func TestProbeIndexArg(t *testing.T) {
	vm := &fixture.ViewModel{LastNum: -1, Items: []fixture.Item{{ID: 12}, {ID: 34}}}
	probeClick(t, vm, 50)
	if vm.LastNum != 1 { t.Fatalf("second row index = %d, want 1", vm.LastNum) }
}`},
		{"index_key", `<For Each="item" Index="i" In="{Items}" Key="{i}"><Button Text="choose" Click="RecordNum(item.ID)"/></For>`, `
func TestProbeIndexKey(t *testing.T) {
	vm := &fixture.ViewModel{Items: []fixture.Item{{ID: 12}, {ID: 34}}}
	probeClick(t, vm, 50)
	if vm.LastNum != 34 { t.Fatalf("second row value = %d, want 34", vm.LastNum) }
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := verified(t, tc.body)
			first, err := Generate(v, bindingOpts())
			if err != nil {
				t.Fatal(err)
			}
			second, err := Generate(v, bindingOpts())
			if err != nil || !bytes.Equal(first, second) {
				t.Fatalf("generation must be deterministic: %v", err)
			}
			runScopeProbe(t, first, tc.probe)
		})
	}
}
