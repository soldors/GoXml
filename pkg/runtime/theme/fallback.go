package theme

// ============================================================================
// TASK-FONT-001-C：按字形覆盖的 fallback 链（C1 字形解析 + C2 覆盖判定）
//
// 范围严格限定在 C 卡 §2 / §3：
//   - C1：让 Gio 的逐字形选择结果**可观测**（命中哪个 face、有没有字形）；
//   - C2：把「能否解析」升级为「是否真的有汉字」，挡住静默 notdef。
//
// **不重建 fallback 链**：Gio v0.10.3 的 `fontscan.FontMap.ResolveFace` 已经
// 实现了 CSS 风格的 5 级逐字形 fallback（见 C 卡 §0）。C 只做观测与准入。
//
// 路线 B（ADR-0012 决策 1）：本包仍是字体决策的唯一落点。
// ============================================================================

import (
	"strings"

	giofont "gioui.org/font"
	"gioui.org/text"
	gotextfont "github.com/go-text/typesetting/font"
	"golang.org/x/image/math/fixed"
)

// GlyphID 位布局（Gio v0.10.3 `text/shaper.go` 的 `newGlyphID`）。
//
// ⚠️ 这是 Gio **未导出的实现细节**，本包复刻它纯粹为了观测——Gio 没有公开
// 「这个 rune 命中了哪个 face」的查询接口。相依性由两条自检兜底（C 卡 §2.2）：
// 交叉校验（覆盖内码位的 gid 必须等于 NominalGlyph）与反向自检（覆盖外码位
// 必须判为无字形）。**Gio 升级后若这两条变红，不许放宽断言，要重读源码。**
const (
	gidFaceBits = 16
	gidSizeBits = 16
	gidGIDBits  = 64 - gidFaceBits - gidSizeBits
)

// splitGlyphID 反解 text.Glyph.ID，与 Gio 的 splitGlyphID 同布局。
func splitGlyphID(g text.GlyphID) (ppem fixed.Int26_6, faceIdx int, gid gotextfont.GID) {
	faceIdx = int(uint64(g) >> (gidGIDBits + gidSizeBits))
	rawPpem := (g & ((1<<gidSizeBits - 1) << gidGIDBits)) >> gidGIDBits
	gid = gotextfont.GID(g) & (1<<gidGIDBits - 1)
	return fixed.Int26_6(rawPpem), faceIdx, gid
}

// GlyphResolution 是单个 rune 在给定 shaper 下的字形解析结果。
//
// # 语义（不要误读）
//
//   - `HasGlyph == false` 表示**链上没有任何字体提供该字形**，Gio 会渲染
//     notdef（豆腐块）。这是「事实」，不是「错误」——C 只负责让它可见。
//   - `Shaped == false` 表示 shaper 连一个 glyph 都没产出（比 notdef 更彻底）。
//     二者都算缺字形，但分开记录，便于区分 Gio 的两种退化路径。
//   - `FaceIdx` 是 shaper **内部** face 列表的索引，不是稳定标识：它随
//     collection 的装配顺序变化。只用于诊断，不要拿它做跨进程假设。
type GlyphResolution struct {
	// Rune 是被解析的字符。
	Rune rune
	// GID 是命中字体的字形 id。**0 表示 notdef**。
	GID gotextfont.GID
	// HasGlyph 报告是否真的有字形（GID != 0 且 shaper 产出了 glyph）。
	HasGlyph bool
	// Shaped 报告 shaper 是否产出了至少一个 glyph。
	Shaped bool
	// FaceIdx 是命中的 face 索引；未产出 glyph 时为 -1。
	FaceIdx int
	// Advance 是该字形的步进宽度。
	Advance fixed.Int26_6
}

// Resolver 基于一个 shaper 做逐 rune 字形解析。
//
// # 并发
//
// `text.Shaper` 自身不是并发安全的（Gio 文档：不同顶层窗口要用不同 Shaper），
// 因此 Resolver 同样**不是**并发安全的。不要在多个 goroutine 间共享一个实例。
type Resolver struct {
	shaper *text.Shaper
	params text.Parameters
}

// NewResolver 用默认排版参数构造 Resolver。shaper 为 nil 时所有解析都退化为
// 「无字形」——这是显式行为，不是 panic：调用方可能在主题构建失败时拿到 nil。
func NewResolver(s *text.Shaper) *Resolver {
	return &Resolver{
		shaper: s,
		params: text.Parameters{
			PxPerEm:  fixed.Int26_6(16 * 64),
			MaxWidth: 1 << 30,
		},
	}
}

// ResolveRune 解析单个 rune。
//
// **一次只 shape 一个 rune** 是必需的：Gio 的 `text.Glyph` 只带 cluster 的
// rune **计数**，不带 rune 本身，多字符 shaping 无法把 glyph 可靠地映射回输入。
func (r *Resolver) ResolveRune(rn rune) GlyphResolution {
	res := GlyphResolution{Rune: rn, FaceIdx: -1}
	if r == nil || r.shaper == nil {
		return res
	}
	r.shaper.LayoutString(r.params, string(rn))
	var last text.Glyph
	n := 0
	for g, ok := r.shaper.NextGlyph(); ok; g, ok = r.shaper.NextGlyph() {
		last = g
		n++
	}
	if n == 0 {
		// 连 notdef 都没产出：比「有 notdef」更彻底的缺失。
		return res
	}
	res.Shaped = true
	_, faceIdx, gid := splitGlyphID(last.ID)
	res.FaceIdx = faceIdx
	res.GID = gid
	res.HasGlyph = gid != 0
	res.Advance = last.Advance
	return res
}

// MissingGlyphs 返回文本中所有**没有字形**的 rune（去重、按首次出现顺序）。
//
// 这是 C4 的降级入口：emoji 等本框架不提供字形的字符会被如实列出，
// 调用方可以据此决定显示占位符、告警或记录诊断——**而不是静默渲染豆腐块**。
func (r *Resolver) MissingGlyphs(s string) []rune {
	if r == nil || s == "" {
		return nil
	}
	var out []rune
	seen := make(map[rune]bool)
	for _, rn := range s {
		if seen[rn] {
			continue
		}
		seen[rn] = true
		if !r.ResolveRune(rn).HasGlyph {
			out = append(out, rn)
		}
	}
	return out
}

// ============================================================================
// C2：覆盖判定（候选准入）
// ============================================================================

// RequiredRunes 是准入判定用的最小 CJK 码位集。
//
// 设计取舍：
//   - **抽样，不是全量**。它跑在探测热路径上（每个候选都要判定一次），
//     用 charset.txt 的 7,540 个码位做判定会把探测耗时推高——而系统探测
//     本就已实测 ~96 ms（C 卡 §1.5，归 D 解决），C 不得让它更慢。
//   - 全部取自 **GB2312 一级字**（3,755 个高频字中的常用者）与常用全角标点。
//     任何自称 CJK 的字体都必须覆盖这些码位；覆盖不了就不该被当作 CJK 字体命中。
//   - 返回的切片是副本，调用方改动不会污染下一轮判定。
func RequiredRunes() []rune {
	return append([]rune(nil), requiredRunes...)
}

// requiredRunes 是 RequiredRunes 的源数据（C 卡 §3.2）。
var requiredRunes = []rune{
	// GB2312 一级字中的高频字。
	'的', '一', '是', '在', '不', '了', '有', '和',
	'人', '这', '中', '大', '为', '上', '个', '国',
	// 常用全角标点（中文排版不可缺）。
	'，', '。', '、', '；',
}

// minCoverageRatio 是候选准入的最低覆盖率（C 卡 §3.2）。
//
// **为什么不写 100%**：单个抽样码位缺失不该判死整族字体——某些字体确实会
// 偶缺一两个一级字，但整体可用。反过来，缺 3 个以上（20 个里命中 < 18）
// 就说明它不是可用的 CJK 字体：那正是「命中拉丁字体 → 汉字全 notdef」的漏洞
// 要挡住的情形（拉丁字体对这 20 个码位的命中数是 0）。
//
// 这是**判断**不是事实：取值理由写在这里，改值必须同步改注释与 C 卡。
const minCoverageRatio = 0.9

// Coverage 统计 faces 对 required 的覆盖，返回命中数与缺失清单。
//
// 判定用 `NominalGlyph` 是否存在**字形**，而不是字体元数据的自述——自述可能
// 撒谎，字形不会。
//
// faces 为空或 required 为空时返回 (0, required 副本)：把"没东西可判"表达成
// "全部缺失"，而不是"全部命中"，避免空输入被读成通过。
//
// # 性能（实测，勿回退）
//
// 底层 face **必须在码位循环外解包**。`faces[i].Face.Face()` 实测约 29µs/次，
// 若放在循环里，20 个码位的判定就是 ~590µs；而 `NominalGlyph` 本身是纳秒级——
// 瓶颈全在接口解包。它跑在探测热路径上（每个候选一次），这个数量级差异
// 会直接叠加到本已 ~96 ms 的系统探测上。
func Coverage(faces []giofont.FontFace, required []rune) (hit int, missing []rune) {
	if len(required) == 0 {
		return 0, nil
	}
	under := underlyingFaces(faces)
	for _, r := range required {
		if anyFaceHasGlyph(under, r) {
			hit++
			continue
		}
		missing = append(missing, r)
	}
	return hit, missing
}

// underlyingFaces 一次性解包出底层 *font.Face（见 Coverage 的性能说明）。
func underlyingFaces(faces []giofont.FontFace) []*gotextfont.Face {
	out := make([]*gotextfont.Face, 0, len(faces))
	for i := range faces {
		if faces[i].Face == nil {
			continue
		}
		if f := faces[i].Face.Face(); f != nil {
			out = append(out, f)
		}
	}
	return out
}

// anyFaceHasGlyph 报告解包后的 faces 中是否有**任一** face 含该 rune 的字形。
func anyFaceHasGlyph(faces []*gotextfont.Face, r rune) bool {
	for i := range faces {
		if gid, ok := faces[i].NominalGlyph(r); ok && gid != 0 {
			return true
		}
	}
	return false
}

// CoverageSufficient 判定覆盖率是否达到准入线。
//
// 判据是「命中数 / 总数 >= 阈值」，分母是 **required 的长度**而不是 faces 的数量：
// 覆盖率衡量的是字体对文字的覆盖能力，与集合里有多少个 face 无关。
func CoverageSufficient(hit, total int) bool {
	if total <= 0 {
		return false
	}
	return float64(hit)/float64(total) >= minCoverageRatio
}

// CoverageResult 是覆盖判定的结果（进 ProbeReport，让判定可见）。
type CoverageResult struct {
	// Required 是参与判定的码位数。
	Required int
	// Hit 是命中的码位数。
	Hit int
	// Missing 是缺失的码位（Required-Hit 个）。
	Missing []rune
	// Supplemented 表示是否因存在缺口而触发了字形级补充链（追加嵌入，C 卡 §4）。
	Supplemented bool
}

// String 返回可打印的判定文本。
func (c *CoverageResult) String() string {
	if c == nil {
		return "<未做覆盖判定>"
	}
	s := strings.Builder{}
	s.WriteString("coverage ")
	s.WriteString(itoa(c.Hit))
	s.WriteString("/")
	s.WriteString(itoa(c.Required))
	if c.Supplemented {
		s.WriteString(" supplemented=true")
	}
	if len(c.Missing) > 0 {
		s.WriteString(" missing=")
		for i, r := range c.Missing {
			if i > 0 {
				s.WriteString(",")
			}
			s.WriteString("U+")
			s.WriteString(hexUpper(r))
		}
	}
	return s.String()
}

// itoa 是小整数转字符串（避免为诊断文本引入 strconv 的格式化开销）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// hexUpper 把 rune 转成大写十六进制（不带 U+ 前缀）。
func hexUpper(r rune) string {
	const digits = "0123456789ABCDEF"
	if r == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for r > 0 {
		i--
		buf[i] = digits[r&0xF]
		r >>= 4
	}
	return string(buf[i:])
}
