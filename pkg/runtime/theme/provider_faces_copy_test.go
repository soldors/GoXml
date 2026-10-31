package theme

import (
	"gioui.org/widget/material"
	"testing"
	"time"

	"gioui.org/font"
)

// R2 切片3：报告/结果所有权——Provider.Faces 作为真实消费入口（Lazy 用它构建真主题）
// 必须返回切片副本，避免调用方就地改写污染 Provider 缓存、进而污染后续主题。
//
// 与 SelectedFaces.Faces() 已确立的"返回副本"契约保持一致（见 facesel.go / facesel_test.go）。
// 不深拷贝 FontFace 内部的字体对象（按契约共享只读）；只隔离切片本身。
func TestProviderFacesReturnsCopy(t *testing.T) {
	data := hanziFontBytes(t)
	p := NewProvider(ProviderOptions{
		Candidates: []string{"copy.otf"},
		Read:       func(string) ([]byte, error) { return data, nil },
	})
	id, rep, err := p.ResolveWithReport(FamilyCJK)
	if err != nil || id == "" || rep == nil {
		t.Fatalf("fixture failed: id=%q err=%v rep=%v", id, err, rep)
	}
	first, ok := p.Faces(id)
	if !ok || len(first) == 0 {
		t.Fatalf("faces empty for id %q", id)
	}
	if first[0].Face == nil {
		t.Fatal("fixture face is nil")
	}
	origWeight := first[0].Font.Weight

	// 不同次调用必须给不同底层数组（副本），不是同一块内部缓存。
	second, _ := p.Faces(id)
	if &first[0] == &second[0] {
		t.Fatal("两次 Provider.Faces 返回同一底层数组（未返回副本）")
	}

	// 就地改写返回切片元素：不得污染内部缓存。
	first[0] = font.FontFace{}
	again, ok := p.Faces(id)
	if !ok || len(again) == 0 {
		t.Fatalf("faces empty after mutation attempt for id %q", id)
	}
	if again[0].Face == nil {
		t.Fatal("就地改写 Provider.Faces 返回值污染了内部缓存（应返回副本）")
	}
	if again[0].Font.Weight != origWeight {
		t.Fatalf("face metadata changed after mutation attempt: weight %v -> %v", origWeight, again[0].Font.Weight)
	}
}

// Exercise the real asynchronous consumer after the caller overwrites its copy.
// A Ready result alone cannot prove glyph quality, but Take must return the theme
// constructed through Provider.Faces, rather than merely checking cache metadata.
func TestProviderFacesCopySurvivesLazyConsumer(t *testing.T) {
	data := hanziFontBytes(t)
	p := NewProvider(ProviderOptions{Candidates: []string{"lazy-copy.otf"}, Read: func(string) ([]byte, error) { return data, nil }})
	id, err := p.Resolve(FamilyCJK)
	if err != nil {
		t.Fatal(err)
	}
	faces, ok := p.Faces(id)
	if !ok || len(faces) == 0 {
		t.Fatal("fixture has no faces")
	}
	for i := range faces {
		faces[i] = font.FontFace{}
	}
	lz, err := NewLazy(LazyOptions{Provider: p, Temp: material.NewTheme()})
	if err != nil {
		t.Fatal(err)
	}
	lz.Request()
	select {
	case <-lz.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("lazy consumer did not complete")
	}
	if lz.State() != LoadReady {
		t.Fatalf("lazy state=%v err=%v", lz.State(), lz.Report().Err)
	}
	if th := lz.Take(); th == nil || th.Shaper == nil {
		t.Fatal("lazy consumer did not construct a real theme")
	}
}

func TestProviderFacesEmptyAndUnknown(t *testing.T) {
	p := NewProvider(ProviderOptions{})
	// No nil-vs-empty guarantee: known empty sets have ok=true and len=0.
	p.sets = map[string]*fontSet{"nil": {Faces: nil}, "empty": {Faces: []font.FontFace{}}}
	for _, id := range []string{"nil", "empty"} {
		faces, ok := p.Faces(id)
		if !ok || len(faces) != 0 {
			t.Fatalf("known empty %q: ok=%v len=%d", id, ok, len(faces))
		}
	}
	if faces, ok := p.Faces("unknown"); ok || faces != nil {
		t.Fatalf("unknown: ok=%v faces=%v", ok, faces)
	}
}
