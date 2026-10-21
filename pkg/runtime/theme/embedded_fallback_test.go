package theme

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/font/opentype"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/goxml/goxml/contracts"
	embedasset "github.com/goxml/goxml/pkg/runtime/theme/embedded"
)

// ============================================================================
// TASK-FONT-001-B / B3：嵌入字体的落地、元数据与解析
//
// 证据等级：**逻辑**（读仓库内产物，任何平台可跑）。
//
// 冻结值说明：下面的 SHA-256 是**产物指纹**，与 METADATA.json 互为校验。
// 字体重新子集化时这些常量必须同步更新——故意写死，就是为了让"悄悄换了字体"
// 变成一次失败的测试，而不是一次无声的替换。
// ============================================================================

const (
	// 子集产物（GoxmlSansSC-Regular.otf）的冻结指纹。
	wantSubsetSHA256 = "652910bee7fb0facfd9333b173c5ea7819eeadea8950592cf3f748bcb158e507"
	wantSubsetBytes  = 1625796

	// 上游原始文件（官方 NotoSansSC-Regular.otf）的冻结指纹与来源。
	wantSourceSHA256 = "faa6c9df652116dde789d351359f3d7e5d2285a2b2a1f04a2d7244df706d5ea9"
	wantSourceCommit = "523d033d6cb47f4a80c58a35753646f5c3608a78"

	// 许可证全文（OFL 1.1）的冻结指纹。
	wantLicenseSHA256 = "6a73f9541c2de74158c0e7cf6b0a58ef774f5a780bf191f2d7ec9cc53efe2bf2"

	// 字符集规模（B 卡 §2.2 冻结：GB2312 6,763 汉字 + ASCII 95）。
	wantCodepoints = 7540
	wantHanzi      = 6763
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// loadEmbedded 读取嵌入资产，失败即 Fatal（用例前提）。
func loadEmbedded(t *testing.T) embedasset.Assets {
	t.Helper()
	if !embedasset.Available() {
		t.Fatal("本次构建没有嵌入字体（nofontembed?）——B 的验收不允许在此前提下通过")
	}
	a, err := embedasset.Load()
	if err != nil {
		t.Fatalf("加载嵌入资产失败: %v", err)
	}
	if len(a.Font) == 0 {
		t.Fatal("嵌入字体字节为空")
	}
	return a
}

// TestEmbeddedFallbackMetadata B1：来源 / 版本 / 许可证全文 / 改名后字体名 /
// 两套 SHA-256 可断言（B 卡 §8）。
func TestEmbeddedFallbackMetadata(t *testing.T) {
	a := loadEmbedded(t)
	m := a.Metadata

	// --- 改名结果（维护者已确认 Goxml Sans SC / GoxmlSansSC-Regular）
	// 先锚定包常量与确认名一致，再让元数据与常量比对：两次断言各管一件事，
	// 避免写成 `a != x || a != y` 这种自相矛盾的条件（vet 会报 suspect or）。
	if embedasset.FamilyName != "Goxml Sans SC" {
		t.Errorf("包常量 FamilyName 与确认名不符: %q", embedasset.FamilyName)
	}
	if embedasset.PostScriptName != "GoxmlSansSC-Regular" {
		t.Errorf("包常量 PostScriptName 与确认名不符: %q", embedasset.PostScriptName)
	}
	if m.FamilyName != embedasset.FamilyName {
		t.Errorf("familyName 不符: %q != %q", m.FamilyName, embedasset.FamilyName)
	}
	if m.PostScriptName != embedasset.PostScriptName {
		t.Errorf("postScriptName 不符: %q != %q", m.PostScriptName, embedasset.PostScriptName)
	}
	if strings.Contains(strings.ToLower(m.FamilyName), "noto") {
		t.Error("改名字体名中仍含上游品牌名 Noto（nameID7 声明 Noto 是 Google 商标）")
	}

	// --- 上游来源（B1 的可追溯锚点）
	if m.Upstream.Repository != "https://github.com/notofonts/noto-cjk" {
		t.Errorf("上游仓库不符: %q", m.Upstream.Repository)
	}
	if m.Upstream.Tag != "Sans2.004" {
		t.Errorf("上游 tag 不符: %q", m.Upstream.Tag)
	}
	if m.Upstream.Commit != wantSourceCommit {
		t.Errorf("上游 commit 不符: %q", m.Upstream.Commit)
	}
	if m.Upstream.Path != "Sans/SubsetOTF/SC/NotoSansSC-Regular.otf" {
		t.Errorf("上游路径不符: %q", m.Upstream.Path)
	}
	if m.Source.SHA256 != wantSourceSHA256 {
		t.Errorf("上游原始文件 SHA-256 不符: %q", m.Source.SHA256)
	}

	// --- 两级 SHA-256：元数据 vs 实际字节（内容锚定）
	if got := sha256Hex(a.Font); got != m.Subset.SHA256 {
		t.Errorf("字体实际 SHA-256 与元数据不符:\n  got  %s\n  want %s", got, m.Subset.SHA256)
	}
	if got := sha256Hex(a.Font); got != wantSubsetSHA256 {
		t.Errorf("字体 SHA-256 与冻结值不符（产物变了？需同步更新常量与任务卡）:\n  got  %s\n  want %s",
			got, wantSubsetSHA256)
	}
	if int64(len(a.Font)) != m.Subset.Bytes || len(a.Font) != wantSubsetBytes {
		t.Errorf("字体字节数不符: actual=%d meta=%d frozen=%d",
			len(a.Font), m.Subset.Bytes, wantSubsetBytes)
	}
	if m.Subset.File != embedasset.PostScriptName+".otf" {
		t.Errorf("元数据里的文件名不想符: %q", m.Subset.File)
	}

	// --- 许可证全文（OFL 要求随附，不能只写一个 "OFL 1.1" 字样）
	if got := sha256Hex(a.License); got != m.License.SHA256 {
		t.Errorf("许可证 SHA-256 与元数据不符: %s != %s", got, m.License.SHA256)
	}
	if got := sha256Hex(a.License); got != wantLicenseSHA256 {
		t.Errorf("许可证 SHA-256 与冻结值不符: %s", got)
	}
	lic := string(a.License)
	for _, want := range []string{
		"SIL OPEN FONT LICENSE Version 1.1 - 26 February 2007",
		"PERMISSION & CONDITIONS",
		"TERMINATION",
		"DISCLAIMER",
	} {
		if !strings.Contains(lic, want) {
			t.Errorf("许可证全文缺少必备段落 %q（不能只写许可证名称）", want)
		}
	}
	if m.License.Name != "SIL Open Font License 1.1" {
		t.Errorf("许可证名称不符: %q", m.License.Name)
	}

	// --- 生成工具可复现性
	if m.Tool.Name != "fontTools.pyftsubset" || m.Tool.Version != "4.67.0" {
		t.Errorf("工具记录不符: %+v", m.Tool)
	}
	joined := strings.Join(m.Tool.Args, " ")
	if !strings.Contains(joined, "--no-recalc-timestamp") {
		t.Error("子集参数缺少 --no-recalc-timestamp：产物将逐次不同，哈希失去比对意义")
	}

	// --- 覆盖边界必须如实标注（不得描述为完整 CJK）
	if m.Coverage.CompleteCJK {
		t.Error("coverage.completeCJK 必须为 false：本字体不是完整 CJK 覆盖")
	}

	// --- 字体内部 name 表：Gio 读出来的 typeface 必须就是改名后的名字
	faces, err := opentype.ParseCollection(a.Font)
	if err != nil {
		t.Fatalf("嵌入字体无法被 Gio 解析: %v", err)
	}
	if len(faces) == 0 {
		t.Fatal("解析结果为空集合")
	}
	if got := string(faces[0].Font.Typeface); got != embedasset.FamilyName {
		t.Errorf("字体 name 表里的族名与元数据不符: %q != %q", got, embedasset.FamilyName)
	}
}

// TestEmbeddedFallbackSubsetCoverage B2：清单内的字**逐个**有字形；
// 并断言存在清单外 CJK 码位无字形（如实标注边界，不伪造"完整 CJK"）。
func TestEmbeddedFallbackSubsetCoverage(t *testing.T) {
	a := loadEmbedded(t)

	// --- 字符集清单本身：规模与哈希都可核对
	if got := sha256Hex(a.Charset); got != a.Metadata.Charset.SHA256 {
		t.Errorf("字符集清单 SHA-256 与元数据不符: %s != %s", got, a.Metadata.Charset.SHA256)
	}
	cps := parseCharset(t, a.Charset)
	if len(cps) != wantCodepoints || a.Metadata.Charset.Codepoints != wantCodepoints {
		t.Fatalf("字符集规模不符: actual=%d meta=%d frozen=%d",
			len(cps), a.Metadata.Charset.Codepoints, wantCodepoints)
	}
	hanzi := 0
	for _, cp := range cps {
		if cp >= 0x4E00 && cp <= 0x9FFF {
			hanzi++
		}
	}
	if hanzi != wantHanzi || a.Metadata.Charset.Hanzi != wantHanzi {
		t.Errorf("汉字数不符: actual=%d meta=%d frozen=%d", hanzi, a.Metadata.Charset.Hanzi, wantHanzi)
	}

	// --- 逐项覆盖：清单里每一个码位都必须有真实字形（GID 0 = notdef，不算数）
	faces, err := opentype.ParseCollection(a.Font)
	if err != nil {
		t.Fatalf("解析嵌入字体失败: %v", err)
	}
	face := faces[0].Face.Face()
	hasGlyph := func(r rune) bool {
		gid, ok := face.NominalGlyph(r)
		return ok && gid != 0
	}
	if missing := missingGlyphs(hasGlyph, cps, 16); len(missing) > 0 {
		t.Errorf("清单内 %d 个码位缺字形（示例 %U）——覆盖清单不可信", len(missing), missing)
	}

	// 反向自检：把清单外码位混进来，同一个判定必须报缺失。
	// 没有这条，上面那句"零缺失"可能只是因为检测函数恒返回空。
	t.Run("反向自检：判定器必须能发现缺字形", func(t *testing.T) {
		bogus := []rune{0x9FA6, 0x20000}
		if got := missingGlyphs(hasGlyph, bogus, 16); len(got) != 2 {
			t.Fatalf("判定器对清单外码位未报缺失（got=%U）——覆盖断言是空的", got)
		}
	})

	// --- 边界：清单外必须无字形，否则"不是完整 CJK"这句声明就是错的
	for _, cp := range []rune{0x20000, 0x2A700, 0x3400, 0x9FA6, 0xE000} {
		if gid, ok := face.NominalGlyph(cp); ok && gid != 0 {
			t.Errorf("清单外码位 %U 竟有字形（gid=%d）：覆盖边界标注与产物不符", cp, gid)
		}
	}

	// --- 抽查常见字，避免"清单能解析但字体是别的字"
	for _, r := range []rune{'中', '文', '汉', '字', 'A', 'a', '0', '，', '。'} {
		if gid, ok := face.NominalGlyph(r); !ok || gid == 0 {
			t.Errorf("常用字 %q 无字形", r)
		}
	}
}

// TestEmbeddedFallbackParseAndCorrupted B3：有效字体解析成功且含「中」；
// 损坏字节 → 可诊断错误（非 panic、非空集合）。
func TestEmbeddedFallbackParseAndCorrupted(t *testing.T) {
	a := loadEmbedded(t)

	faces, err := opentype.ParseCollection(a.Font)
	if err != nil {
		t.Fatalf("有效字体应解析成功: %v", err)
	}
	found := false
	for _, ff := range faces {
		if gid, ok := ff.Face.Face().NominalGlyph('中'); ok && gid != 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("解析结果不含「中」字形")
	}

	t.Run("损坏字节必须可诊断", func(t *testing.T) {
		for name, data := range map[string][]byte{
			"非字体": []byte("this is definitely not a font file"),
			"截断":  a.Font[:2048],
			"空":   {},
		} {
			got, err := opentype.ParseCollection(data)
			if err == nil {
				t.Errorf("%s: 期望返回错误，实际成功且 faces=%d", name, len(got))
				continue
			}
			if len(got) != 0 {
				t.Errorf("%s: 失败时不得返回非空集合（伪装成功），实际 %d", name, len(got))
			}
			if strings.TrimSpace(err.Error()) == "" {
				t.Errorf("%s: 错误信息为空——诊断必须能回答原因", name)
			}
			t.Logf("%s → %v", name, err)
		}
	})
}

// missingGlyphs 返回 cps 中无字形的码位（最多 limit 个）。
//
// 用「谓词 + 码位表」而不是直接收字体对象，是为了能被反向自检复用，
// 同时不必把 go-text 的 GID 类型泄漏到测试签名里。
// 判定器本身必须可被证伪，否则"零缺失"可能只是它恒返回空切片。
func missingGlyphs(has func(rune) bool, cps []rune, limit int) []rune {
	var out []rune
	for _, cp := range cps {
		if !has(cp) {
			out = append(out, cp)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// parseCharset 解析 charset.txt（一行一个 U+XXXX）。
func parseCharset(t *testing.T, data []byte) []rune {
	t.Helper()
	var out []rune
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "U+") {
			t.Fatalf("charset.txt 行格式非法: %q（应为 U+XXXX）", line)
		}
		var cp int64
		for _, c := range line[2:] {
			var d int64
			switch {
			case c >= '0' && c <= '9':
				d = int64(c - '0')
			case c >= 'A' && c <= 'F':
				d = int64(c-'A') + 10
			case c >= 'a' && c <= 'f':
				d = int64(c-'a') + 10
			default:
				t.Fatalf("charset.txt 非十六进制: %q", line)
			}
			cp = cp*16 + d
		}
		out = append(out, rune(cp))
	}
	return out
}

// embeddedDir 返回仓库内嵌入产物目录（供其它用例定位文件做磁盘级比对）。
func embeddedDir(t *testing.T) string {
	t.Helper()
	// 本文件所在包目录 = pkg/runtime/theme；产物在其子目录 embedded。
	dir := filepath.Join("embedded")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("找不到嵌入产物目录 %s: %v", dir, err)
	}
	return dir
}

// ============================================================================
// B4：系统优先、嵌入兜底
//
// 关键可观测点不是"兜底能用"，而是**什么时候不该用**：
// 系统命中时若还去解析嵌入字体，就是白付一次解析成本。
// 这一点只能用计数器证明——所以嵌入加载必须经过注入点。
// ============================================================================

// countingLoader 记录嵌入加载被调用的次数。
type countingLoader struct {
	calls int
	data  []byte
	err   error
}

func (c *countingLoader) Load() ([]byte, string, error) {
	c.calls++
	if c.err != nil {
		return nil, "", c.err
	}
	return c.data, embedasset.PostScriptName, nil
}

// TestEmbeddedFallbackSelectionOrder B4：系统失败→选嵌入（前缀 embedded:）；
// 系统命中→嵌入解析**未被调用**。
func TestEmbeddedFallbackSelectionOrder(t *testing.T) {
	a := loadEmbedded(t)

	t.Run("系统命中时不得解析嵌入字体", func(t *testing.T) {
		good := writeFont(t, t.TempDir(), "good.ttf", goregular.TTF)
		cl := &countingLoader{data: a.Font}

		p := NewProvider(ProviderOptions{
			Candidates: []string{good},
			Timeout:    time.Second,
			Embedded:   cl,
		})
		id, rep, err := p.ResolveWithReport(FamilyCJK)
		if err != nil {
			t.Fatalf("系统命中时 Resolve 不应失败: %v", err)
		}
		if cl.calls != 0 {
			t.Errorf("系统命中却调用了嵌入加载 %d 次——白付一次解析成本", cl.calls)
		}
		if !strings.HasPrefix(id, "system:") {
			t.Errorf("系统命中时标识应以 system: 开头，实际 %q", id)
		}
		if rep.Embedded != nil && rep.Embedded.Attempted {
			t.Errorf("系统命中时报告不应记录嵌入已尝试: %v", rep.Embedded)
		}
	})

	t.Run("系统全部失败时选嵌入并保留两条链路诊断", func(t *testing.T) {
		cl := &countingLoader{data: a.Font}
		p := NewProvider(ProviderOptions{
			Candidates: []string{
				filepath.Join(t.TempDir(), "a.ttc"),
				filepath.Join(t.TempDir(), "b.ttc"),
			},
			Timeout:  time.Second,
			Embedded: cl,
		})
		id, rep, err := p.ResolveWithReport(FamilyCJK)
		if err != nil {
			t.Fatalf("系统失败后应兜底成功，实际: %v\n诊断:\n%s", err, rep)
		}
		if cl.calls != 1 {
			t.Errorf("嵌入加载应恰好调用 1 次，实际 %d", cl.calls)
		}
		if !strings.HasPrefix(id, "embedded:") {
			t.Errorf("兜底命中时标识应以 embedded: 开头，实际 %q", id)
		}
		if rep.Err != nil {
			t.Errorf("兜底成功后报告 Err 必须清空（否则会被读成失败），实际 %v", rep.Err)
		}
		if len(rep.Candidates) != 2 {
			t.Errorf("系统链路的失败必须保留在诊断里，实际 %d 条", len(rep.Candidates))
		}
		if rep.Embedded == nil || !rep.Embedded.Attempted {
			t.Fatalf("报告必须记录嵌入链路已尝试: %v", rep.Embedded)
		}
		if rep.Embedded.Faces == 0 || rep.Embedded.Bytes != len(a.Font) {
			t.Errorf("嵌入诊断数据不完整: %v", rep.Embedded)
		}
		if !strings.Contains(rep.String(), "embedded attempted") {
			t.Errorf("报告文本必须暴露嵌入链路:\n%s", rep)
		}

		// 标识必须能反查到真实集合（评审要点 1 对嵌入路径同样成立）
		faces, ok := p.Faces(id)
		if !ok || len(faces) == 0 {
			t.Fatalf("标识 %q 反查不到嵌入字体集合", id)
		}
		found := false
		for _, ff := range faces {
			if gid, ok := ff.Face.Face().NominalGlyph('中'); ok && gid != 0 {
				found = true
				break
			}
		}
		if !found {
			t.Error("兜底集合不含「中」字形")
		}
	})

	t.Run("平台无候选时也要走兜底", func(t *testing.T) {
		cl := &countingLoader{data: a.Font}
		p := NewProvider(ProviderOptions{
			Candidates: []string{}, // 非 nil 的空表：模拟无候选的平台
			Timeout:    time.Second,
			Embedded:   cl,
		})
		id, rep, err := p.ResolveWithReport(FamilyCJK)
		if err != nil {
			t.Fatalf("无候选平台应兜底成功，实际: %v\n诊断:\n%s", err, rep)
		}
		if !strings.HasPrefix(id, "embedded:") {
			t.Errorf("应使用 embedded 标识，实际 %q", id)
		}
		if cl.calls != 1 {
			t.Errorf("无候选时也应只加载一次，实际 %d", cl.calls)
		}
	})

	t.Run("兜底结果按 family 缓存", func(t *testing.T) {
		cl := &countingLoader{data: a.Font}
		p := NewProvider(ProviderOptions{
			Candidates: []string{filepath.Join(t.TempDir(), "a.ttc")},
			Timeout:    time.Second,
			Embedded:   cl,
		})
		if _, _, err := p.ResolveWithReport(FamilyCJK); err != nil {
			t.Fatalf("首次解析失败: %v", err)
		}
		if _, _, err := p.ResolveWithReport(FamilyCJK); err != nil {
			t.Fatalf("二次解析失败: %v", err)
		}
		if cl.calls != 1 {
			t.Errorf("兜底结果必须缓存：嵌入加载被调用 %d 次", cl.calls)
		}
	})
}

// ============================================================================
// B5：二进制体积预算
//
// 只测**子集文件大小**是不够的：真正进产物的是 `//go:embed` 之后编进
// 二进制的字节。因此这里用同一套构建标志构建两次 `examples/demo`，
// 唯一差别是 `nofontembed` 标签（它把嵌入数据整段排除），再比字节数。
//
// 两条断言缺一不可：
//   - 增量 > 子集字节数 → 证明字体**确实进了二进制**（否则"没超预算"毫无意义）；
//   - 增量 ≤ 3 MiB    → 规范 §3.1 的硬门槛。
//
// ⚠️ 不得为了过门槛而删减已冻结的字符集（B 卡 §5）。真超了就缩小字符集
// 并如实标注覆盖边界，而不是偷偷少放几千字还宣称"覆盖完整"。
// ============================================================================

const (
	// 3 MiB：规范 §3.1 的硬门槛（按 1024 进制换算，比 3,000,000 更严格）。
	sizeBudgetBytes = 3 * 1024 * 1024

	// 增量下限：必须至少包含子集字体的字节数，否则说明嵌入没生效。
	// 取略低于子集大小（1,625,796）的值，留出对齐/元数据差异。
	minEmbeddedDeltaBytes = 1500000
)

func TestEmbeddedFallbackSizeBudget(t *testing.T) {
	root := moduleRoot(t)
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("找不到 go 命令，无法实测二进制增量: %v", err)
	}

	out := t.TempDir()

	// 同一套标志，唯一变量是构建标签。
	build := func(dst string, tags string, pkg string) int64 {
		t.Helper()
		args := []string{"build", "-trimpath", "-buildvcs=false", "-o", dst}
		if tags != "" {
			args = append(args, "-tags", tags)
		}
		args = append(args, pkg)
		cmd := exec.Command(goBin, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("构建失败（pkg=%s tags=%q）: %v\n%s", pkg, tags, err, stderr.String())
		}
		return fileSize(t, dst)
	}

	// 观察记录（不做断言）：examples/demo 走的是注入的 material.NewTheme()，
	// 不链接 pkg/runtime/theme，因此它的前后差恒为 0——"没超预算"在这里是假绿。
	// 这段保留在测试里，是为了让后来者一眼看到"为什么不用 demo 做断言靶子"。
	demoNo := build(filepath.Join(out, "demo_noembed.exe"), "nofontembed", "./examples/demo")
	demoEm := build(filepath.Join(out, "demo_embed.exe"), "", "./examples/demo")
	t.Logf("参考（不断言）: examples/demo 未嵌入=%d B 嵌入=%d B 增量=%d B "+
		"—— demo 注入 material.NewTheme()，不链接 theme 包，故此差值不可用于预算判定",
		demoNo, demoEm, demoEm-demoNo)

	// 断言靶子：testdata/sizeprobe 走 theme.NewTheme()，真实链接嵌入字体。
	probe := "./pkg/runtime/theme/testdata/sizeprobe"
	sizeNo := build(filepath.Join(out, "probe_noembed.exe"), "nofontembed", probe)
	sizeEm := build(filepath.Join(out, "probe_embed.exe"), "", probe)
	delta := sizeEm - sizeNo

	t.Logf("二进制体积实测（%s）: 未嵌入=%d B, 嵌入=%d B, 增量=%d B (预算 %d B)",
		probe, sizeNo, sizeEm, delta, sizeBudgetBytes)

	if delta < minEmbeddedDeltaBytes {
		t.Errorf("增量仅 %d B，低于下限 %d B——嵌入字体可能没有真的进二进制"+
			"（此时「未超预算」是假绿）", delta, minEmbeddedDeltaBytes)
	}
	if delta > sizeBudgetBytes {
		t.Errorf("增量 %d B 超过硬门槛 %d B（超出 %d B）；"+
			"正确做法是按 B 卡 §2.2 缩小字符集并如实标注边界，而不是删减已冻结的覆盖",
			delta, sizeBudgetBytes, delta-sizeBudgetBytes)
	}
}

// fileSize 返回文件字节数。
func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return st.Size()
}

// moduleRoot 从当前包目录向上找 go.mod。
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("向上找不到 go.mod")
		}
		dir = parent
	}
}

// ============================================================================
// B6：失败语义（关闭 A 卡约束 10 的未闭合项）
//
// A 阶段遗留：探测失败时 NewTheme 仍沿用 Gio 默认 Shaper，只保证"诊断可见"。
// B 必须把它收紧为「两条链路都失败 → 明确诊断错误」，且错误必须可观察
// （errors.Is(ErrFontNotFound) + 报告含两条链路），而不是崩溃或静默降级。
// ============================================================================

// failingLoader 模拟"嵌入字体也不可用"。
type failingLoader struct {
	calls int
	err   error
}

func (f *failingLoader) Load() ([]byte, string, error) {
	f.calls++
	return nil, "", f.err
}

// TestEmbeddedFallbackFailureSemantics B6：系统与嵌入都失败 →
// ErrFontNotFound + 报告含两条链路原因 + theme.Diagnostics() 可见。
func TestEmbeddedFallbackFailureSemantics(t *testing.T) {
	t.Run("两条链路都失败必须严格报错且保留双方原因", func(t *testing.T) {
		fl := &failingLoader{err: errors.New("嵌入字体损坏")}
		p := NewProvider(ProviderOptions{
			Candidates: []string{filepath.Join(t.TempDir(), "a.ttc")},
			Timeout:    time.Second,
			Embedded:   fl,
		})
		id, rep, err := p.ResolveWithReport(FamilyCJK)
		if !errors.Is(err, contracts.ErrFontNotFound) {
			t.Fatalf("期望包装 ErrFontNotFound，实际 %v", err)
		}
		if id != "" {
			t.Errorf("失败时不得返回标识（不得伪装成功），实际 %q", id)
		}
		if fl.calls != 1 {
			t.Errorf("系统失败后应尝试嵌入兜底 1 次，实际 %d", fl.calls)
		}
		if rep.Embedded == nil || !rep.Embedded.Attempted || rep.Embedded.Err == nil {
			t.Fatalf("报告必须记录嵌入链路的失败: %v", rep.Embedded)
		}
		if len(rep.Candidates) == 0 {
			t.Error("报告必须保留系统链路的候选诊断")
		}
		msg := err.Error()
		for _, want := range []string{"system", "embedded", "嵌入字体损坏"} {
			if !strings.Contains(msg, want) {
				t.Errorf("错误信息缺少 %q，无法定位是哪条链路失败: %s", want, msg)
			}
		}
		if _, ok := p.Faces(id); ok {
			t.Error("失败的标识不得进入集合缓存")
		}
	})

	t.Run("嵌入加载返回空字节 + nil 也必须算失败", func(t *testing.T) {
		// 这是最容易写出的"伪装成功"：Load 没报错但什么也没给。
		empty := &countingLoader{data: nil}
		p := NewProvider(ProviderOptions{
			Candidates: []string{filepath.Join(t.TempDir(), "a.ttc")},
			Timeout:    time.Second,
			Embedded:   empty,
		})
		_, rep, err := p.ResolveWithReport(FamilyCJK)
		if !errors.Is(err, contracts.ErrFontNotFound) {
			t.Fatalf("空字节应判失败，实际 %v\n诊断:\n%s", err, rep)
		}
		if rep.Embedded == nil || rep.Embedded.Err == nil {
			t.Error("空字节必须在诊断里给出原因")
		}
	})

	t.Run("失败不缓存，环境变化后可重试", func(t *testing.T) {
		// 先用损坏的嵌入字体失败一次，再换成好字体，必须能恢复。
		fl := &failingLoader{err: errors.New("boom")}
		p := NewProvider(ProviderOptions{
			Candidates: []string{filepath.Join(t.TempDir(), "a.ttc")},
			Timeout:    time.Second,
			Embedded:   fl,
		})
		if _, _, err := p.ResolveWithReport(FamilyCJK); err == nil {
			t.Fatal("首次应失败")
		}
		p.opts.Embedded = &countingLoader{data: loadEmbedded(t).Font}
		id, _, err := p.ResolveWithReport(FamilyCJK)
		if err != nil {
			t.Fatalf("环境恢复后应能重新解析成功，实际: %v", err)
		}
		if !strings.HasPrefix(id, "embedded:") {
			t.Errorf("应走嵌入兜底，实际 %q", id)
		}
	})

	t.Run("theme.Diagnostics 必须能看到两条链路", func(t *testing.T) {
		orig := defaultProvider
		t.Cleanup(func() { defaultProvider = orig })

		defaultProvider = NewProvider(ProviderOptions{
			Candidates: []string{filepath.Join(t.TempDir(), "missing.ttc")},
			Timeout:    time.Second,
			Embedded:   &failingLoader{err: errors.New("嵌入不可用")},
		})

		_, rep := NewThemeWithReport()
		if rep == nil || rep.Err == nil {
			t.Fatalf("默认主题路径必须返回失败诊断: %+v", rep)
		}
		diag := Diagnostics()
		if diag == nil {
			t.Fatal("Diagnostics() 返回 nil——A 卡约束 10 的「诊断必须可观察」未闭合")
		}
		if diag.Err == nil {
			t.Error("Diagnostics() 的报告没有错误，无法定位失败原因")
		}
		if diag.Embedded == nil || !diag.Embedded.Attempted {
			t.Error("Diagnostics() 未包含嵌入链路")
		}
		if len(diag.Candidates) == 0 {
			t.Error("Diagnostics() 未包含系统链路")
		}
		if !strings.Contains(diag.String(), "embedded") {
			t.Errorf("诊断文本必须包含两条链路:\n%s", diag)
		}
	})
}
