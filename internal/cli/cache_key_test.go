package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// TASK-GENERATE-CACHE-001 切片 B1：缓存键与排除集（纯函数，不碰磁盘）
//
// 这些测试要能真实变红：
//   - 去掉 writeKeyField 的长度前缀 → TestCacheKeyFieldBoundariesGoRed 变红；
//   - 去掉 K3 排序 → TestCacheKeyPackageSourcesOrderInsensitive 变红；
//   - 只排除 manifestOutputs（漏掉 batchTargets）→
//     TestCacheKeyExclusionCoversBatchTargets 变红。
//
// 注意 `-run` 一律用单一前缀 `^TestCache`：写成 `TestCacheA\|TestCacheB`
// 在 PowerShell 里 `\|` 会被原样传给 Go 正则，而 RE2 的 `\|` 是**字面量竖线**，
// 结果是「0 个测试执行 + 退出码 0」的空转门禁。
// ============================================================================

// baseKeyInput 是各用例的基线输入；改一个字段就能验证「该字段参与键」。
func baseKeyInput() cacheKeyInput {
	return cacheKeyInput{
		SourceRel:       "ui/home.gxml",
		OutputRel:       "ui/home_gen.go",
		SourcePath:      "home.gxml",
		SourceHash:      strings.Repeat("a", 64),
		PackageName:     "ui",
		CompilerVersion: "1",
		PackageSources: []cachePackageSource{
			{Rel: "home.go", Hash: strings.Repeat("b", 64)},
			{Rel: "widgets.go", Hash: strings.Repeat("c", 64)},
		},
	}
}

func mustCacheKey(t *testing.T, in cacheKeyInput) string {
	t.Helper()
	k, err := cacheKey(in)
	if err != nil {
		t.Fatalf("cacheKey 失败: %v", err)
	}
	if len(k) != 64 {
		t.Fatalf("缓存键应为 64 位 hex，得到 %d 位: %q", len(k), k)
	}
	return k
}

// TestCacheKeyDeterministic：同一输入必须得到同一键（否则命中率恒为 0）。
func TestCacheKeyDeterministic(t *testing.T) {
	a := mustCacheKey(t, baseKeyInput())
	b := mustCacheKey(t, baseKeyInput())
	if a != b {
		t.Fatalf("同一输入算出不同键:\n a=%s\n b=%s", a, b)
	}
}

// TestCacheKeyEveryComponentMatters：逐字段改动必须改变键。
//
// 这张表就是契约 §11「缓存键至少包含六项」的机器可验证形式：
// 任何一项漏进键，对应行就会「键不变」而红。
func TestCacheKeyEveryComponentMatters(t *testing.T) {
	base := mustCacheKey(t, baseKeyInput())

	cases := []struct {
		name   string
		mutate func(*cacheKeyInput)
		why    string
	}{
		{"源内容", func(in *cacheKeyInput) { in.SourceHash = strings.Repeat("d", 64) }, "K1"},
		{"源路径", func(in *cacheKeyInput) { in.SourceRel = "ui/other.gxml" }, "K6"},
		{"输出路径", func(in *cacheKeyInput) { in.OutputRel = "gen/home_gen.go" }, "K6/--out-dir"},
		{"保护头 Source", func(in *cacheKeyInput) { in.SourcePath = "renamed.gxml" }, "K6"},
		{"包名", func(in *cacheKeyInput) { in.PackageName = "otherpkg" }, "K5"},
		{"生成器版本", func(in *cacheKeyInput) { in.CompilerVersion = "2" }, "K4"},
		{"包源码内容", func(in *cacheKeyInput) {
			in.PackageSources[0].Hash = strings.Repeat("e", 64)
		}, "K3"},
		{"包源码文件名", func(in *cacheKeyInput) {
			in.PackageSources[0].Rel = "renamed.go"
		}, "K3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseKeyInput()
			tc.mutate(&in)
			got := mustCacheKey(t, in)
			if got == base {
				t.Fatalf("%s（%s）变化后缓存键未变——该分量没有参与键", tc.name, tc.why)
			}
		})
	}
}

// TestCacheKeyPackageSourceCountMatters：增删包源码文件必须改变键。
//
// 与「内容变化」分开测：只比较内容拼接会把「多一个空文件」漏掉。
func TestCacheKeyPackageSourceCountMatters(t *testing.T) {
	base := mustCacheKey(t, baseKeyInput())

	in := baseKeyInput()
	in.PackageSources = append(in.PackageSources, cachePackageSource{
		Rel: "added.go", Hash: strings.Repeat("f", 64),
	})
	if got := mustCacheKey(t, in); got == base {
		t.Fatal("新增包源码文件后缓存键未变")
	}

	in = baseKeyInput()
	in.PackageSources = in.PackageSources[:1]
	if got := mustCacheKey(t, in); got == base {
		t.Fatal("删除包源码文件后缓存键未变")
	}
}

// TestCacheKeyPackageSourcesOrderInsensitive：K3 顺序不敏感。
//
// 理由：目录遍历顺序不保证稳定，若顺序进入键，命中会随文件系统抖动而丢失。
func TestCacheKeyPackageSourcesOrderInsensitive(t *testing.T) {
	a := mustCacheKey(t, baseKeyInput())

	in := baseKeyInput()
	in.PackageSources[0], in.PackageSources[1] = in.PackageSources[1], in.PackageSources[0]
	b := mustCacheKey(t, in)

	if a != b {
		t.Fatalf("K3 顺序影响了键（未排序）:\n a=%s\n b=%s", a, b)
	}
}

// TestCacheKeyFieldBoundariesGoRed：长度前缀消除「值吞掉下一个标签」的歧义。
//
// 只说「字段被串起来」还太弱：字段之间本来就有固定**标签**分隔，
// 所以 (SourceRel=ab, OutputRel=c) 与 (a, bc) 即使在无长度前缀时也不会碰撞
// ——这条断言曾经就是这么写的，实测**变异不红**（2026-10-20 自查）。
//
// 真正的歧义向量是：**某个值里嵌入了下一个标签的文本**。此时无长度前缀的
// 串会一模一样：
//
//	sourceRel + "a" + outputRel + "b" + outputRel + "c"   (值=aoutputRelb, 值=c)
//	sourceRel + "a" + outputRel + "b" + outputRel + "c"   (值=a,         值=boutputRelc)
//
// 有 4 字节长度前缀时，两个值被自定界，永不碰撞。
//
// 变异验证方式（2026-10-20 实测，两条都要记住）：
//   - **有效变异**：把 writeKeyField 改成只写 label+value（去掉全部长度字节）
//     → 本用例红，实测报「值内嵌入下一标签文本时算出同一键」。
//   - **无效变异**：把 `uint32(len(x))` 改成 `uint32(0)`——仍然写出 4 个零字节，
//     零字节本身就充当了分隔符，用例**照样绿**。别用它当证据，
//     否则会得出「长度前缀没用」的错误结论。
func TestCacheKeyFieldBoundariesGoRed(t *testing.T) {
	in1 := baseKeyInput()
	in1.SourceRel = "a" + "outputRel" + "b"
	in1.OutputRel = "c"

	in2 := baseKeyInput()
	in2.SourceRel = "a"
	in2.OutputRel = "b" + "outputRel" + "c"

	k1 := mustCacheKey(t, in1)
	k2 := mustCacheKey(t, in2)
	if k1 == k2 {
		t.Fatalf("值内嵌入下一标签文本时算出同一键 %s——字段没有长度自定界", k1)
	}
}

// TestCacheKeyRejectsAbsolutePaths：绝对路径不得入键。
//
// 若放行，同一项目在不同磁盘位置（或 CI 与本地）永远不命中；
// 这种失效很难与「输入变了」区分，所以直接报错。
func TestCacheKeyRejectsAbsolutePaths(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "home.gxml")

	cases := []struct {
		name   string
		mutate func(*cacheKeyInput)
	}{
		{"SourceRel 绝对路径", func(in *cacheKeyInput) { in.SourceRel = abs }},
		{"OutputRel 绝对路径", func(in *cacheKeyInput) { in.OutputRel = abs }},
		{"SourcePath 盘符路径", func(in *cacheKeyInput) { in.SourcePath = `C:\ui\home.gxml` }},
		{"SourceRel 含 ..", func(in *cacheKeyInput) { in.SourceRel = "../outside.gxml" }},
		{"包源码 Rel 含 ..", func(in *cacheKeyInput) { in.PackageSources[0].Rel = "..\\x.go" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseKeyInput()
			tc.mutate(&in)
			if _, err := cacheKey(in); err == nil {
				t.Fatal("应拒绝非法路径，却算出了键")
			}
		})
	}
}

// TestCacheKeyRejectsEmptyRequiredFields：缺分量必须报错而非静默算出键。
//
// 空 SourceHash 会让**所有源**共用同一键（最危险的假命中），
// 空 PackageName 会跨包命中，空 CompilerVersion 会让生成逻辑变化后仍命中。
func TestCacheKeyRejectsEmptyRequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*cacheKeyInput)
	}{
		{"SourceHash", func(in *cacheKeyInput) { in.SourceHash = "" }},
		{"PackageName", func(in *cacheKeyInput) { in.PackageName = "" }},
		{"CompilerVersion", func(in *cacheKeyInput) { in.CompilerVersion = "" }},
		{"SourceRel", func(in *cacheKeyInput) { in.SourceRel = "" }},
		{"包源码 Hash", func(in *cacheKeyInput) { in.PackageSources[0].Hash = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := baseKeyInput()
			tc.mutate(&in)
			if _, err := cacheKey(in); err == nil {
				t.Fatalf("%s 为空时应报错，却算出了键", tc.name)
			}
		})
	}
}

// TestCacheKeyExclusionCoversBatchTargets 是陷阱 T1 的机器可验证形式。
//
// 排除集必须同时覆盖「Manifest 已登记产物」与「本批计划 target」。
// 只覆盖前者时，同包多文件场景会永远不命中（本批刚写出的产物尚未登记，
// 却被算进了 K3）。这里断言两个来源都在集合内，且路径已规范化。
func TestCacheKeyExclusionCoversBatchTargets(t *testing.T) {
	manifestOutputs := []string{
		filepath.Join("ui", "home_gen.go"), // 用分隔符构造，验证 Clean/统一
	}
	batchTargets := []string{
		filepath.Join("ui", "list_gen.go"),
	}
	ex := fingerprintExclusion(manifestOutputs, batchTargets)

	for _, want := range []string{
		filepath.Clean(filepath.Join("ui", "home_gen.go")),
		filepath.Clean(filepath.Join("ui", "list_gen.go")),
	} {
		if !ex[want] {
			t.Fatalf("排除集缺少 %q（T1：会导致命中率恒为 0）; 实际=%v", want, ex)
		}
	}
	if len(ex) != 2 {
		t.Fatalf("排除集应去重后为 2 条，实际 %d: %v", len(ex), ex)
	}
}

// TestCacheKeyExclusionIgnoresEmpty：空串不得进入排除集（否则 Clean("") == "."，
// 可能意外排除整个目录）。
func TestCacheKeyExclusionIgnoresEmpty(t *testing.T) {
	ex := fingerprintExclusion([]string{""}, nil)
	if len(ex) != 0 {
		t.Fatalf("空路径不应进入排除集，实际: %v", ex)
	}
}

// TestCacheKeyExclusionNilSafe：两个来源都为空时返回空集，不 panic。
func TestCacheKeyExclusionNilSafe(t *testing.T) {
	if ex := fingerprintExclusion(nil, nil); len(ex) != 0 {
		t.Fatalf("nil 输入应得到空集，实际: %v", ex)
	}
}
