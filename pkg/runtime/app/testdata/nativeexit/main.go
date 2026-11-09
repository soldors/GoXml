// nativeexit exercises only the approved public single-window process API.
// It is built for coordinator acceptance; normal/grid/font-strict open a window
// and require explicit native-run authorization. invalid-options exits before a
// window. No event, failure, or exit injection is exposed by app.ProcessOptions.
package main

import (
	"encoding/json"
	"fmt"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"github.com/goxml/goxml/contracts"
	goxmlapp "github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/theme"
	"github.com/goxml/goxml/pkg/widgets"
	"os"
	"path/filepath"
)

type nativeView struct {
	grid              *widgets.DataGrid
	frames, maxScroll int
	clicked           []string
}

func (v *nativeView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	v.frames++
	if v.grid == nil {
		return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return material.Body1(th, "R27 single-window acceptance: resize, then close with the title-bar X.").Layout(gtx)
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body1(th, "R27 DataGrid: 10,000 rows. Scroll down, select rows, resize, return to top, close X.").Layout(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			d := v.grid.Layout(gtx, th)
			stats := v.grid.LastFrame()
			if stats.ScrollOffset > v.maxScroll {
				v.maxScroll = stats.ScrollOffset
			}
			if id, _, ok := v.grid.Clicked(); ok {
				v.clicked = append(v.clicked, id)
			}
			return d
		}),
	)
}

type rejectedEmbedded struct{}

func (rejectedEmbedded) Load() ([]byte, string, error) {
	return nil, "owned-rejected-fixture", contracts.ErrFontNotFound
}
func failingLazy() *theme.Lazy {
	provider := theme.NewProvider(theme.ProviderOptions{Candidates: []string{"owned-unavailable-font"}, Read: func(string) ([]byte, error) { return nil, os.ErrNotExist }, Embedded: rejectedEmbedded{}})
	lz, err := theme.NewLazy(theme.LazyOptions{Provider: provider, Temp: material.NewTheme()})
	if err != nil {
		panic(err)
	}
	return lz
}
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: nativeexit <new-evidence-dir> <normal|grid|font-strict|invalid-options>")
		os.Exit(64)
	}
	dir, scenario := os.Args[1], os.Args[2]
	// Each run owns a fresh evidence directory; an old PASS cannot be reused.
	if err := os.Mkdir(dir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64)
	}
	marker := filepath.Join(dir, "marker.txt")
	appendMarker := func(line string) {
		f, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		_, err = f.WriteString(line + "\n")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			panic(err)
		}
	}
	v := &nativeView{}
	source := "system-default"
	opts := goxmlapp.ProcessOptions{Observer: func(res goxmlapp.ExitResult) {
		appendMarker(fmt.Sprintf("observer:callback=%t cleanup=%t", res.CallbackDone, res.CleanupDone))
		data, err := json.Marshal(map[string]any{"scenario": scenario, "pid": os.Getpid(), "code": res.Code, "err": errText(res.Err), "destroy_err": errText(res.DestroyErr), "font_err": errText(res.FontErr), "callback_done": res.CallbackDone, "cleanup_done": res.CleanupDone, "font_source": source, "frames": v.frames, "max_scroll": v.maxScroll, "clicked_rows": v.clicked})
		if err != nil {
			panic(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "result.json"), data, 0600); err != nil {
			panic(err)
		}
		fmt.Fprintf(os.Stderr, "R27 result scenario=%s code=%d callback=%t cleanup=%t frames=%d err=%q\n", scenario, res.Code, res.CallbackDone, res.CleanupDone, v.frames, errText(res.Err))
	}}
	switch scenario {
	case "normal":
	case "grid":
		v.grid = widgets.NewDataGrid(unit.Dp(28))
		if err := v.grid.SetColumns([]widgets.DataGridColumn{{Title: "RowID", WidthDp: 180}, {Title: "Value", WidthDp: 240}, {Title: "Group", WidthDp: 180}}); err != nil {
			panic(err)
		}
		rows := make([]widgets.DataGridRow, 10000)
		for i := range rows {
			id := fmt.Sprintf("ROW-%05d", i)
			rows[i] = widgets.DataGridRow{ID: id, Cells: []string{id, fmt.Sprintf("value %d", i), fmt.Sprintf("group %d", i%7)}}
		}
		if r := v.grid.SetRows(rows); !r.OK {
			panic("grid fixture rejected")
		}
	case "font-strict":
		source = "explicit failing Provider fixture, not a system font failure"
		opts.Lazy = failingLazy()
	case "invalid-options":
		source = "mutually exclusive Theme/Lazy validation"
		opts.Theme = material.NewTheme()
		opts.Lazy = failingLazy()
	default:
		fmt.Fprintln(os.Stderr, "unknown scenario", scenario)
		os.Exit(64)
	}
	view, err := goxmlapp.WithOnDestroy(v, func() { appendMarker("callback") })
	if err != nil {
		panic(err)
	}
	fmt.Fprintf(os.Stderr, "R27 start pid=%d scenario=%s source=%s\n", os.Getpid(), scenario, source)
	goxmlapp.RunSingleWindowProcess(view, goxmlapp.Config{Title: "R27 native [" + scenario + "]", Width: 960, Height: 540, StrictFontFailure: scenario == "font-strict"}, opts)
	fmt.Fprintln(os.Stderr, "unreachable: process entry returned")
	os.Exit(70)
}
