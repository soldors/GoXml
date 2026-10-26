package codegen

import (
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Observe only Spacer leaves; generated Grid/Flex code and returned dimensions
// remain unchanged. This measures Gio geometry without fonts or a GPU backend.
func gridLayoutProbe(t *testing.T, body, probe string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", mustGen(t, body, defaultOpts()), parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	leaves := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "Layout" {
			return true
		}
		literal, ok := method.X.(*ast.CompositeLit)
		if !ok {
			return true
		}
		typ, ok := literal.Type.(*ast.SelectorExpr)
		if !ok || typ.Sel.Name != "Spacer" {
			return true
		}
		pkg, ok := typ.X.(*ast.Ident)
		if !ok || pkg.Name != "layout" || len(call.Args) != 1 {
			return true
		}
		original := *call
		*call = ast.CallExpr{
			Fun: ast.NewIdent("gridObserve"),
			Args: []ast.Expr{
				&ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(leaves)},
				ast.NewIdent("gtx"),
				&original,
			},
		}
		leaves++
		return false
	})
	if leaves == 0 {
		t.Fatal("Grid probe has no observable Spacer leaves")
	}
	var generated strings.Builder
	if err := format.Node(&generated, fset, f); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	sum, err := os.ReadFile(filepath.Join(repoRootOf(t), "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"gen.go":        []byte(generated.String()),
		"probe_test.go": []byte(gridGeometryProbeHeader + probe),
		"go.mod":        []byte("module gridprobe\n\ngo 1.26.0\n\nrequire gioui.org v0.10.3\n"),
		"go.sum":        sum,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-p=1", "-count=1", "-v", "-timeout", "60s", "-run", "^TestProbeGrid", "./")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=mod", "GOPROXY=off", "GOWORK=off")
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Grid layout probe failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- PASS: TestProbeGrid") || strings.Contains(string(out), "--- SKIP:") {
		t.Fatalf("generated Grid probe did not execute its assertions:\n%s", out)
	}
	t.Logf("%s", out)
}

const gridGeometryProbeHeader = `package demo

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

type gridObservation struct {
	constraints layout.Constraints
	size image.Point
	calls int
}

var gridObserved map[int]gridObservation
var gridHitAreas bool
var gridHitTags map[int]*gridHitTag

type gridHitTag struct{ id int }

func gridObserve(id int, gtx layout.Context, dims layout.Dimensions) layout.Dimensions {
	previous := gridObserved[id]
	gridObserved[id] = gridObservation{gtx.Constraints, dims.Size, previous.calls + 1}
	if gridHitAreas {
		if gridHitTags == nil {
			gridHitTags = make(map[int]*gridHitTag)
		}
		tag := gridHitTags[id]
		if tag == nil {
			tag = &gridHitTag{id: id}
			gridHitTags[id] = tag
		}
		area := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
		event.Op(gtx.Ops, tag)
		area.Pop()
	}
	return dims
}

func gridAssertHit(t *testing.T, ops *op.Ops, at image.Point, wantID int, wantLocal image.Point) {
	t.Helper()
	var router input.Router
	for _, tag := range gridHitTags {
		for {
			if _, ok := router.Event(pointer.Filter{Target: tag, Kinds: pointer.Press}); !ok {
				break
			}
		}
	}
	router.Frame(ops)
	router.Queue(pointer.Event{
		Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary,
		Position: f32.Pt(float32(at.X), float32(at.Y)),
	})
	gotID := -1
	var local f32.Point
	for id, tag := range gridHitTags {
		for {
			e, ok := router.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
			if !ok {
				break
			}
			if press, ok := e.(pointer.Event); ok && press.Kind == pointer.Press {
				if gotID != -1 {
					t.Fatalf("point %v reached multiple leaves: %d and %d", at, gotID, id)
				}
				gotID, local = id, press.Position
			}
		}
	}
	if gotID != wantID {
		t.Fatalf("point %v hit leaf %d, want %d", at, gotID, wantID)
	}
	if wantID >= 0 && local != f32.Pt(float32(wantLocal.X), float32(wantLocal.Y)) {
		t.Fatalf("point %v reached leaf %d at local %v, want %v", at, gotID, local, wantLocal)
	}
}

func gridContext(ops *op.Ops, width, height int, scale float32) layout.Context {
	return layout.Context{
		Ops: ops,
		Constraints: layout.Constraints{Max: image.Pt(width, height)},
		Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
	}
}

func gridCheckWidths(t *testing.T, widths []int, rows int) {
	t.Helper()
	if len(gridObserved) != len(widths)*rows {
		t.Fatalf("observed %d cells, want %d (including padding)", len(gridObserved), len(widths)*rows)
	}
	for row := 0; row < rows; row++ {
		for col, width := range widths {
			cell := gridObserved[row*len(widths)+col]
			if cell.calls != 1 {
				t.Fatalf("row %d column %d observed %d times, want 1", row, col, cell.calls)
			}
			if cell.constraints.Min.X != width || cell.constraints.Max.X != width || cell.size.X != width {
				t.Errorf("row %d column %d: constraints=%v size=%v, want exact width %d", row, col, cell.constraints, cell.size, width)
			}
		}
	}
}
`

func TestGridCellHeightsRuntime(t *testing.T) {
	cases := []struct {
		name     string
		first    string
		firstRow int
	}{
		{"Natural", `<Column><Spacer Height="12"/></Column>`, 20},
		{"Fill", `<Column Height="Fill"><Spacer Height="12"/></Column>`, 20},
		{"SpacerNatural", `<Spacer/>`, 20},
		{"SpacerFill", `<Spacer Height="Fill"/>`, 20},
		{"SpacerExact", `<Spacer Height="40"/>`, 40},
		{"ColumnExact", `<Column Height="40"><Spacer Height="12"/></Column>`, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `<Grid Columns="* *" Gap="6">` + tc.first +
				`<Spacer Height="20"/><Spacer Height="14"/><Spacer Height="18"/></Grid>`
			gridLayoutProbe(t, body, fmt.Sprintf(`
func TestProbeGridHeights(t *testing.T) {
	for _, scale := range []float32{1, 1.5, 2} {
		for _, height := range []int{300, 600} {
			gridObserved = make(map[int]gridObservation)
			var ops op.Ops
			gtx := gridContext(&ops, 240, height, scale)
			v := &GoxmlDashboardView{}
			dims := v.Layout(gtx, &material.Theme{})
			wantHeight := gtx.Dp(%d) + gtx.Dp(6) + gtx.Dp(18)
			if dims.Size != image.Pt(240, wantHeight) {
				t.Fatalf("scale=%%v availableHeight=%%d: Grid size=%%v, want (240,%%d)", scale, height, dims.Size, wantHeight)
			}
			availableWidth := 240 - gtx.Dp(6)
			gridCheckWidths(t, []int{(availableWidth + 1) / 2, availableWidth / 2}, 2)
			for id, dp := range map[int]unit.Dp{2: 14, 3: 18} {
				if cell := gridObserved[id]; cell.size.Y != gtx.Dp(dp) {
					t.Errorf("second-row cell %%d height=%%d, want %%d", id, cell.size.Y, gtx.Dp(dp))
				}
			}
			t.Logf("scale=%%v availableHeight=%%d natural Grid size=%%v", scale, height, dims.Size)
		}
	}
}
`, tc.firstRow))
		})
	}
}

func TestGridColumnAlignment(t *testing.T) {
	body := `<Grid Columns="72 * 96" Gap="8">` +
		strings.Repeat(`<Spacer Height="12"/>`, 7) + `</Grid>`
	gridLayoutProbe(t, body, `
func TestProbeGridColumns(t *testing.T) {
	for _, scale := range []float32{1, 1.5, 2} {
		for _, width := range []int{0, 120, 240, 600} {
			gridObserved = make(map[int]gridObservation)
			var ops op.Ops
			gtx := gridContext(&ops, width, 400, scale)
			v := &GoxmlDashboardView{}
			dims := v.Layout(gtx, &material.Theme{})
			fixed0, fixed2, gap := gtx.Dp(72), gtx.Dp(96), gtx.Dp(8)
			flex := max(0, width-fixed0-fixed2-2*gap)
			gridCheckWidths(t, []int{fixed0, flex, fixed2}, 3)
			wantHeight := 3*gtx.Dp(12) + 2*gap
			if dims.Size.Y != wantHeight {
				t.Fatalf("scale=%v width=%d: height=%d, want %d (row Gap must be retained)", scale, width, dims.Size.Y, wantHeight)
			}
			t.Logf("scale=%v width=%d: columns=%v rows=3 height=%d", scale, width, []int{fixed0, flex, fixed2}, dims.Size.Y)
		}
	}
}
`)
}

func TestGridWeightsRuntime(t *testing.T) {
	cases := []struct {
		name    string
		columns string
		width   int
		want    []int
	}{
		{"Weighted", "2* * 3*", 600, []int{200, 100, 300}},
		{"Fractional", "0.5* * 1.5*", 600, []int{100, 200, 300}},
		{"RoundingCarry", "* * *", 100, []int{33, 34, 33}},
		{"NoPixelCompensation", "* * * * *", 116, []int{23, 23, 23, 23, 23}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `<Grid Columns="` + tc.columns + `">` +
				strings.Repeat(`<Spacer Height="12"/>`, len(tc.want)+1) + `</Grid>`
			var widths []string
			used := 0
			for _, width := range tc.want {
				widths = append(widths, strconv.Itoa(width))
				used += width
			}
			gridLayoutProbe(t, body, fmt.Sprintf(`
func TestProbeGridWeights(t *testing.T) {
	gridObserved = make(map[int]gridObservation)
	var ops op.Ops
	gtx := gridContext(&ops, %d, 400, 1)
	v := &GoxmlDashboardView{}
	dims := v.Layout(gtx, &material.Theme{})
	gridCheckWidths(t, []int{%s}, 2)
	if dims.Size != image.Pt(%d, 24) {
		t.Fatalf("Grid size=%%v, want (%%d,24)", dims.Size, %d)
	}
	t.Logf("availableWidth=%%d widths=%%v used=%%d", gtx.Constraints.Max.X, []int{%s}, dims.Size.X)
}
`, tc.width, strings.Join(widths, ","), used, used, strings.Join(widths, ",")))
		})
	}
}

func TestGridAlignRuntime(t *testing.T) {
	cases := []struct {
		name  string
		align string
		shift int
	}{
		{"Default", "", 0},
		{"Start", "Start", 0},
		{"Center", "Center", 10},
		{"End", "End", 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			align := ""
			if tc.align != "" {
				align = ` Align="` + tc.align + `"`
			}
			body := `<Grid Columns="64 64" Gap="8"` + align + `>` +
				`<Spacer Height="10"/><Spacer Height="30"/><Spacer Height="20"/><Spacer Height="40"/></Grid>`
			gridLayoutProbe(t, body, fmt.Sprintf(`
func TestProbeGridAlignment(t *testing.T) {
	gridHitAreas = true
	for _, scale := range []float32{1, 1.5, 2} {
		gridObserved = make(map[int]gridObservation)
		gridHitTags = make(map[int]*gridHitTag)
		var ops op.Ops
		gtx := gridContext(&ops, 512, 400, scale)
		v := &GoxmlDashboardView{}
		dims := v.Layout(gtx, &material.Theme{})
		columnWidth, gap := gtx.Dp(64), gtx.Dp(8)
		secondX := columnWidth + gap
		secondY := gtx.Dp(30) + gap
		shift := gtx.Dp(%d)
		gridCheckWidths(t, []int{columnWidth, columnWidth}, 2)
		if dims.Size != image.Pt(2*columnWidth+gap, secondY+gtx.Dp(40)) {
			t.Fatalf("scale=%%v unexpected Grid dimensions %%v", scale, dims.Size)
		}
		gridAssertHit(t, &ops, image.Pt(1, shift+1), 0, image.Pt(1, 1))
		gridAssertHit(t, &ops, image.Pt(secondX+1, 1), 1, image.Pt(1, 1))
		gridAssertHit(t, &ops, image.Pt(1, secondY+shift+1), 2, image.Pt(1, 1))
		gridAssertHit(t, &ops, image.Pt(secondX+1, secondY+1), 3, image.Pt(1, 1))
		if shift > 0 {
			gridAssertHit(t, &ops, image.Pt(1, shift-1), -1, image.Point{})
			gridAssertHit(t, &ops, image.Pt(1, secondY+shift-1), -1, image.Point{})
		} else {
			gridAssertHit(t, &ops, image.Pt(1, gtx.Dp(10)), -1, image.Point{})
			gridAssertHit(t, &ops, image.Pt(1, secondY+gtx.Dp(20)), -1, image.Point{})
		}
		gridAssertHit(t, &ops, image.Pt(columnWidth+gap/2, 1), -1, image.Point{})
		gridAssertHit(t, &ops, image.Pt(secondX+1, gtx.Dp(30)+gap/2), -1, image.Point{})
		t.Logf("scale=%%v short-cell offsets=(%%d,%%d) row/column gaps are empty", scale, shift, secondY+shift)
	}
}
`, tc.shift))
		})
	}
}

func TestGridOverflowRuntime(t *testing.T) {
	gridLayoutProbe(t, `<Grid Columns="40 60" Gap="8">`+
		`<Column><Spacer Width="100" Height="40"/></Column><Spacer Height="12"/></Grid>`, `
func TestProbeGridOverflow(t *testing.T) {
	gridHitAreas = true
	for _, height := range []int{10, 120} {
		gridObserved = make(map[int]gridObservation)
		gridHitTags = make(map[int]*gridHitTag)
		var ops op.Ops
		gtx := gridContext(&ops, 80, height, 1)
		v := &GoxmlDashboardView{}
		dims := v.Layout(gtx, &material.Theme{})
		if len(gridObserved) != 2 || gridObserved[0].size != image.Pt(100, 40) {
			t.Fatalf("nested fixed-size content was compressed: %v", gridObserved)
		}
		if gridObserved[1].size != image.Pt(60, 12) {
			t.Fatalf("overflow changed the second column: %v", gridObserved[1])
		}
		// The short second cell ends at y=12. This point must still reach
		// the first cell's overflowing child, beyond its 40px column.
		gridAssertHit(t, &ops, image.Pt(70, 20), 0, image.Pt(70, 20))
		gridAssertHit(t, &ops, image.Pt(49, 1), 1, image.Pt(1, 1))
		gridAssertHit(t, &ops, image.Pt(110, 20), -1, image.Point{})
		t.Logf("available=(80,%d) root=%v child=%v overflow hit is retained", height, dims.Size, gridObserved[0].size)
	}
}
`)
}

func TestGridCacheReplayRuntime(t *testing.T) {
	gridLayoutProbe(t, `<Grid Columns="40 *" Gap="8">`+
		`<Spacer Height="12"/><Spacer Height="20"/><Spacer Height="14"/><Spacer Height="18"/></Grid>`, `
func TestProbeGridCache(t *testing.T) {
	gridHitAreas = true
	gridObserved = make(map[int]gridObservation)
	gridHitTags = make(map[int]*gridHitTag)
	var ops op.Ops
	gtx := gridContext(&ops, 200, 120, 1)
	th := &material.Theme{}
	v := &GoxmlDashboardView{}
	first := v.Layout(gtx, th)
	gridCheckWidths(t, []int{40, 152}, 2)
	if !v.cached || first.Size != image.Pt(200, 46) {
		t.Fatalf("first frame did not record Grid: cached=%v size=%v", v.cached, first.Size)
	}

	gridObserved = make(map[int]gridObservation)
	ops.Reset()
	second := v.Layout(gtx, th)
	if len(gridObserved) != 0 || second != first {
		t.Fatalf("same-key frame relaid out Grid: leaves=%d dimensions=%v, want %v", len(gridObserved), second, first)
	}
	gridAssertHit(t, &ops, image.Pt(1, 29), 2, image.Pt(1, 1))

	steps := []struct {
		name string
		change func()
	}{
		{"Constraints", func() { gtx.Constraints.Max.X = 240 }},
		{"Metric", func() { gtx.Metric = unit.Metric{PxPerDp: 2, PxPerSp: 2} }},
		{"Theme", func() { th = &material.Theme{} }},
		{"Locale", func() { gtx.Locale.Language = "ar" }},
	}
	for _, step := range steps {
		step.change()
		gridObserved = make(map[int]gridObservation)
		ops.Reset()
		recorded := v.Layout(gtx, th)
		columnWidth, gap := gtx.Dp(40), gtx.Dp(8)
		gridCheckWidths(t, []int{columnWidth, gtx.Constraints.Max.X-columnWidth-gap}, 2)
		wantSize := image.Pt(gtx.Constraints.Max.X, gtx.Dp(20)+gap+gtx.Dp(18))
		if recorded.Size != wantSize {
			t.Fatalf("%s re-recorded size=%v, want %v", step.name, recorded.Size, wantSize)
		}
		gridObserved = make(map[int]gridObservation)
		ops.Reset()
		replayed := v.Layout(gtx, th)
		if len(gridObserved) != 0 || replayed != recorded {
			t.Fatalf("%s same-key replay changed layout: leaves=%d size=%v", step.name, len(gridObserved), replayed.Size)
		}
		gridAssertHit(t, &ops, image.Pt(columnWidth+gap+1, 1), 1, image.Pt(1, 1))
		t.Logf("%s invalidates once, then replays unchanged dimensions and hit regions", step.name)
	}

	var last layout.Dimensions
	allocs := testing.AllocsPerRun(200, func() {
		ops.Reset()
		last = v.Layout(gtx, th)
	})
	if allocs != 0 || len(gridObserved) != 0 || last != v.cacheDims {
		t.Fatalf("stable replay: allocations=%v leaves=%d dimensions=%v cached=%v", allocs, len(gridObserved), last, v.cacheDims)
	}
	t.Logf("stable cached Grid frames: allocs/frame=%v, no leaf layout", allocs)
}
`)
}
