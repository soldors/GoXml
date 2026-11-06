package watch

// R26-A A1 判别回归：监听快照 + 100ms 防抖状态机 + owned 自激防护 + 末代获胜/取消。
// 全部确定性（注入时钟、fake builder/swapper），无真实 goroutine/sleep/窗口。

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeBuilder 记录真实调用代次/路径；按脚本返回结果。
type fakeBuilder struct {
	calls   []int      // 每次 Build 的代次
	paths   [][]string // 每次 Build 的触发路径
	results map[int]GenerationResult
	def     GenerationResult
}

func (b *fakeBuilder) Build(_ context.Context, gen int, changed []string) GenerationResult {
	b.calls = append(b.calls, gen)
	b.paths = append(b.paths, append([]string(nil), changed...))
	if r, ok := b.results[gen]; ok {
		return r
	}
	if b.def.Gen == 0 {
		b.def.Gen = gen
	}
	return GenerationResult{OK: true, Gen: gen}
}

// fakeSwapper 记录交接代次。
type fakeSwapper struct{ swapped []int }

func (s *fakeSwapper) Swap(_ context.Context, gen int) error {
	s.swapped = append(s.swapped, gen)
	return nil
}

type recordObs struct {
	gens      []int
	published []bool
}

func (o *recordObs) OnGeneration(gen int, _ GenerationResult, published bool) {
	o.gens = append(o.gens, gen)
	o.published = append(o.published, published)
}

func mkProj(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/demo\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(dir, "ui", "home.gxml"), `<Goxml Version="1"><Page Route="/"><Text Value="x"/></Page></Goxml>`)
	mustWrite(t, filepath.Join(dir, "ui", "vm.go"), "package ui\n")
	return dir
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 1) 快照确定性：同目录两次 Scan 结果一致；Diff 稳定排序。
func TestScanDeterministic(t *testing.T) {
	dir := mkProj(t)
	s1, err := Scan(Config{ProjectDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := Scan(Config{ProjectDir: dir})
	if !reflect.DeepEqual(s1, s2) {
		t.Fatal("两次 Scan 必须一致（确定性）")
	}
	// 变更一个文件 -> Diff 稳定有序
	mustWrite(t, filepath.Join(dir, "ui", "vm.go"), "package ui // changed\n")
	s3, _ := Scan(Config{ProjectDir: dir})
	ch := Diff(s1, s3)
	if len(ch) != 1 || ch[0] != "ui/vm.go" {
		t.Fatalf("Diff 应只报 ui/vm.go，得到 %v", ch)
	}
}

// 2) .git / .goxml 排除；ExcludeDirs 排除自有构建输出。
func TestScanExcludes(t *testing.T) {
	dir := mkProj(t)
	mustWrite(t, filepath.Join(dir, ".git", "HEAD"), "ref: x\n")
	mustWrite(t, filepath.Join(dir, ".goxml", "generate-manifest.json"), `{"version":1,"entries":[]}`)
	mustWrite(t, filepath.Join(dir, "build", "app.exe"), "BIN")
	s, err := Scan(Config{ProjectDir: dir, ExcludeDirs: []string{"build"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s[".git/HEAD"]; ok {
		t.Fatal(".git 必须排除")
	}
	if _, ok := s[".goxml/generate-manifest.json"]; ok {
		t.Fatal(".goxml 必须排除")
	}
	if _, ok := s["build/app.exe"]; ok {
		t.Fatal("ExcludeDirs 必须排除自有构建输出")
	}
}

// 3) 100ms 防抖：突发多次变更合并为一次构建；静默期满才触发。
func TestDebounceBurstCoalesced(t *testing.T) {
	dir := mkProj(t)
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, err := New(Config{ProjectDir: dir}, fb, fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// t=0,10,20ms 连续变更（同窗口内）
	for _, ms := range []int{0, 10, 20} {
		if _, err := w.NotifyChange(ctx, ms, []string{"ui/home.gxml"}); err != nil {
			t.Fatal(err)
		}
	}
	// t=50ms 未到 deadline(20+100=120) -> 不构建
	if did, _ := w.Tick(ctx, 50); did {
		t.Fatal("未到防抖窗口不得构建")
	}
	// t=119 仍不构建；t=120 到期 -> 一次构建
	if did, _ := w.Tick(ctx, 119); did {
		t.Fatal("119ms(<deadline) 不得构建")
	}
	did, _ := w.Tick(ctx, 120)
	if !did {
		t.Fatal("120ms 到期应触发一次构建")
	}
	if len(fb.calls) != 1 {
		t.Fatalf("突发变更应合并为 1 次构建，实际 %d", len(fb.calls))
	}
	// 再次 Tick 同代次不应重复构建
	if did, _ := w.Tick(ctx, 130); did {
		t.Fatal("无新变更不得再次构建")
	}
	if len(fb.calls) != 1 {
		t.Fatalf("不得重复构建，实际 %d", len(fb.calls))
	}
}

// 4) owned 变更（Manifest 登记 ∧ 保护头匹配）不触发重建。
func TestOwnedChangeDoesNotTrigger(t *testing.T) {
	dir := mkProj(t)
	// 构造 owned 产物 + Manifest
	gen := "// Goxml-Version: 1\n// Source: home.gxml\n\npackage ui\n"
	mustWrite(t, filepath.Join(dir, "ui", "home_gen.go"), gen)
	mustWrite(t, filepath.Join(dir, ".goxml", "generate-manifest.json"),
		`{"version":1,"entries":[{"source":"ui/home.gxml","output":"ui/home_gen.go"}]}`)
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: dir}, fb, fs, nil)
	ctx := context.Background()
	cs, err := w.NotifyChange(ctx, 0, []string{"ui/home_gen.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Trigger) != 0 || len(cs.Owned) != 1 {
		t.Fatalf("owned 生成物应分类为 Owned 不触发: trigger=%v owned=%v", cs.Trigger, cs.Owned)
	}
	// 到窗口也不构建
	if did, _ := w.Tick(ctx, 200); did {
		t.Fatal("owned 变更不得触发构建（防自激循环）")
	}
	if len(fb.calls) != 0 {
		t.Fatalf("owned 不应产生构建调用，实际 %d", len(fb.calls))
	}
}

// 5) 同名但**无所有权**的用户 Go（无保护头）仍触发。
func TestUnownedSameNameGoTriggers(t *testing.T) {
	dir := mkProj(t)
	// 用户文件，Manifest 未登记
	mustWrite(t, filepath.Join(dir, "ui", "user_gen.go"), "package ui // user owned, no guard\n")
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: dir}, fb, fs, nil)
	ctx := context.Background()
	cs, _ := w.NotifyChange(ctx, 0, []string{"ui/user_gen.go"})
	if len(cs.Trigger) != 1 {
		t.Fatalf("无所有权用户 Go 必须触发，trigger=%v", cs.Trigger)
	}
	if did, _ := w.Tick(ctx, 120); !did || len(fb.calls) != 1 {
		t.Fatal("用户 Go 变更应触发一次构建")
	}
}

// 6) 构建中再次变更：旧代不发布（final-wins），重排最新代次。
func TestChangeDuringBuildFinalWins(t *testing.T) {
	dir := mkProj(t)
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: dir}, fb, fs, nil)
	ctx := context.Background()
	w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	// t=120 触发 gen1 构建
	if did, _ := w.Tick(ctx, 120); !did {
		t.Fatal("应触发 gen1 构建")
	}
	if w.LastPublishedGen() != 1 {
		t.Fatalf("gen1 成功后应发布，实际 %d", w.LastPublishedGen())
	}
	// 新变更 -> gen2 待定；t=220 到期，但让 builder 在 gen2 期间“又有变更”
	w.NotifyChange(ctx, 130, []string{"ui/vm.go"})
	// 在 gen2 构建前再登记一次变更（模拟构建中变更）: 用 Note 直接推进 latestGen
	w.Debouncer().Note(140, "ui/home.gxml") // 使 latestGen=3 且 deadline 推后
	// t=250 仍 < 140+100=240? 250>=240 到期 -> gen? Pump 取 latestGen=3
	did, _ := w.Tick(ctx, 250)
	if !did {
		t.Fatal("应触发构建（最新代次）")
	}
	// 此刻 builder 收到的是 gen3（最新）；gen2 从未单独构建（被合并）
	gotGen := fb.calls[len(fb.calls)-1]
	if gotGen != 3 {
		t.Fatalf("应按最新代次 gen3 构建，实际 %d（calls=%v）", gotGen, fb.calls)
	}
}

// 7) 取消进行中的构建：不发布。
func TestCancelDoesNotPublish(t *testing.T) {
	dir := mkProj(t)
	fb := &fakeBuilder{}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: dir}, fb, fs, nil)
	ctx := context.Background()
	w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	// 手动进入构建（Pump 但用 fake builder 无法打断；改为驱动状态机层面）
	gen := w.Debouncer().Pump(120)
	if gen == 0 {
		t.Fatal("应进入构建")
	}
	// 取消 -> BuildFinished(gen, true) 不发布
	w.Debouncer().Cancel()
	publish, _ := w.Debouncer().BuildFinished(gen, true)
	if publish {
		t.Fatal("取消后不得发布")
	}
}

// 8) 构建失败：不交接、不发布。
func TestBuildFailureNoPublish(t *testing.T) {
	dir := mkProj(t)
	fb := &fakeBuilder{results: map[int]GenerationResult{1: {OK: false, Stage: "generate", Detail: "bad gxml", Gen: 1}}}
	fs := &fakeSwapper{}
	w, _ := New(Config{ProjectDir: dir}, fb, fs, nil)
	ctx := context.Background()
	w.NotifyChange(ctx, 0, []string{"ui/home.gxml"})
	w.Tick(ctx, 120)
	if len(fs.swapped) != 0 {
		t.Fatal("构建失败不得交接新进程")
	}
	if w.LastPublishedGen() != 0 {
		t.Fatal("构建失败不得更新已发布代次")
	}
}
