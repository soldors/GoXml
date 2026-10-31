package theme

// FACE-SELECT 阶段一（同步 loader）测试。
//
// oracle 原则（不复制解析实现当 oracle）：
//   - 元数据：`gioot.DescriptionToFont(font.Describe())` —— 与 whole 路径同一转换；
//   - 字形：`font.Face.NominalGlyph`（cmap 直查，与回退无关）；
//   - 度量：`text.NoSystemFonts()` 隔离回退后塑形「ASCII+CJK 混排」的
//     Advance/Ascent/Descent/X/Y 序列。
//
// fixture 用 Go Regular + Go Bold 合成**可区分**双面 TTC（共享/复制表数据），
// 并先用 `gioot.ParseCollection` 验证其为合法集合并断言两面可区分。

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	giofont "gioui.org/font"
	gioot "gioui.org/font/opentype"
	"gioui.org/io/system"
	"gioui.org/text"
	fontapi "github.com/go-text/typesetting/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/math/fixed"
)

// ---------------------------------------------------------------------------
// fixture
// ---------------------------------------------------------------------------

// writeFileForTest 写临时字体文件（Path 源用例用）。
func writeFileForTest(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

func selTestAlign4(n int) int { return (n + 3) &^ 3 }

type selTestTable struct {
	tag      [4]byte
	checksum uint32
	srcOff   uint32
	length   uint32
}

func selTestParseTables(t *testing.T, f []byte) []selTestTable {
	t.Helper()
	if len(f) < 12 {
		t.Fatalf("字体过短：%d 字节", len(f))
	}
	n := int(binary.BigEndian.Uint16(f[4:6]))
	if n <= 0 || len(f) < 12+16*n {
		t.Fatalf("表目录非法：numTables=%d len=%d", n, len(f))
	}
	out := make([]selTestTable, 0, n)
	for i := 0; i < n; i++ {
		e := 12 + 16*i
		var tag [4]byte
		copy(tag[:], f[e:e+4])
		off := binary.BigEndian.Uint32(f[e+8 : e+12])
		ln := binary.BigEndian.Uint32(f[e+12 : e+16])
		if int64(off)+int64(ln) > int64(len(f)) {
			t.Fatalf("表 %q 越界：off=%d len=%d", string(tag[:]), off, ln)
		}
		out = append(out, selTestTable{
			tag: tag, checksum: binary.BigEndian.Uint32(f[e+4 : e+8]), srcOff: off, length: ln,
		})
	}
	return out
}

// selTestBuildTTC 用给定字体合成合法 TTC（每个字体各自一份表数据）。
func selTestBuildTTC(t *testing.T, fonts [][]byte) []byte {
	t.Helper()
	if len(fonts) < 1 {
		t.Fatalf("至少 1 个字体，实际 %d", len(fonts))
	}
	perFont := make([][]selTestTable, len(fonts))
	dirOffs := make([]int, len(fonts))
	headerSize := 12 + 4*len(fonts)
	cursor := headerSize
	for i, f := range fonts {
		perFont[i] = selTestParseTables(t, f)
		dirOffs[i] = cursor
		cursor += 12 + 16*len(perFont[i])
	}
	dataStart := selTestAlign4(cursor)

	dstOffs := make([][]uint32, len(fonts))
	total := dataStart
	for i := range fonts {
		dstOffs[i] = make([]uint32, len(perFont[i]))
		for j, tb := range perFont[i] {
			dstOffs[i][j] = uint32(total)
			total = selTestAlign4(total + int(tb.length))
		}
	}

	out := make([]byte, total)
	copy(out[0:4], "ttcf")
	binary.BigEndian.PutUint32(out[4:8], 0x00010000)
	binary.BigEndian.PutUint32(out[8:12], uint32(len(fonts)))
	for i := range fonts {
		binary.BigEndian.PutUint32(out[12+4*i:16+4*i], uint32(dirOffs[i]))
		for j, tb := range perFont[i] {
			d := dstOffs[i][j]
			copy(out[d:d+tb.length], fonts[i][tb.srcOff:tb.srcOff+tb.length])
		}
		dir := dirOffs[i]
		copy(out[dir:dir+12], fonts[i][0:12])
		for j, tb := range perFont[i] {
			e := dir + 12 + 16*j
			copy(out[e:e+4], tb.tag[:])
			binary.BigEndian.PutUint32(out[e+4:e+8], tb.checksum)
			binary.BigEndian.PutUint32(out[e+8:e+12], dstOffs[i][j])
			binary.BigEndian.PutUint32(out[e+12:e+16], tb.length)
		}
	}

	got, err := gioot.ParseCollection(out)
	if err != nil {
		t.Fatalf("合成 TTC 无法被 Gio 解析: %v", err)
	}
	if len(got) != len(fonts) {
		t.Fatalf("fixture 面数 = %d，期望 %d", len(got), len(fonts))
	}
	return out
}

// selTestDistinctTTC 返回 Regular+Bold 双面 TTC，并断言两面可区分（字重与度量）。
func selTestDistinctTTC(t *testing.T) []byte {
	t.Helper()
	buf := selTestBuildTTC(t, [][]byte{goregular.TTF, gobold.TTF})
	whole, err := gioot.ParseCollection(buf)
	if err != nil {
		t.Fatalf("whole 解析失败: %v", err)
	}
	if len(whole) != 2 {
		t.Fatalf("面数 = %d，期望 2", len(whole))
	}
	if whole[0].Font.Weight == whole[1].Font.Weight {
		t.Fatalf("fixture 两面字重相同（%v）：无法检出取错面", whole[0].Font.Weight)
	}
	if strings.Join(selTestMetrics(t, whole[0]), "|") == strings.Join(selTestMetrics(t, whole[1]), "|") {
		t.Fatal("fixture 两面在隔离回退下的塑形度量完全相同：无法检出取错面")
	}
	return buf
}

// selTestCorruptTable 复制并破坏指定表前 n 字节（表目录保持合法）。
func selTestCorruptTable(t *testing.T, f []byte, tag string, n int) []byte {
	t.Helper()
	cp := append([]byte(nil), f...)
	tbls := selTestParseTables(t, cp)
	var off, ln uint32
	found := false
	for _, tb := range tbls {
		if string(tb.tag[:]) == tag {
			off, ln, found = tb.srcOff, tb.length, true
			break
		}
	}
	if !found {
		t.Fatalf("表 %q 不存在", tag)
	}
	if int(ln) < n {
		t.Fatalf("表 %q 太短（%d）", tag, ln)
	}
	for i := 0; i < n; i++ {
		cp[int(off)+i] = 0xAB
	}
	return cp
}

// selTestMetrics 用隔离回退的 Shaper 塑形混排文本，返回每字形度量串。
func selTestMetrics(t *testing.T, ff giofont.FontFace) []string {
	t.Helper()
	sh := text.NewShaper(text.NoSystemFonts(), text.WithCollection([]giofont.FontFace{ff}))
	sh.LayoutString(text.Parameters{
		PxPerEm: fixed.I(16), MaxWidth: 1 << 20, Locale: system.Locale{},
	}, "Goxml abc 123 中文测试")
	var out []string
	for {
		g, ok := sh.NextGlyph()
		if !ok {
			break
		}
		out = append(out, strings.Join([]string{
			selTestItoa(int(g.Advance)), selTestItoa(int(g.Ascent)),
			selTestItoa(int(g.Descent)), selTestItoa(int(g.X)), selTestItoa(int(g.Y)),
		}, ","))
	}
	return out
}

func selTestItoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var d [20]byte
	i := len(d)
	for n > 0 {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(d[i:])
	if neg {
		return "-" + s
	}
	return s
}

// selTestAssertSameFace 断言 selected 面与 whole 对应面元数据/字形/度量等价。
func selTestAssertSameFace(t *testing.T, label string, want, got giofont.FontFace) {
	t.Helper()
	if want.Font.Typeface != got.Font.Typeface {
		t.Errorf("%s: Typeface %q != %q", label, got.Font.Typeface, want.Font.Typeface)
	}
	if want.Font.Style != got.Font.Style {
		t.Errorf("%s: Style %v != %v", label, got.Font.Style, want.Font.Style)
	}
	if want.Font.Weight != got.Font.Weight {
		t.Errorf("%s: Weight %v != %v", label, got.Font.Weight, want.Font.Weight)
	}
	wf, gf := want.Face.Face(), got.Face.Face()
	if a, b := wf.Upem(), gf.Upem(); a != b {
		t.Errorf("%s: Upem %d != %d", label, b, a)
	}
	sample := append(RequiredRunes(), []rune("Goxml abc 123 ABC")...)
	for _, r := range sample {
		wg, wok := wf.NominalGlyph(r)
		gg, gok := gf.NominalGlyph(r)
		if wok != gok {
			t.Errorf("%s: rune %q 存在性不同: whole=%v selected=%v", label, r, wok, gok)
			continue
		}
		if wok && wg != gg {
			t.Errorf("%s: rune %q GID 不同: %d vs %d", label, r, wg, gg)
		}
	}
	wm, gm := selTestMetrics(t, want), selTestMetrics(t, got)
	if strings.Join(wm, "|") != strings.Join(gm, "|") {
		t.Errorf("%s: 塑形度量不同", label)
	}
}

// selTestErrKind 取出 SelectionError 并返回其 ErrorKind。
func selTestErrKind(t *testing.T, err error) (string, SelectionReport) {
	t.Helper()
	var se *SelectionError
	if !errors.As(err, &se) {
		t.Fatalf("错误不是 *SelectionError，实际 %T: %v", err, err)
	}
	return se.Report.ErrorKind, se.Report
}

// ---------------------------------------------------------------------------
// 配置错误全族
// ---------------------------------------------------------------------------

func TestSelectionInvalidConfigFamily(t *testing.T) {
	ttf := goregular.TTF
	cases := []struct {
		name string
		sel  FaceSelection
	}{
		{"Bytes 与 Path 同时设置", FaceSelection{Source: FontSource{Bytes: ttf, Path: `C:\x.ttf`, Name: "n"}, Indices: []int{0}}},
		{"Bytes 已设置但为空", FaceSelection{Source: FontSource{Bytes: []byte{}, Name: "n"}, Indices: []int{0}}},
		{"Bytes 已设置但为空且给了 Path", FaceSelection{Source: FontSource{Bytes: []byte{}, Path: `C:\x.ttf`, Name: "n"}, Indices: []int{0}}},
		{"既无 Bytes 也无 Path", FaceSelection{Source: FontSource{Name: "n"}, Indices: []int{0}}},
		{"Bytes 源缺 Name", FaceSelection{Source: FontSource{Bytes: ttf}, Indices: []int{0}}},
		{"Indices 为 nil", FaceSelection{Source: FontSource{Bytes: ttf, Name: "n"}}},
		{"Indices 为空切片", FaceSelection{Source: FontSource{Bytes: ttf, Name: "n"}, Indices: []int{}}},
		{"索引为负", FaceSelection{Source: FontSource{Bytes: ttf, Name: "n"}, Indices: []int{-1}}},
		{"索引重复", FaceSelection{Source: FontSource{Bytes: ttf, Name: "n"}, Indices: []int{0, 0}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := LoadSelectedFaces(c.sel)
			if err == nil {
				t.Fatal("应返回配置错误")
			}
			kind, _ := selTestErrKind(t, err)
			if kind != SelectionInvalidConfig {
				t.Errorf("ErrorKind = %q，期望 %q", kind, SelectionInvalidConfig)
			}
			// 配置错误发生在构造期（未读源）：Ensure 底层错误可 errors.Is/As 追踪。
			var se *SelectionError
			if errors.As(err, &se) && se.Err == nil {
				t.Error("SelectionError.Err 不应为 nil（需保留原因链）")
			}
		})
	}
}

// TestSelectionOutOfRangeIsLoadTime 证明越界是**加载期**错误（构造期不偷读）。
func TestSelectionOutOfRangeIsLoadTime(t *testing.T) {
	sel := FaceSelection{Source: FontSource{Bytes: goregular.TTF, Name: "regular"}, Indices: []int{1}}
	// 纯配置校验阶段无法知道面数，因此 freezeSelection 必须成功。
	if _, err := freezeSelection(sel); err != nil {
		t.Fatalf("freezeSelection 不应在配置阶段拒绝越界索引：%v", err)
	}
	// 读源之后才报越界。
	_, err := LoadSelectedFaces(sel)
	if err == nil {
		t.Fatal("单字体文件选择 index 1 应报越界")
	}
	kind, rep := selTestErrKind(t, err)
	if kind != SelectionInvalidConfig {
		t.Errorf("ErrorKind = %q，期望 %q", kind, SelectionInvalidConfig)
	}
	if rep.ErrorPhase != "indices" {
		t.Errorf("ErrorPhase = %q，期望 indices", rep.ErrorPhase)
	}
	if rep.DeclaredFaces != 1 {
		t.Errorf("DeclaredFaces = %d，期望 1", rep.DeclaredFaces)
	}
}

// ---------------------------------------------------------------------------
// 单字体 / 非零 / 逆序 / 等价性
// ---------------------------------------------------------------------------

func TestSelectionSingleFontIndexZero(t *testing.T) {
	whole, err := gioot.ParseCollection(goregular.TTF)
	if err != nil {
		t.Fatalf("whole 失败: %v", err)
	}
	got, err := LoadSelectedFaces(FaceSelection{
		Source: FontSource{Bytes: goregular.TTF, Name: "regular"}, Indices: []int{0},
	})
	if err != nil {
		t.Fatalf("选中单字体 index 0 失败: %v", err)
	}
	faces := got.Faces()
	if len(faces) != 1 {
		t.Fatalf("面数 = %d，期望 1", len(faces))
	}
	selTestAssertSameFace(t, "single-0", whole[0], faces[0])
	rep := got.Report()
	if !rep.SelectionApplied || rep.DeclaredFaces != 1 {
		t.Errorf("报告不符: applied=%v declared=%d", rep.SelectionApplied, rep.DeclaredFaces)
	}
}

func TestSelectionNonZeroAndReverseOrder(t *testing.T) {
	buf := selTestDistinctTTC(t)
	whole, err := gioot.ParseCollection(buf)
	if err != nil {
		t.Fatalf("whole 失败: %v", err)
	}

	// 非零单选
	got1, err := LoadSelectedFaces(FaceSelection{
		Source: FontSource{Bytes: buf, Name: "distinct"}, Indices: []int{1},
	})
	if err != nil {
		t.Fatalf("选中 index 1 失败: %v", err)
	}
	f1 := got1.Faces()
	if len(f1) != 1 {
		t.Fatalf("面数 = %d", len(f1))
	}
	if f1[0].Font.Weight != whole[1].Font.Weight {
		t.Fatalf("selected[1] 字重 %v != whole[1] %v：取错面", f1[0].Font.Weight, whole[1].Font.Weight)
	}
	if f1[0].Font.Weight == whole[0].Font.Weight {
		t.Fatal("selected[1] 与 whole[0] 同重：索引映射错位未被检出")
	}
	selTestAssertSameFace(t, "index-1", whole[1], f1[0])

	// 逆序 [1,0]
	got2, err := LoadSelectedFaces(FaceSelection{
		Source: FontSource{Bytes: buf, Name: "distinct"}, Indices: []int{1, 0},
	})
	if err != nil {
		t.Fatalf("逆序选中失败: %v", err)
	}
	f2 := got2.Faces()
	rep2 := got2.Report()
	if len(f2) != 2 {
		t.Fatalf("面数 = %d，期望 2", len(f2))
	}
	if len(rep2.ActualIndices) != 2 || rep2.ActualIndices[0] != 1 || rep2.ActualIndices[1] != 0 {
		t.Fatalf("ActualIndices = %v，期望 [1 0]", rep2.ActualIndices)
	}
	selTestAssertSameFace(t, "rev-0=whole1", whole[1], f2[0])
	selTestAssertSameFace(t, "rev-1=whole0", whole[0], f2[1])
	if f2[0].Font.Weight == whole[0].Font.Weight {
		t.Fatal("逆序请求首面与 whole[0] 同重：顺序被忽略")
	}
}

// ---------------------------------------------------------------------------
// 覆盖语义
// ---------------------------------------------------------------------------

func TestSelectionCoverageRecordingAndRejection(t *testing.T) {
	// Go Regular 是纯拉丁字体：对 CJK 抽样覆盖必然不足。
	latin := FontSource{Bytes: goregular.TTF, Name: "latin"}

	// 默认（RequireCoverage=false）：记录事实但不拒绝。
	ok, err := LoadSelectedFaces(FaceSelection{Source: latin, Indices: []int{0}})
	if err != nil {
		t.Fatalf("默认策略不应拒绝 Latin 源: %v", err)
	}
	rep := ok.Report()
	if !rep.CoverageChecked {
		t.Error("CoverageChecked 应为 true（已检查）")
	}
	if rep.CoverageTotal != len(RequiredRunes()) {
		t.Errorf("CoverageTotal = %d，期望 %d", rep.CoverageTotal, len(RequiredRunes()))
	}
	if rep.CoverageHit >= rep.CoverageTotal {
		t.Errorf("Latin 源不应达到满覆盖：hit=%d total=%d", rep.CoverageHit, rep.CoverageTotal)
	}
	if len(rep.CoverageMissing) == 0 {
		t.Error("应记录缺失码位")
	}

	// RequireCoverage=true：以 coverage-insufficient 拒绝。
	_, err = LoadSelectedFaces(FaceSelection{Source: latin, Indices: []int{0}, RequireCoverage: true})
	if err == nil {
		t.Fatal("RequireCoverage=true 时覆盖不足应拒绝")
	}
	kind, r2 := selTestErrKind(t, err)
	if kind != SelectionCoverage {
		t.Errorf("ErrorKind = %q，期望 %q", kind, SelectionCoverage)
	}
	if r2.SelectionApplied {
		t.Error("失败报告不得标记 SelectionApplied")
	}
	if len(r2.ActualIndices) != 0 || len(r2.Meta) != 0 {
		t.Error("失败报告不得包含实际索引/元数据")
	}
	// 覆盖事实仍应保留（"未检查"与"已检查零命中"可分）。
	if !r2.CoverageChecked {
		t.Error("失败报告仍应记录“已检查”")
	}
}

// ---------------------------------------------------------------------------
// 冻结与副本隔离
// ---------------------------------------------------------------------------

func TestSelectionFreezeAndCopyIsolation(t *testing.T) {
	buf := selTestDistinctTTC(t)
	indices := []int{1, 0}
	src := append([]byte(nil), buf...)

	got, err := LoadSelectedFaces(FaceSelection{
		Source: FontSource{Bytes: src, Name: "distinct"}, Indices: indices,
	})
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	before := got.Report()

	// 调用方篡改自己的 Indices 与 Bytes。
	indices[0], indices[1] = 0, 1
	for i := range src {
		src[i] = 0
	}

	after := got.Report()
	if len(after.ActualIndices) != 2 || after.ActualIndices[0] != 1 || after.ActualIndices[1] != 0 {
		t.Errorf("篡改输入后 ActualIndices 变了：%v", after.ActualIndices)
	}
	if len(before.Meta) != len(after.Meta) || before.Meta[0].Weight != after.Meta[0].Weight {
		t.Error("篡改输入后元数据变了")
	}
	// 破坏调用方字节后，已解析的字形仍应可用（证明不别名调用方切片）。
	faces := got.Faces()
	if len(faces) != 2 {
		t.Fatalf("面数 = %d", len(faces))
	}
	if _, ok := faces[0].Face.Face().NominalGlyph('A'); !ok {
		t.Error("破坏调用方字节后字形不可用：说明字体对象仍别名输入切片")
	}
	if len(selTestMetrics(t, faces[0])) == 0 {
		t.Error("破坏调用方字节后塑形无输出")
	}

	// Faces() 返回副本：改返回值不影响内部。
	out := got.Faces()
	original := out[0]
	out[0] = giofont.FontFace{}
	if first := got.Faces()[0]; first != original {
		t.Error("Faces() 未返回副本（改动传播回内部）")
	}

	// Report() 返回副本：改 Missing 不影响内部。
	r := got.Report()
	if len(r.CoverageMissing) > 0 {
		r.CoverageMissing[0] = 'x'
		if rr := got.Report(); len(rr.CoverageMissing) > 0 && rr.CoverageMissing[0] == 'x' {
			t.Error("Report() 未深拷贝 CoverageMissing")
		}
	}
}

// TestSelectionFaceWrappersIndependent 验证每次 Face() 产生独立可变 Face。
func TestSelectionFaceWrappersIndependent(t *testing.T) {
	got, err := LoadSelectedFaces(FaceSelection{
		Source: FontSource{Bytes: goregular.TTF, Name: "regular"}, Indices: []int{0},
	})
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	ff := got.Faces()[0]
	a, b := ff.Face.Face(), ff.Face.Face()
	if a == b {
		t.Fatal("两次 Face() 返回同一指针：可变 Face 被共享")
	}
	ga, oka := a.NominalGlyph('A')
	gb, okb := b.NominalGlyph('A')
	if oka != okb || ga != gb {
		t.Errorf("两个包装结果不一致: (%d,%v) vs (%d,%v)", ga, oka, gb, okb)
	}
}

// ---------------------------------------------------------------------------
// 解析失败 / panic 收敛
// ---------------------------------------------------------------------------

func TestSelectionCorruptLocaPanicContained(t *testing.T) {
	bad := selTestCorruptTable(t, goregular.TTF, "loca", 8)
	var (
		got *SelectedFaces
		err error
		pan any
	)
	func() {
		defer func() { pan = recover() }()
		got, err = LoadSelectedFaces(FaceSelection{
			Source: FontSource{Bytes: bad, Name: "corrupt-loca"}, Indices: []int{0},
		})
	}()
	if pan != nil {
		t.Fatalf("panic 逃出加载入口（适配层未收敛）：%v", pan)
	}
	if err == nil {
		t.Fatal("损坏 loca 应返回错误")
	}
	if got != nil {
		t.Fatal("失败时不得返回结果（无半成品）")
	}
	kind, rep := selTestErrKind(t, err)
	if kind != SelectionParserPanic {
		t.Errorf("ErrorKind = %q，期望 %q", kind, SelectionParserPanic)
	}
	if !rep.FaceIndexKnown || rep.FaceIndex != 0 {
		t.Errorf("应给出可确定的面索引 0，实际 known=%v idx=%d", rep.FaceIndexKnown, rep.FaceIndex)
	}
	var se *SelectionError
	if errors.As(err, &se) {
		if se.Err == nil || !strings.Contains(se.Err.Error(), "panic") {
			t.Errorf("原因链应保留 panic 描述，实际 %v", se.Err)
		}
	}
}

func TestSelectionCorruptCmapRejected(t *testing.T) {
	bad := selTestCorruptTable(t, goregular.TTF, "cmap", 8)
	_, err := LoadSelectedFaces(FaceSelection{
		Source: FontSource{Bytes: bad, Name: "corrupt-cmap"}, Indices: []int{0},
	})
	if err == nil {
		t.Fatal("cmap（必需表）损坏应报错")
	}
	kind, _ := selTestErrKind(t, err)
	// cmap 是 go-text 文档列明的必需表：损坏应表现为**普通解析错误**（parse），
	// 不是 parser-panic。R1 曾把两者混为一谈，此处收紧为单一期望。
	if kind != SelectionParse {
		t.Errorf("ErrorKind = %q，期望 %q（必需表损坏属普通解析错误）", kind, SelectionParse)
	}
}

// TestSelectionSourceIOError 验证 Path 源读取失败 -> source-io。
func TestSelectionSourceIOError(t *testing.T) {
	_, err := LoadSelectedFaces(FaceSelection{
		Source: FontSource{Path: `C:\__goxml_absent__\nope.ttf`}, Indices: []int{0},
	})
	if err == nil {
		t.Fatal("不存在的路径应报错")
	}
	kind, _ := selTestErrKind(t, err)
	if kind != SelectionSourceIO {
		t.Errorf("ErrorKind = %q，期望 %q", kind, SelectionSourceIO)
	}
}

// TestSelectionPathSourceAndLabel 验证 Path 源可用且 label 取 basename。
func TestSelectionPathSourceAndLabel(t *testing.T) {
	dir := t.TempDir()
	p := dir + `\distinct.ttc`
	if err := writeFileForTest(p, selTestDistinctTTC(t)); err != nil {
		t.Fatalf("写临时字体失败: %v", err)
	}
	got, err := LoadSelectedFaces(FaceSelection{Source: FontSource{Path: p}, Indices: []int{1}})
	if err != nil {
		t.Fatalf("Path 源加载失败: %v", err)
	}
	rep := got.Report()
	if rep.SourceName != "distinct.ttc" {
		t.Errorf("SourceName = %q，期望 basename distinct.ttc", rep.SourceName)
	}
	if !strings.Contains(rep.Label, "#faces=1") {
		t.Errorf("Label = %q，应含 #faces=1", rep.Label)
	}
}

// TestSelectionThemeIsIndependent 验证 Theme() 每次构造独立主题，不共享 Shaper。
func TestSelectionThemeIsIndependent(t *testing.T) {
	got, err := LoadSelectedFaces(FaceSelection{
		Source: FontSource{Bytes: goregular.TTF, Name: "regular"}, Indices: []int{0},
	})
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	a, b := got.Theme(), got.Theme()
	if a == nil || b == nil {
		t.Fatal("Theme() 不应返回 nil")
	}
	if a == b {
		t.Fatal("两次 Theme() 返回同一实例：mutable Theme 被共享")
	}
	if a.Shaper == b.Shaper {
		t.Fatal("两次 Theme() 共享同一个 Shaper：渲染线程间会互相污染")
	}
	// 系统回退口径与 newThemeWithFaces 一致（非 NoSystemFonts）。
	if a.Shaper == nil {
		t.Fatal("Theme().Shaper 不应为 nil")
	}
}

// ---------------------------------------------------------------------------
// R1.1 回归：普通解析错误 vs 真实 recover panic 的类型化区分
// ---------------------------------------------------------------------------

// selTestRenameTable 把指定表的 tag 改成新 tag（目录结构、偏移、校验和保持不变）。
//
// 这样 NewLoaders 仍能成功（只读头部/目录），而 NewFont 会因为找不到必需表
// 返回**普通 error**（不 panic）——正是区分"缺表"与"解析器 panic"的判别输入。
func selTestRenameTable(t *testing.T, f []byte, from, to string) []byte {
	t.Helper()
	if len(from) != 4 || len(to) != 4 {
		t.Fatalf("tag 必须是 4 字节：%q -> %q", from, to)
	}
	cp := append([]byte(nil), f...)
	n := int(cp[4])<<8 | int(cp[5])
	if n <= 0 || len(cp) < 12+16*n {
		t.Fatalf("表目录非法：numTables=%d", n)
	}
	for i := 0; i < n; i++ {
		e := 12 + 16*i
		if string(cp[e:e+4]) == from {
			copy(cp[e:e+4], []byte(to))
			return cp
		}
	}
	t.Fatalf("表 %q 不存在", from)
	return nil
}

// TestSelectionMissingRequiredTableIsParse 是 P0-1 的判别性回归。
//
// 事实链（不复制解析实现）：
//  1. 把 Go Regular 的 cmap 表改名为 zzzz；
//  2. 直接调用 fontapi.NewFont 必须返回**普通 error**且不 panic（独立事实）；
//  3. 因此 LoadSelectedFaces 必须归类为 SelectionParse（不是 parser-panic）。
func TestSelectionMissingRequiredTableIsParse(t *testing.T) {
	for _, tag := range []string{"cmap", "maxp"} {
		t.Run(tag, func(t *testing.T) {
			bad := selTestRenameTable(t, goregular.TTF, tag, "zzzz")

			// 独立事实：NewLoaders 仍成功，NewFont 返回普通错误、不 panic。
			loaders, lerr := newSelLoaders(bad)
			if lerr != nil {
				t.Fatalf("改名 %q 后 NewLoaders 不应失败：%v", tag, lerr)
			}
			if len(loaders) != 1 {
				t.Fatalf("loaders = %d，期望 1", len(loaders))
			}
			var pan any
			var directErr error
			func() {
				defer func() { pan = recover() }()
				_, directErr = fontapi.NewFont(loaders[0])
			}()
			if pan != nil {
				t.Fatalf("改名 %q 后 NewFont 不应 panic，实际 panic：%v", tag, pan)
			}
			if directErr == nil {
				t.Fatalf("改名 %q 后 NewFont 应返回普通错误", tag)
			}

			// 结论：LoadSelectedFaces 必须归为 parse（R1 会误报 parser-panic）。
			_, err := LoadSelectedFaces(FaceSelection{
				Source: FontSource{Bytes: bad, Name: "rename-" + tag}, Indices: []int{0},
			})
			if err == nil {
				t.Fatal("缺必需表应返回错误")
			}
			kind, _ := selTestErrKind(t, err)
			if kind != SelectionParse {
				t.Errorf("ErrorKind = %q，期望 %q（普通解析错误不得被报成 parser-panic）", kind, SelectionParse)
			}
			// 错误链保留底层原因（不伪造身份，只验证可追踪）。
			var se *SelectionError
			if !errors.As(err, &se) {
				t.Fatal("应可用 errors.As 取回 *SelectionError")
			}
			if se.Err == nil {
				t.Error("SelectionError.Err 不应为 nil")
			}
			if !strings.Contains(se.Err.Error(), "missing") && !strings.Contains(se.Err.Error(), "cmap") &&
				!strings.Contains(se.Err.Error(), "maxp") {
				t.Errorf("原因链应保留底层缺表信息，实际 %v", se.Err)
			}
		})
	}
}

// TestSelectionParseErrClassification 直接覆盖私有分类 helper 的判别力。
//
// 两条都是"不看文本、只看类型"的关键：
//  1. 普通 error 的文本里出现 panic 一词，仍必须是 parse；
//  2. 被包装的私有 selPanicError 必须仍能由 errors.As 检出 ⇒ parser-panic。
func TestSelectionParseErrClassification(t *testing.T) {
	// ① 文本含 panic 的普通错误：不得因此升级为 parser-panic。
	plain := errors.New("upstream said: panic while decoding (but this is a plain error)")
	if got := selClassifyParseErr(plain); got != SelectionParse {
		t.Errorf("文本含 panic 的普通错误被归为 %q，期望 %q", got, SelectionParse)
	}
	// 同理：被 fmt.Errorf 包装的普通错误。
	plainWrapped := fmt.Errorf("loaders: %w", plain)
	if got := selClassifyParseErr(plainWrapped); got != SelectionParse {
		t.Errorf("包装后的普通错误被归为 %q，期望 %q", got, SelectionParse)
	}

	// ② 真正的私有 panic 错误（含再包一层）必须被识别。
	pe := &selPanicError{phase: selPhaseFont, faceIdx: 2, faceKnown: true, reason: "boom"}
	if got := selClassifyParseErr(pe); got != SelectionParserPanic {
		t.Errorf("直接 selPanicError 被归为 %q，期望 %q", got, SelectionParserPanic)
	}
	wrapped := fmt.Errorf("wrap: %w", pe)
	if got := selClassifyParseErr(wrapped); got != SelectionParserPanic {
		t.Errorf("被包装的 selPanicError 被归为 %q，期望 %q", got, SelectionParserPanic)
	}
	if found, ok := selIsPanicErr(wrapped); !ok || found != pe {
		t.Error("errors.As 应能取回同一个 selPanicError")
	}
	// ③ 私有类型不导出：对外只通过 ErrorKind 观察（此处仅断言其 error 文本可用）。
	if pe.Error() == "" {
		t.Error("selPanicError.Error() 不应为空")
	}
}
