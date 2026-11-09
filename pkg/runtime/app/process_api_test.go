package app

import (
	"reflect"
	"strings"
	"testing"

	"gioui.org/io/event"
	"gioui.org/widget/material"
)

func TestProcessOptionsExportsProductionFieldsOnly(t *testing.T) {
	public := map[string]bool{"Theme": true, "Lazy": true, "Observer": true}
	typ := reflect.TypeOf(ProcessOptions{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.IsExported() && !public[f.Name] {
			t.Errorf("self-test field leaked into public ProcessOptions: %s", f.Name)
		}
	}
	if f, ok := reflect.TypeOf(ExitResult{}).FieldByName("Steps"); ok && f.IsExported() {
		t.Fatal("internal diagnostic steps became a public contract")
	}
}

func TestProcessRejectsAmbiguousThemeBeforeWindow(t *testing.T) {
	lz := newFailingLazy(t, material.NewTheme())
	var got ExitResult
	mainCalls := 0
	old := mainFunc
	mainFunc = func() { mainCalls++ }
	t.Cleanup(func() { mainFunc = old })
	RunSingleWindowProcess(&countingView{}, Config{}, ProcessOptions{
		Theme: material.NewTheme(), Lazy: lz,
		next:     func() event.Event { panic("unreachable") },
		Observer: func(r ExitResult) { got = r }, exit: func(int) {},
	})
	if got.Code != 1 || got.Err == nil || !strings.Contains(got.Err.Error(), "mutually exclusive") {
		t.Fatalf("ambiguous theme was not rejected: %+v", got)
	}
	if got.CallbackDone || got.CleanupDone || mainCalls != 0 {
		t.Fatalf("ambiguous options entered window path: result=%+v main=%d", got, mainCalls)
	}
}
