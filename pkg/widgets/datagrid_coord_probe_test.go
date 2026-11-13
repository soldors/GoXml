package widgets

import (
	"testing"

	"gioui.org/io/key"
)

func TestCoordR29ShiftNavigationRequiresMultiSelect(t *testing.T) {
	grid, h := kbSetupFocused(t, 100)
	kbKey(h, key.NameDownArrow)
	first, ok := grid.SelectedRowID()
	if !ok {
		t.Fatal("plain Down did not select a row")
	}
	h.frame(key.Event{Name: key.NameDownArrow, State: key.Press, Modifiers: key.ModShift})
	after, _ := grid.SelectedRowID()
	if after != first {
		t.Fatalf("Shift+Down moved selection with multi-select disabled: %q → %q", first, after)
	}
	kbKey(h, key.NameDownArrow)
	if afterPlain, _ := grid.SelectedRowID(); afterPlain == after {
		t.Fatal("plain Down must still navigate")
	}
}

func TestCoordR29MultiSelectPlainBoundaryNavigationCollapses(t *testing.T) {
	grid, h := msSetup(t, 8)
	grid.EnableKeyboardNav(DataGridKeyboardConfig{})
	grid.EnableMultiSelect()
	h.frame()
	msCtrlClick(h, 12, msRowY(2))
	msCtrlClick(h, 12, msRowY(0))
	lead, ok := grid.SelectedRowID()
	if !ok || len(grid.SelectedRowIDs()) != 2 {
		t.Fatalf("expected two selected rows before Up; lead=%q count=%d", lead, len(grid.SelectedRowIDs()))
	}
	kbKey(h, key.NameUpArrow)
	if after := grid.SelectedRowIDs(); len(after) != 1 || after[0] != lead {
		t.Fatalf("unmodified Up at top did not collapse to the lead %q: %v", lead, after)
	}
}
