package theme

// ============================================================================
// TASK-FONT-001-C / C1：字形解析与 notdef 可观测
//
// 证据等级：**逻辑 + 受控实测**。
//
// # 为什么主证据不用系统字体
//
// 系统字体在 Windows / macOS / Linux 上各不相同，同一个 rune 的 GID 也不同，
// 断言具体数值会变成跨平台红灯。因此顺序实验用 **goregular（Go 内置纯拉丁字体，
// 字节随库分发，任何平台一致）** 与嵌入子集组成对照：
//
//	- goregular：有 ASCII，**无汉字**
//	- 嵌入子集：有 ASCII **和** GB2312 汉字
//
// 两者组合能同时证明「顺序决定优先」与「缺字形才跨到下一个」，且与机器无关。
// 系统字体场景只做**记录性观测**（不断言具体 GID），见 emoji 用例。
// ============================================================================

import (
	"errors"
	"strings"
	"testing"
	"time"

	giofont "gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/text"
	gotextfont "github.com/go-text/typesetting/font"
	"golang.org/x/image/font/gofont/goregular"
)

// embeddedFaces 解析嵌入子集，得到受控的 face 集合。
func embeddedFaces(t *testing.T) []giofont.FontFace {
	t.Helper()
	a := loadEmbedded(t)
	faces, err := opentype.ParseCollection(a.Font)
	if err != nil {
		t.Fatalf("解析嵌入字体失败: %v", err)
	}
	if len(faces) == 0 {
		t.Fatal("嵌入字体解析出 0 个 face")
	}
	return faces
}

// latinFaces 解析 goregular（纯拉丁，无汉字）作为对照字体。
func latinFaces(t *testing.T) []giofont.FontFace {
	t.Helper()
	faces, err := opentype.ParseCollection(goregular.TTF)
	if err != nil {
		t.Fatalf("解析 goregular 失败: %v", err)
	}
	if len(faces) == 0 {
		t.Fatal("goregular 解析出 0 个 face")
	}
	return faces
}

// controlledShaper 构造完全受控的 shaper：禁用系统字体，只装给定 collection。
//
// 这是 C 断言具体 GID 的前提——一旦允许系统字体介入，结果就取决于机器上
// 装了什么，不再是确定性的。
func controlledShaper(collection ...[]giofont.FontFace) *text.Shaper {
	var all []giofont.FontFace
	for _, c := range collection {
		all = append(all, c...)
	}
	return text.NewShaper(text.NoSystemFonts(), text.WithCollection(all))
}

// nominalGID 取某个 rune 在 faces[0] 里的字形 id（字体指纹）。
func nominalGID(faces []giofont.FontFace, r rune) (gotextfont.GID, bool) {
	if len(faces) == 0 {
		return 0, false
	}
	return faces[0].Face.Face().NominalGlyph(r)
}

// TestFontFallbackChainGlyphIDDecode C 卡 §6-1：GlyphID 反解必须与 NominalGlyph 一致。
//
// 这是整个 C 的地基：**如果这条不成立，下面所有基于 gid 的结论都是假的。**
// 它同时充当「Gio 改变位布局」的警报器——布局一变，这里立刻红。
func TestFontFallbackChainGlyphIDDecode(t *testing.T) {
	faces := embeddedFaces(t)
	sh := controlledShaper(faces)
	res := NewResolver(sh)

	a := loadEmbedded(t)
	cps := parseCharset(t, a.Charset)
	// 抽样：全部 7,540 个都跑 shaping 太慢，取步进样本 + ASCII 全段。
	var sample []rune
	for i, r := range cps {
		if i%149 == 0 {
			sample = append(sample, r)
		}
	}
	for r := rune(0x20); r <= 0x7E; r++ {
		sample = append(sample, r)
	}
	if len(sample) < 50 {
		t.Fatalf("抽样规模过小: %d", len(sample))
	}

	checked := 0
	for _, r := range sample {
		want, ok := nominalGID(faces, r)
		if !ok || want == 0 {
			// 覆盖外的码位由 notdef 用例单独验证，这里只校验"有字形"的那批。
			continue
		}
		got := res.ResolveRune(r)
		if got.GID != want {
			t.Fatalf("rune %q (U+%04X) 反解 gid=%d，但 NominalGlyph=%d——"+
				"GlyphID 位布局与 Gio 不一致，C 的全部判定失效", r, r, got.GID, want)
		}
		if !got.HasGlyph {
			t.Fatalf("rune %q (U+%04X) gid=%d 非 0，却被判为无字形", r, r, got.GID)
		}
		checked++
	}
	if checked < 50 {
		t.Fatalf("参与交叉校验的码位仅 %d 个（应 ≥50）——抽样或覆盖判定有误", checked)
	}
	t.Logf("交叉校验: 抽样 %d 个码位，其中 %d 个覆盖内 gid 与 NominalGlyph 逐项一致",
		len(sample), checked)
}

// TestFontFallbackChainNotdef C 卡 §6-2：覆盖外码位必须判为 notdef。
//
// 含一条**永久反向自检**：同一批判定在换成覆盖内码位后必须翻转。
// 没有这条，"恒返回有字形"的假实现也能通过前半段。
func TestFontFallbackChainNotdef(t *testing.T) {
	faces := embeddedFaces(t)
	sh := controlledShaper(faces)
	res := NewResolver(sh)

	// 嵌入子集是 GB2312 覆盖，这些码位必然在清单外（B 已冻结该边界）。
	outside := []rune{
		0x1F600, // 😀 emoji
		0x2000B, // 罕用汉字，CJK 扩展 B
		0x9FFF,  // CJK 基本区末位，GB2312 外
		0x3007,  // 〇（实测嵌入子集不含）
		0x2605,  // ★ 不在子集内？——实际在，见下方过滤
	}

	for _, r := range outside {
		got := res.ResolveRune(r)
		_, inSubset := nominalGID(faces, r)
		if inSubset {
			// 该码位其实被子集覆盖了（上面的注释可能有误），跳过而非误判。
			continue
		}
		if got.HasGlyph {
			t.Errorf("rune %q (U+%04X) 在嵌入子集外，却被判为有字形 (gid=%d)——"+
				"notdef 判定失效", r, r, got.GID)
		}
		if got.GID != 0 {
			t.Errorf("rune %q (U+%04X) 应为 notdef (gid=0)，实际 gid=%d", r, r, got.GID)
		}
	}

	t.Run("反向自检：覆盖内码位必须翻转", func(t *testing.T) {
		// 判定器若恒返回"无字形"，上面会全绿——用覆盖内码位把这种实现逼出来。
		inside := []rune{'A', '世', '汉', '中'}
		for _, r := range inside {
			got := res.ResolveRune(r)
			if !got.HasGlyph {
				t.Errorf("rune %q (U+%04X) 在嵌入子集内，却被判为无字形——"+
					"判定器可能恒返回缺失，上一轮的 notdef 结论不可信", r, r)
			}
		}
	})

	t.Run("nil shaper 必须显式退化为无字形", func(t *testing.T) {
		// 不 panic，也不伪装成"有字形"：调用方可能在主题构建失败时拿到 nil。
		var nilResolver = NewResolver(nil)
		if got := nilResolver.ResolveRune('A'); got.HasGlyph || got.Shaped {
			t.Errorf("nil shaper 的解析结果为 HasGlyph=%v Shaped=%v，应均为 false",
				got.HasGlyph, got.Shaped)
		}
	})
}

// TestFontFallbackChainMixedScript C 卡 §6-3：中英混排。
func TestFontFallbackChainMixedScript(t *testing.T) {
	faces := embeddedFaces(t)
	res := NewResolver(controlledShaper(faces))

	const mixed = "Hello 世界"
	missing := res.MissingGlyphs(mixed)
	if len(missing) != 0 {
		t.Fatalf("嵌入子集下 %q 有缺字形: %q——中英混排未覆盖", mixed, string(missing))
	}

	// 逐字符断言：中英段都必须有字形，且 GID 与指纹一致。
	for _, r := range mixed {
		want, ok := nominalGID(faces, r)
		got := res.ResolveRune(r)
		if !ok {
			t.Fatalf("%q 里的 %q 不在嵌入子集内，用例前提不成立", mixed, r)
		}
		if got.GID != want {
			t.Errorf("%q 的 gid=%d，指纹=%d：混排时命中了预期之外的字体", r, got.GID, want)
		}
	}

	// 系统场景：只记录，不断言——系统字体因平台而异。
	if sysFaces, ok := trySystemFaces(t); ok {
		sysRes := NewResolver(text.NewShaper(text.WithCollection(sysFaces)))
		t.Logf("系统字体下 %q 缺字形: %q", mixed, string(sysRes.MissingGlyphs(mixed)))
	}
}

// TestFontFallbackChainOrderSemantics C 卡 §6-4：collection 顺序 = 字形级优先级。
//
// 用 goregular（无汉字）与嵌入子集（有汉字）组成两端，观察同一 rune 的 gid：
// 谁排前面，就该拿到谁的指纹。
func TestFontFallbackChainOrderSemantics(t *testing.T) {
	emb := embeddedFaces(t)
	lat := latinFaces(t)

	// 找一个两者**都有**但 GID 不同的 rune：GID 相同就无法区分谁被选中。
	var probe rune
	for _, r := range []rune{'A', 'B', 'a', 'z', '0', '1'} {
		eg, eok := nominalGID(emb, r)
		lg, lok := nominalGID(lat, r)
		if eok && lok && eg != 0 && lg != 0 && eg != lg {
			probe = r
			break
		}
	}
	if probe == 0 {
		t.Fatal("找不到在两个字体中 GID 不同的共有 rune——无法做顺序实验")
	}
	embGID, _ := nominalGID(emb, probe)
	latGID, _ := nominalGID(lat, probe)
	t.Logf("探针 rune=%q 嵌入指纹=%d 拉丁指纹=%d", probe, embGID, latGID)

	t.Run("拉丁在前则命中拉丁", func(t *testing.T) {
		res := NewResolver(controlledShaper(lat, emb))
		if got := res.ResolveRune(probe); got.GID != latGID {
			t.Errorf("collection=[拉丁, 嵌入] 时 gid=%d，期望拉丁指纹 %d", got.GID, latGID)
		}
	})

	t.Run("嵌入在前则命中嵌入", func(t *testing.T) {
		res := NewResolver(controlledShaper(emb, lat))
		if got := res.ResolveRune(probe); got.GID != embGID {
			t.Errorf("collection=[嵌入, 拉丁] 时 gid=%d，期望嵌入指纹 %d", got.GID, embGID)
		}
	})

	t.Run("顺序必须真的改变结果", func(t *testing.T) {
		// 若两个顺序给出同一个 gid，上面的断言对"顺序无效"的实现也会通过。
		a := NewResolver(controlledShaper(lat, emb)).ResolveRune(probe)
		b := NewResolver(controlledShaper(emb, lat)).ResolveRune(probe)
		if a.GID == b.GID {
			t.Errorf("两种顺序给出相同 gid=%d——顺序不改变选择，优先级断言是空的", a.GID)
		}
	})

	t.Run("FaceIdx 必须指向 collection 的实际位置", func(t *testing.T) {
		// 为什么单靠 gid 指纹不够：gid 指纹只能区分"哪个字体"，无法发现
		// **位布局解码本身**出错——FaceIdx 错位时 gid 依然正确，测试仍全绿。
		// 变异实测过一次：把 splitGlyphID 的位移改错，改动前 5 个用例全绿。
		// 因此 FaceIdx 必须有断言，否则它就是个可能悄悄出错却无人察觉的字段。
		//
		// collection 顺序 = shaperImpl.faces 的 append 顺序（Gio 按序 Load），
		// 且本用例禁用系统字体，不会有额外 face 插入。
		latFirst := NewResolver(controlledShaper(lat, emb))
		if got := latFirst.ResolveRune(probe); got.FaceIdx != 0 {
			t.Errorf("[拉丁, 嵌入] 下 %q 的 FaceIdx=%d，期望 0（拉丁在前）", probe, got.FaceIdx)
		}
		// 拉丁没有汉字 → 必须落到 index 1（嵌入）。
		if got := latFirst.ResolveRune('世'); got.FaceIdx != 1 {
			t.Errorf("[拉丁, 嵌入] 下 '世' 的 FaceIdx=%d，期望 1（拉丁缺字形，补到嵌入）",
				got.FaceIdx)
		}

		embFirst := NewResolver(controlledShaper(emb, lat))
		if got := embFirst.ResolveRune(probe); got.FaceIdx != 0 {
			t.Errorf("[嵌入, 拉丁] 下 %q 的 FaceIdx=%d，期望 0（嵌入在前）", probe, got.FaceIdx)
		}
		if got := embFirst.ResolveRune('世'); got.FaceIdx != 0 {
			t.Errorf("[嵌入, 拉丁] 下 '世' 的 FaceIdx=%d，期望 0（嵌入自身就有字形）",
				got.FaceIdx)
		}
	})
}

// TestFontFallbackChainSupplementsMissingGlyph C 卡 §6-5：缺字形才跨到下一个。
//
// goregular 无汉字，因此汉字必须 fallback 到嵌入子集（排在其后）。
// 这是「按字形补位」的直接证据——不是整段文本换字体，而是逐 rune 选。
func TestFontFallbackChainSupplementsMissingGlyph(t *testing.T) {
	emb := embeddedFaces(t)
	lat := latinFaces(t)

	// 拉丁字体对汉字必然无字形；嵌入必然有。
	hanzi := []rune{'世', '界', '中', '文'}
	for _, r := range hanzi {
		if _, ok := nominalGID(lat, r); ok {
			t.Fatalf("goregular 竟然含 %q 的字形——用例前提（纯拉丁字体）不成立", r)
		}
		want, ok := nominalGID(emb, r)
		if !ok || want == 0 {
			t.Fatalf("嵌入子集缺 %q——用例前提不成立", r)
		}
	}

	// [拉丁, 嵌入]：拉丁缺字形 → 必须补到嵌入。
	res := NewResolver(controlledShaper(lat, emb))
	for _, r := range hanzi {
		got := res.ResolveRune(r)
		want, _ := nominalGID(emb, r)
		if !got.HasGlyph {
			t.Fatalf("%q 在 [拉丁, 嵌入] 下无字形——fallback 未补位", r)
		}
		if got.GID != want {
			t.Errorf("%q gid=%d，期望嵌入指纹 %d：补位的字体不对", r, got.GID, want)
		}
	}

	t.Run("反向自检：只有拉丁时汉字必须是 notdef", func(t *testing.T) {
		// 若"恒返回有字形"，上面会全绿。这里证明单独装拉丁时确实缺字形，
		// 从而坐实"上面的有字形是补位得来的"。
		only := NewResolver(controlledShaper(lat))
		for _, r := range hanzi {
			if got := only.ResolveRune(r); got.HasGlyph {
				t.Errorf("仅装拉丁字体时 %q 却有字形 (gid=%d)——"+
					"补位断言失去对照，无法证明 fallback 生效", r, got.GID)
			}
		}
	})
}

// TestFontFallbackChainCoverage C 卡 §6-6：覆盖判定必须可证伪。
//
// 这条守卫的是一个**真实漏洞**：B 的探测标准是"能否解析"，不检查"是否真有汉字"。
// 于是只有拉丁字体的机器会"系统命中成功、报告无错、汉字全部 notdef"。
// 判定器必须能把这种字体挡在门外——用 goregular 当样本坐实这一点。
func TestFontFallbackChainCoverage(t *testing.T) {
	emb := embeddedFaces(t)
	lat := latinFaces(t)
	req := RequiredRunes()

	t.Run("嵌入子集必须全覆盖准入集", func(t *testing.T) {
		hit, missing := Coverage(emb, req)
		if hit != len(req) {
			t.Errorf("嵌入子集命中 %d/%d，缺 %q——准入集选了子集不含的码位",
				hit, len(req), string(missing))
		}
		if !CoverageSufficient(hit, len(req)) {
			t.Error("嵌入子集被判为覆盖不足——准入阈值或判定有误")
		}
	})

	t.Run("纯拉丁字体必须被判为覆盖不足", func(t *testing.T) {
		// 这就是漏洞本体：goregular 能解析、有 ASCII，但一个汉字都没有。
		hit, _ := Coverage(lat, req)
		if !CoverageSufficient(hit, len(req)) {
			return // 期望结果
		}
		t.Errorf("纯拉丁字体被判为覆盖足够 (hit=%d/%d)——"+
			"「命中但汉字全 notdef」的漏洞没被挡住", hit, len(req))
	})

	t.Run("反向自检：判定器不得恒定结论", func(t *testing.T) {
		// 恒返回"全覆盖"的实现能通过上面第一条；恒返回"都不覆盖"能骗过第二条。
		// 这里要求同一个判定器对两组输入给出**相反**结论。
		hanziHit, _ := Coverage(emb, req)
		emojiHit, emojiMiss := Coverage(emb, []rune{0x1F600, 0x1F601, 0x1F602})
		if hanziHit == 0 {
			t.Error("嵌入子集对准入集命中 0——判定器可能恒返回缺失")
		}
		if emojiHit != 0 {
			t.Errorf("嵌入子集对 emoji 命中 %d——判定器可能恒返回覆盖", emojiHit)
		}
		if len(emojiMiss) != 3 {
			t.Errorf("emoji 缺失清单长度 %d，期望 3", len(emojiMiss))
		}
	})

	t.Run("阈值边界", func(t *testing.T) {
		// 20 个码位：命中 18 → 恰好达标；17 → 不达标。
		if !CoverageSufficient(18, 20) {
			t.Error("18/20 应达标（阈值 0.9）")
		}
		if CoverageSufficient(17, 20) {
			t.Error("17/20 不应达标（阈值 0.9）")
		}
		if CoverageSufficient(20, 0) {
			t.Error("total=0 必须判为不达标：空输入不得被读成通过")
		}
	})

	t.Run("空集合必须判为全缺失而非全命中", func(t *testing.T) {
		hit, missing := Coverage(nil, req)
		if hit != 0 || len(missing) != len(req) {
			t.Errorf("空 faces 得到 hit=%d missing=%d——空输入被读成了通过",
				hit, len(missing))
		}
	})
}

// TestFontFallbackChainEmoji C 卡 §6-8：emoji 实测与冻结边界。
//
// 结论必须是**实测**的，不是假设的：父卡 §3.2 第 2 条明确要求 C 回答
// "emoji 有没有字形"，而不是假定 Gio 会兜底。
func TestFontFallbackChainEmoji(t *testing.T) {
	emb := embeddedFaces(t)
	res := NewResolver(controlledShaper(emb))

	// 嵌入子集**永不**含 emoji（B 子集化时就不含，属已冻结边界）。
	for _, r := range []rune{0x1F600, 0x1F601, 0x2764} {
		if got := res.ResolveRune(r); got.HasGlyph {
			t.Errorf("嵌入子集竟然含 emoji %q (gid=%d)——与 B 冻结的字符集边界矛盾",
				r, got.GID)
		}
	}

	// 缺字形必须能被 MissingGlyphs 如实列出（降级是"报告"，不是"补字形"）。
	missing := res.MissingGlyphs("A😀B")
	if len(missing) != 1 || missing[0] != 0x1F600 {
		t.Errorf("MissingGlyphs(\"A😀B\")=%q，期望只含 U+1F600", string(missing))
	}

	// 系统场景：只记录，不断言——emoji 是否可用取决于机器上装了什么。
	// C 卡 §1.4 实测：命中 msyh 时有字形（gid=6787），裸 shaper 时没有。
	if sysFaces, ok := trySystemFaces(t); ok {
		sysRes := NewResolver(text.NewShaper(text.WithCollection(sysFaces)))
		got := sysRes.ResolveRune(0x1F600)
		t.Logf("系统字体 emoji: HasGlyph=%v gid=%d", got.HasGlyph, got.GID)
	}
}

// TestFontFallbackChainSupplementOnDemand C 卡 §6-7：补充链必须**按需**触发。
//
// 这是 C 卡 §4 的落地检查：无条件追加嵌入在本机是纯亏（§1.3 实测零增量），
// 因此"覆盖完整时不加载"必须能被证明，而不只是写在注释里。
func TestFontFallbackChainSupplementOnDemand(t *testing.T) {
	t.Run("覆盖完整时不加载嵌入", func(t *testing.T) {
		// 端到端：用含汉字的字体当系统候选，覆盖判定应全命中。
		good := writeFont(t, t.TempDir(), "good.otf", hanziFontBytes(t))
		cl := &countingLoader{data: loadEmbedded(t).Font}
		p := NewProvider(ProviderOptions{
			Candidates: []string{good},
			Timeout:    time.Second,
			Embedded:   cl,
		})
		id, rep, err := p.ResolveWithReport(FamilyCJK)
		if err != nil {
			t.Fatalf("覆盖完整的系统候选应命中: %v\n诊断:\n%s", err, rep)
		}
		if !strings.HasPrefix(id, "system:") {
			t.Errorf("应使用 system 标识，实际 %q", id)
		}
		if cl.calls != 0 {
			t.Errorf("覆盖完整却加载了嵌入 %d 次——补充链未按需触发，白付解析成本", cl.calls)
		}
		if rep.Coverage == nil || len(rep.Coverage.Missing) != 0 {
			t.Errorf("覆盖判定应全命中，实际 %v", rep.Coverage)
		}
		if rep.Coverage != nil && rep.Coverage.Supplemented {
			t.Error("覆盖完整却标记了 Supplemented")
		}
	})

	t.Run("有缺口时追加嵌入并进诊断", func(t *testing.T) {
		// supplement 是私有方法：这里做分支级验证，因为"达标但有缺口"
		// 的端到端场景需要专门造一个缺几个字的字体，代价远大于收益。
		cl := &countingLoader{data: loadEmbedded(t).Font}
		p := NewProvider(ProviderOptions{Embedded: cl})
		base := latinFaces(t)
		rep := &ProbeReport{
			Coverage: &CoverageResult{Required: 20, Hit: 18, Missing: []rune{'鿿', '〇'}},
		}

		got := p.supplement(base, rep)
		if cl.calls != 1 {
			t.Errorf("有缺口时应加载嵌入 1 次，实际 %d", cl.calls)
		}
		if len(got) <= len(base) {
			t.Fatalf("补充后集合应变大: base=%d got=%d", len(base), len(got))
		}
		if !rep.Coverage.Supplemented {
			t.Error("补充成功必须标记 Supplemented=true，否则诊断看不出补没补")
		}
		if rep.Embedded == nil || !rep.Embedded.Attempted {
			t.Error("补充链路必须进诊断")
		}
		// 追加在**末尾**：系统在前面才有优先（C 卡 §1.2 顺序即优先级）。
		if got[0].Font.Typeface != base[0].Font.Typeface {
			t.Errorf("补充后首位不应改变: got[0]=%q base[0]=%q",
				got[0].Font.Typeface, base[0].Font.Typeface)
		}
	})

	t.Run("嵌入加载失败不得影响已命中的系统结果", func(t *testing.T) {
		// 系统已经可用，嵌入只是增益：加载失败要记录，但不能把整体打成失败。
		p := NewProvider(ProviderOptions{
			Embedded: &failingLoader{err: errors.New("测试注入：嵌入不可用")},
		})
		base := latinFaces(t)
		rep := &ProbeReport{
			Coverage: &CoverageResult{Required: 20, Hit: 18, Missing: []rune{'鿿'}},
		}
		got := p.supplement(base, rep)
		if len(got) != len(base) {
			t.Errorf("嵌入失败时应原样返回系统集合: got=%d base=%d", len(got), len(base))
		}
		if rep.Coverage.Supplemented {
			t.Error("嵌入失败却标记了 Supplemented")
		}
		if rep.Embedded == nil || rep.Embedded.Err == nil {
			t.Error("嵌入失败原因必须留在诊断里")
		}
	})
}

// TestFontFallbackChainCoverageInsufficient 纯拉丁字体不得被当作 CJK 命中
// （C 卡 §3.1 的漏洞本体）。
//
// 这个场景原本是 B「系统命中不解析嵌入」用例的前提（用 goregular 当 good 字体），
// 在 C 里它变成了漏洞的证据：能解析、有 ASCII、一个汉字都没有。
func TestFontFallbackChainCoverageInsufficient(t *testing.T) {
	latin := writeFont(t, t.TempDir(), "latin.ttf", goregular.TTF)
	cl := &countingLoader{data: loadEmbedded(t).Font}
	p := NewProvider(ProviderOptions{
		Candidates: []string{latin},
		Timeout:    time.Second,
		Embedded:   cl,
	})

	id, rep, err := p.ResolveWithReport(FamilyCJK)
	if err != nil {
		t.Fatalf("纯拉丁候选应被挡住并兜底成功，实际失败: %v\n诊断:\n%s", err, rep)
	}
	if !strings.HasPrefix(id, "embedded:") {
		t.Errorf("纯拉丁字体不应算 CJK 命中，应兜底到 embedded，实际 %q", id)
	}
	if len(rep.Candidates) != 1 || rep.Candidates[0].Status != StatusCoverageInsufficient {
		t.Fatalf("候选状态应为 coverage_insufficient，实际 %v", rep.Candidates)
	}
	if rep.Candidates[0].Err == nil {
		t.Error("覆盖不足必须记录原因——否则诊断回答不了为什么没用它")
	}
	if !strings.Contains(rep.String(), string(StatusCoverageInsufficient)) {
		t.Errorf("报告文本必须暴露覆盖不足:\n%s", rep)
	}
}

// TestFontFallbackChainProbeCost C 卡 §6-9：覆盖判定不得明显拖慢探测。
//
// 不断言端到端探测耗时（那取决于磁盘与系统字体，跨机器波动大），
// 而是测**覆盖判定本身**的开销：它跑在每个候选的命中路径上，必须是常量级。
func TestFontFallbackChainProbeCost(t *testing.T) {
	faces := embeddedFaces(t)
	// 预热，避免把首次解析算进来。
	checkCoverage(faces)

	start := time.Now()
	const n = 200
	for i := 0; i < n; i++ {
		checkCoverage(faces)
	}
	per := time.Since(start) / n
	t.Logf("覆盖判定单次耗时: %v（%d 次平均，20 个码位）", per, n)
	// 预算：单次必须远小于系统探测的毫秒级开销。1ms 上限留了数量级余量。
	if per > time.Millisecond {
		t.Errorf("覆盖判定单次 %v 超过 1ms 预算——它跑在探测热路径上，"+
			"会让本已 ~96ms 的系统探测更慢（该延迟归 D 解决，C 不得加剧）", per)
	}
}

// trySystemFaces 尝试取得系统 CJK 集合；系统不可用时返回 false（仅供记录性观测）。
func trySystemFaces(t *testing.T) ([]giofont.FontFace, bool) {
	t.Helper()
	p := NewProvider(ProviderOptions{})
	id, _, err := p.ResolveWithReport(FamilyCJK)
	if err != nil || id == "" {
		return nil, false
	}
	faces, ok := p.Faces(id)
	if !ok || len(faces) == 0 {
		return nil, false
	}
	return faces, true
}
