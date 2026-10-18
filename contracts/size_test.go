package contracts

import "testing"

// ---------------------------------------------------------------------------
// SizeFill：语言层语义，不是某个具体布局调用的别名（规范 §3.1.5）
// ---------------------------------------------------------------------------

func TestSizeFillCarriesNoValue(t *testing.T) {
	f := FillSize()
	if f.Mode != SizeFill {
		t.Fatalf("FillSize().Mode = %v", f.Mode)
	}
	// Fill 只表达「占满父容器在对应方向上的可用空间」，
	// 不携带任何具体布局参数：轴向由父容器决定，不是 SizeSpec 的职责。
	// 若将来有人给 SizeSpec 加了 Flexed 之类的字段，这个断言会提醒更新规范。
	if f.Value != 0 {
		t.Errorf("FillSize() 不应携带数值，实际 %v", f.Value)
	}
}

func TestFixedSizePreservesDecimal(t *testing.T) {
	// 规范 §3.1.5 允许小数，精度为 float32。
	s := FixedSize(80.5)
	if s.Mode != SizeFixed || s.Value != 80.5 {
		t.Errorf("FixedSize(80.5) = %+v", s)
	}
}

func TestNaturalSizeIsNotSet(t *testing.T) {
	n := NaturalSize()
	if n.IsSet() {
		t.Error("NaturalSize 不应视为已设置")
	}
}
