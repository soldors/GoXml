package contracts

import (
	"math"
	"testing"
)

func TestGridColumnRejectsNonFinite(t *testing.T) {
	for _, mode := range []GridColumnMode{GridColumnFixed, GridColumnFlex} {
		for _, value := range []float32{float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN())} {
			if (GridColumn{Mode: mode, Value: value}).IsValid() {
				t.Errorf("mode=%v accepted non-finite value %v", mode, value)
			}
		}
	}
	for _, col := range []GridColumn{{Mode: GridColumnFixed, Value: 0}, {Mode: GridColumnFlex, Value: 0.5}, {Mode: GridColumnFlex, Value: math.MaxFloat32}} {
		if !col.IsValid() {
			t.Errorf("finite valid column rejected: %+v", col)
		}
	}
}
