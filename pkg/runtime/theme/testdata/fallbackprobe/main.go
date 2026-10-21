// Command fallbackprobe 是 TASK-FONT-001-C 的**设计探针**（不是产品代码）。
//
// 目的：在冻结 C 卡之前，用实测回答三个问题：
//  1. Gio v0.10.3 的 Glyph.ID 位布局能否反解出 faceIdx 与 gid？
//  2. gid == 0 能否可靠判定「该 rune 在当前 shaper 里没有字形」？
//  3. 系统命中场景下，GB2312 外的罕用字与 emoji 的实际行为是什么？
//
// 放在 testdata/ 下：Go 工具链忽略该目录，不会进入 ./... 的包列表。
package main

import (
	"fmt"
	"os"
	"strings"

	giofont "gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/text"
	gotextfont "github.com/go-text/typesetting/font"
	"golang.org/x/image/math/fixed"

	"github.com/goxml/goxml/pkg/runtime/theme"
	"github.com/goxml/goxml/pkg/runtime/theme/embedded"
)

// Gio v0.10.3 text/shaper.go 的位布局（newGlyphID）。
// 这是**未导出的实现细节**，本探针复刻它纯粹为了观测；
// C 的正式实现必须用交叉校验证明解码结果可信。
const (
	facebits = 16
	sizebits = 16
	gidbits  = 64 - facebits - sizebits
)

func splitGlyphID(g text.GlyphID) (ppem fixed.Int26_6, faceIdx int, gid gotextfont.GID) {
	faceIdx = int(uint64(g) >> (gidbits + sizebits))
	rawPpem := (g & ((1<<sizebits - 1) << gidbits)) >> gidbits
	gid = gotextfont.GID(g) & (1<<gidbits - 1)
	return fixed.Int26_6(rawPpem), faceIdx, gid
}

func params() text.Parameters {
	return text.Parameters{
		PxPerEm:  fixed.Int26_6(16 * 64),
		MaxWidth: 1 << 30,
	}
}

// shapeRune 对单个 rune 做 shaping，返回解码结果与字形数。
func shapeRune(s *text.Shaper, r rune) (faceIdx int, gid gotextfont.GID, n int) {
	s.LayoutString(params(), string(r))
	var last text.Glyph
	for g, ok := s.NextGlyph(); ok; g, ok = s.NextGlyph() {
		last = g
		n++
	}
	if n == 0 {
		return -1, 0, 0
	}
	_, faceIdx, gid = splitGlyphID(last.ID)
	return faceIdx, gid, n
}

type nominalProbe func(r rune) (gotextfont.GID, bool)

func makeProbe(faces []giofont.FontFace) nominalProbe {
	if len(faces) == 0 {
		return nil
	}
	return func(r rune) (gotextfont.GID, bool) {
		return faces[0].Face.Face().NominalGlyph(r)
	}
}

var cases = []struct {
	r    rune
	note string
}{
	{'A', "ASCII（覆盖内）"},
	{'é', "拉丁扩展（ASCII 外）"},
	{'世', "GB2312 内汉字"},
	{'汉', "GB2312 内汉字"},
	{'〇', "GB2312 内"},
	{'𠀋', "U+2000B 罕用字（GB2312 外）"},
	{'鿿', "U+9FFF 边界（GB2312 外）"},
	{'😀', "U+1F600 emoji"},
	{'→', "U+2192 箭头"},
	{'★', "U+2605 符号"},
}

func dump(title string, s *text.Shaper, probe nominalProbe) {
	fmt.Printf("\n=== %s ===\n", title)
	fmt.Printf("%-10s %-10s %-10s %-4s %-9s %s\n", "rune", "cp", "gid", "g#", "nominal", "note")
	for _, c := range cases {
		_, gid, n := shapeRune(s, c.r)
		nom := "-"
		if probe != nil {
			if ngid, ok := probe(c.r); ok {
				nom = fmt.Sprintf("%d", ngid)
			} else {
				nom = "MISS"
			}
		}
		fmt.Printf("%-10q U+%06X   %-10d %-4d %-9s %s\n", c.r, c.r, gid, n, nom, c.note)
	}
}

func main() {
	fmt.Printf("Gio glyph ID 位布局: facebits=%d sizebits=%d gidbits=%d\n", facebits, sizebits, gidbits)
	var sysFaces []giofont.FontFace

	// ---- 场景 1：仅嵌入子集（禁用系统字体），覆盖清单已知 ----
	assets, err := embedded.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "嵌入资产加载失败:", err)
		os.Exit(1)
	}
	faces, err := opentype.ParseCollection(assets.Font)
	if err != nil {
		fmt.Fprintln(os.Stderr, "嵌入字体解析失败:", err)
		os.Exit(1)
	}
	fmt.Printf("嵌入 faces=%d bytes=%d\n", len(faces), len(assets.Font))
	dump("场景1 仅嵌入子集（NoSystemFonts）",
		text.NewShaper(text.NoSystemFonts(), text.WithCollection(faces)),
		makeProbe(faces))

	// ---- 场景 2：默认 NewTheme()（系统命中 + 系统字体库）----
	th, rep := theme.NewThemeWithReport()
	fmt.Printf("\n诊断: %s\n", rep.String())

	sysFaces = nil
	p := theme.NewProvider(theme.ProviderOptions{})
	id, _, err := p.ResolveWithReport(theme.FamilyCJK)
	if err != nil {
		fmt.Println("系统解析失败:", err)
	} else {
		sysFaces, _ = p.Faces(id)
		fmt.Printf("系统集合 id=%s faces=%d\n", id, len(sysFaces))
		dump("场景2 默认 NewTheme（系统字体 + collection）", th.Shaper, makeProbe(sysFaces))
	}

	// ---- 场景 3：裸 Gio Shaper（无任何 collection，纯系统字体库）----
	dump("场景3 裸 Gio Shaper（无 collection）", text.NewShaper(), nil)

	// ---- 场景 4/5：顺序实验 ----
	//
	// 用 gid 指纹识别命中了哪个字体：同一个 rune 在嵌入子集与 msyh 里的
	// GID 不同（"世" 嵌入=691 / msyh=1037），因此解码出的 gid 就是指纹。
	//
	//   - "世"：两者都有 → 看优先谁（证明顺序语义）
	//   - "〇"：嵌入没有、msyh 有 → 看是否 fallback 到第二个（证明缺字形才补）
	if len(sysFaces) > 0 {
		emb := make([]giofont.FontFace, 0, 1+len(sysFaces))
		emb = append(emb, faces...)
		emb = append(emb, sysFaces...)
		sys := make([]giofont.FontFace, 0, 1+len(sysFaces))
		sys = append(sys, sysFaces...)
		sys = append(sys, faces...)
		dump("场景4 collection=[嵌入, 系统]", text.NewShaper(text.WithCollection(emb)), nil)
		dump("场景5 collection=[系统, 嵌入]", text.NewShaper(text.WithCollection(sys)), nil)
		fmt.Printf("\n指纹对照: 世 嵌入=%s 系统=%s | 〇 嵌入=%s 系统=%s\n",
			nominalOf(faces, '世'), nominalOf(sysFaces, '世'),
			nominalOf(faces, '〇'), nominalOf(sysFaces, '〇'))

		// ---- 场景 6：覆盖增量统计 ----
		// 回答「系统命中时把嵌入追加进 collection 有没有收益」：
		// 只有「嵌入有、系统无」的码位才是真实收益。
		cps, err := parseCharset(assets.Charset)
		if err != nil {
			fmt.Println("字符集解析失败:", err)
			return
		}
		both, embOnly, sysOnly, neither := 0, 0, 0, 0
		var embOnlySample, neitherSample []rune
		for _, r := range cps {
			_, e := faces[0].Face.Face().NominalGlyph(r)
			_, s := sysFaces[0].Face.Face().NominalGlyph(r)
			switch {
			case e && s:
				both++
			case e && !s:
				embOnly++
				if len(embOnlySample) < 12 {
					embOnlySample = append(embOnlySample, r)
				}
			case !e && s:
				sysOnly++
			default:
				neither++
				if len(neitherSample) < 12 {
					neitherSample = append(neitherSample, r)
				}
			}
		}
		fmt.Printf("\n=== 场景6 覆盖增量（对照 charset.txt 共 %d 码位）===\n", len(cps))
		fmt.Printf("  两者都有      %d\n", both)
		fmt.Printf("  仅嵌入有      %d  ← 追加嵌入的真实收益\n", embOnly)
		fmt.Printf("  仅系统有      %d\n", sysOnly)
		fmt.Printf("  两者都无      %d\n", neither)
		if len(embOnlySample) > 0 {
			fmt.Printf("  仅嵌入有样本: %q\n", string(embOnlySample))
		}
		if len(neitherSample) > 0 {
			fmt.Printf("  两者都无样本: %q\n", string(neitherSample))
		}
	}
}

func nominalOf(faces []giofont.FontFace, r rune) string {
	if len(faces) == 0 {
		return "-"
	}
	if gid, ok := faces[0].Face.Face().NominalGlyph(r); ok {
		return fmt.Sprintf("%d", gid)
	}
	return "MISS"
}

// parseCharset 解析 charset.txt（每行 "U+XXXX"）。
func parseCharset(data []byte) ([]rune, error) {
	var out []rune
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "U+") {
			return nil, fmt.Errorf("行格式非法: %q", line)
		}
		var cp int64
		if _, err := fmt.Sscanf(line[2:], "%x", &cp); err != nil {
			return nil, fmt.Errorf("解析 %q 失败: %w", line, err)
		}
		out = append(out, rune(cp))
	}
	return out, nil
}
