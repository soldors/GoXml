package theme

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gioui.org/text"
	"gioui.org/widget/material"
	"golang.org/x/image/math/fixed"

	"github.com/goxml/goxml/contracts"
	platformfont "github.com/goxml/goxml/pkg/platform/font"
)

// ============================================================================
// TASK-FONT-001-A 的验收矩阵（A 卡 §5）
//
// 证据等级分三档，测试名与 t.Skip 文案里都必须写清楚：
//   1. 逻辑——任何平台都跑（注入候选/注入 Read/注入 GOOS）；
//   2. 实测——仅当前真机（Windows）；
//   3. 待验证——macOS / Linux 真机，本机无条件，**不得**宣称通过。
// ============================================================================

// writeFont 把一个真实字体写到临时目录，供"解析失败后继续"等用例使用。
func writeFont(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("写入 %s 失败: %v", p, err)
	}
	return p
}

// hanziFontBytes 返回一个**含汉字**的字体字节，供"系统命中"类用例当候选。
//
// 为什么不能用 goregular：它是纯拉丁字体，一个汉字都没有。C 卡 §3 引入按字形
// 覆盖判定后，它不再算"可用的 CJK 系统字体"，用它当"good 候选"会让
// "系统命中"的前提悄悄失效。需要纯拉丁字体做反面样本时，见 fallback_test.go。
func hanziFontBytes(t *testing.T) []byte {
	t.Helper()
	return loadEmbedded(t).Font
}

// TestFontProbeCandidatePaths 三平台候选表（A 卡 §5 第 1 项）。
func TestFontProbeCandidatePaths(t *testing.T) {
	win := platformfont.WindowsCJKCandidates()
	dar := platformfont.DarwinCJKCandidates()
	lin := platformfont.LinuxCJKCandidates()

	for goos, list := range map[string][]string{"windows": win, "darwin": dar, "linux": lin} {
		if len(list) == 0 {
			t.Fatalf("%s 候选表为空", goos)
		}
		for _, p := range list {
			// 注意：不能用宿主机的 filepath.IsAbs —— Windows 上 "/usr/..." 会被判为
			// 非绝对，而它在 POSIX 平台上是绝对的。按各自平台规则判定。
			if goos == "windows" {
				if !filepath.IsAbs(p) {
					t.Errorf("%s 候选 %q 不是绝对路径", goos, p)
				}
			} else if !strings.HasPrefix(p, "/") {
				t.Errorf("%s 候选 %q 不是绝对路径", goos, p)
			}
			// 规范 §2.3：外部命令只能作诊断，不得作为实现路径。
			if strings.Contains(p, "fc-match") || strings.HasPrefix(p, "$(") {
				t.Errorf("%s 候选 %q 依赖外部命令作为实现路径（规范 §2.3 禁止）", goos, p)
			}
		}
	}

	if !strings.HasSuffix(win[0], "msyh.ttc") {
		t.Errorf("Windows 首选应为 msyh.ttc（规范 §2.3），实际 %q", win[0])
	}
	if !strings.Contains(dar[0], "PingFang.ttc") {
		t.Errorf("macOS 首选应为 PingFang.ttc，实际 %q", dar[0])
	}
	if !strings.Contains(lin[0], "NotoSansCJK") {
		t.Errorf("Linux 首选应为 NotoSansCJK，实际 %q", lin[0])
	}
	if got := platformfont.CandidatesFor("plan9"); got != nil {
		t.Errorf("未知平台应返回 nil，实际 %v", got)
	}
}

// TestFontProbeContinuesOnParseFailure 文件存在但解析失败 → 记录原因并继续
// （规范 §2.3、A 卡约束 4）。同时验证评审要点 1：标识能反查到真实集合。
func TestFontProbeContinuesOnParseFailure(t *testing.T) {
	dir := t.TempDir()
	bad := writeFont(t, dir, "bad.ttf", []byte("this is definitely not a font"))
	// 必须含汉字：C 卡 §3 之后，"能解析"不再是命中标准，还要有 CJK 覆盖。
	good := writeFont(t, dir, "good.otf", hanziFontBytes(t))

	reads := 0
	p := NewProvider(ProviderOptions{
		Candidates: []string{bad, good},
		Timeout:    time.Second,
		Read: func(path string) ([]byte, error) {
			reads++
			return os.ReadFile(path)
		},
	})

	id, rep, err := p.ResolveWithReport(FamilyCJK)
	if err != nil {
		t.Fatalf("首个候选解析失败后应继续并命中下一个，实际失败: %v\n诊断:\n%s", err, rep)
	}
	if reads != 2 {
		t.Errorf("应尝试两个候选，实际读取 %d 次", reads)
	}
	if len(rep.Candidates) != 2 {
		t.Fatalf("诊断应记录两个候选，实际 %d", len(rep.Candidates))
	}
	if rep.Candidates[0].Status != StatusParseFailed {
		t.Errorf("首个候选状态应为 parse_failed，实际 %s", rep.Candidates[0].Status)
	}
	if rep.Candidates[0].Err == nil {
		t.Error("解析失败必须记录原因（诊断要能回答为什么）")
	}
	if rep.Candidates[1].Status != StatusOK {
		t.Errorf("第二个候选应命中，实际 %s", rep.Candidates[1].Status)
	}
	if !strings.Contains(rep.String(), string(StatusParseFailed)) {
		t.Errorf("报告文本必须暴露失败状态:\n%s", rep)
	}

	// 评审要点 1：标识 → 已解析集合，不得重新扫描/重新解析。
	faces, ok := p.Faces(id)
	if !ok || len(faces) == 0 {
		t.Fatalf("标识 %q 反查不到字体集合", id)
	}
	if !strings.HasPrefix(id, "system:") {
		t.Errorf("标识格式应为 system:<文件名>，实际 %q", id)
	}
}

// TestFontProbeTimeoutAndDiagnostics 超时语义（A 卡约束 3、评审要点 2）。
//
// 这里断言的是**诚实的**超时语义：
//   - deadline 过后不再启动新候选（skipped_timeout）；
//   - 但已启动的同步读取**不会被取消**，因此 Elapsed 会超过 Timeout。
//
// 绝不能把"调用方不再等待"包装成"底层操作已终止"。
func TestFontProbeTimeoutAndDiagnostics(t *testing.T) {
	const slowRead = 40 * time.Millisecond

	p := NewProvider(ProviderOptions{
		Candidates: []string{"/a/slow.ttc", "/b/second.ttc", "/c/third.ttc"},
		Timeout:    5 * time.Millisecond,
		Read: func(path string) ([]byte, error) {
			if path == "/a/slow.ttc" {
				time.Sleep(slowRead)
			}
			return nil, os.ErrNotExist
		},
		// B 之后"系统全失败"不再等于"整体失败"（还有嵌入兜底）。
		// 本用例要测的是**系统链路**的超时语义，因此必须把兜底链路也置为不可用，
		// 否则断言到的是兜底成功，而不是系统链路的诊断。
		Embedded: &failingLoader{err: errors.New("测试注入：嵌入不可用")},
	})

	_, rep, err := p.ResolveWithReport(FamilyCJK)
	if !errors.Is(err, contracts.ErrFontNotFound) {
		t.Fatalf("全部失败应返回 ErrFontNotFound，实际 %v", err)
	}
	if !rep.Timeout {
		t.Error("报告应记录 Timeout=true")
	}
	if rep.Scanned != 1 {
		t.Errorf("超时后不得再启动新候选，实际尝试了 %d 个", rep.Scanned)
	}
	if len(rep.Candidates) != 3 {
		t.Fatalf("剩余候选也要出现在诊断里（不能静默消失），实际 %d", len(rep.Candidates))
	}
	for i := 1; i < 3; i++ {
		if rep.Candidates[i].Status != StatusSkippedTimeout {
			t.Errorf("候选 %d 应标记 skipped_timeout，实际 %s", i, rep.Candidates[i].Status)
		}
	}
	if rep.Elapsed < slowRead {
		t.Errorf("Elapsed 必须如实反映不可取消的同步读取耗时: %v < %v", rep.Elapsed, slowRead)
	}
	t.Logf("超时语义实测: timeout=%v elapsed=%v（超过预算是预期——同步读取不可取消，"+
		"本实现不使用 goroutine 丢弃，故不存在后台任务堆积）", p.opts.Timeout, rep.Elapsed)
}

// TestFontProbeErrFontNotFound 全部失败 → ErrFontNotFound，且不得返回空串+nil
// （A 卡约束 5）。
//
// B 之后注意：这里的"全部失败"必须同时包含**嵌入兜底也失败**——
// 只让系统候选失败的话，B 的兜底会接管并成功返回，那是 B 的正确行为，
// 不是本用例要断言的东西（见 TestEmbeddedFallbackSelectionOrder）。
func TestFontProbeErrFontNotFound(t *testing.T) {
	p := NewProvider(ProviderOptions{
		Candidates: []string{filepath.Join(t.TempDir(), "a.ttc"), filepath.Join(t.TempDir(), "b.ttc")},
		Timeout:    time.Second,
		Embedded:   &failingLoader{err: errors.New("测试注入：嵌入不可用")},
	})

	id, rep, err := p.ResolveWithReport(FamilyCJK)
	if !errors.Is(err, contracts.ErrFontNotFound) {
		t.Fatalf("期望 ErrFontNotFound，实际 %v", err)
	}
	if id != "" {
		t.Errorf("失败时标识必须为空（不得用占位标识伪装成功），实际 %q", id)
	}
	for _, c := range rep.Candidates {
		if c.Status != StatusMissing {
			t.Errorf("候选 %q 状态应为 missing，实际 %s", c.Path, c.Status)
		}
	}
}

// TestFontProbeResolveCaching 同一 family 二次 Resolve 不重新扫描磁盘，
// 且返回的是同一份已解析集合（契约 + 评审要点 1）。
func TestFontProbeResolveCaching(t *testing.T) {
	// 必须含汉字：C 卡 §3 之后，"能解析"不再是命中标准，还要有 CJK 覆盖。
	good := writeFont(t, t.TempDir(), "good.otf", hanziFontBytes(t))

	reads := 0
	p := NewProvider(ProviderOptions{
		Candidates: []string{good},
		Timeout:    time.Second,
		Read: func(path string) ([]byte, error) {
			reads++
			return os.ReadFile(path)
		},
	})

	id1, _, err := p.ResolveWithReport(FamilyCJK)
	if err != nil {
		t.Fatalf("首次 Resolve 失败: %v", err)
	}
	if reads != 1 {
		t.Fatalf("首次应读取 1 次，实际 %d", reads)
	}
	id2, _, err := p.ResolveWithReport(FamilyCJK)
	if err != nil {
		t.Fatalf("二次 Resolve 失败: %v", err)
	}
	if id1 != id2 {
		t.Errorf("两次 Resolve 的标识应一致: %q vs %q", id1, id2)
	}
	if reads != 1 {
		t.Errorf("二次 Resolve 不得重新扫描磁盘: reads=%d", reads)
	}
	if p.Scans() != 1 {
		t.Errorf("扫描次数应只增加 1，实际 %d", p.Scans())
	}

	f1, ok1 := p.Faces(id1)
	f2, ok2 := p.Faces(id2)
	if !ok1 || !ok2 {
		t.Fatal("缓存后的标识应能反查")
	}
	if len(f1) == 0 || &f1[0] != &f2[0] {
		t.Error("两次反查必须指向同一份集合，不得重新解析出副本")
	}
}

// TestFontProbeRejectsUnknownAndUnimplementedFamilies 未知族名与未实现族名
// 必须严格失败，且**不得**触发磁盘扫描（A 卡 §4.1、约束 5）。
func TestFontProbeRejectsUnknownAndUnimplementedFamilies(t *testing.T) {
	p := NewProvider(ProviderOptions{
		Candidates: []string{"/definitely/not/here.ttc"},
		Timeout:    time.Second,
	})

	for _, fam := range []string{"Nope", FamilyBody, FamilyMono} {
		id, err := p.Resolve(fam)
		if !errors.Is(err, contracts.ErrFontNotFound) {
			t.Errorf("族名 %q 应返回 ErrFontNotFound，实际 %v", fam, err)
		}
		if id != "" {
			t.Errorf("族名 %q 失败时不得返回标识，实际 %q", fam, id)
		}
	}
	if p.Scans() != 0 {
		t.Errorf("未知/未实现族名不得触发磁盘扫描: scans=%d", p.Scans())
	}
}

// TestFontProbeWindowsSystemFonts Windows 真机探测（A 卡 §5 第 6 项）。
//
// 证据等级：**实测**，仅 GOOS=windows。macOS / Linux 的真机探测为**待验证**。
func TestFontProbeWindowsSystemFonts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅 Windows 实测；macOS / Linux 真机探测**待验证**（A 卡 §5）")
	}

	p := NewProvider(ProviderOptions{})
	id, rep, err := p.ResolveWithReport(FamilyCJK)
	if err != nil {
		t.Fatalf("Windows 实测探测失败: %v\n诊断:\n%s", err, rep)
	}
	faces, ok := p.Faces(id)
	if !ok || len(faces) == 0 {
		t.Fatalf("标识 %q 反查不到字体集合", id)
	}
	t.Logf("Windows 实测: id=%s path=%s faces=%d elapsed=%v\n诊断:\n%s",
		id, rep.Resolved, len(faces), rep.Elapsed, rep)
}

// TestFontProbeShaperAssembly 命中后装配的 Shaper 必须真含 CJK 字形
// （A 卡 §5 第 7 项，证据等级：实测，仅 Windows）。
func TestFontProbeShaperAssembly(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅 Windows 实测；macOS / Linux 装配验证**待验证**（A 卡 §5）")
	}

	th, rep := NewThemeWithReport()
	if rep.Err != nil {
		t.Fatalf("装配前探测失败: %v\n诊断:\n%s", rep.Err, rep)
	}
	if th.Shaper == nil {
		t.Fatal("装配后 Shaper 不应为 nil")
	}
	faces, ok := defaultProvider.Faces(rep.ID)
	if !ok || len(faces) == 0 {
		t.Fatalf("标识 %q 反查不到字体集合", rep.ID)
	}

	// GID 0 表示 notdef：命中集合必须真有「中」的字形，而不是靠系统字体兜底。
	found := false
	for _, ff := range faces {
		gid, ok := ff.Face.Face().NominalGlyph('中')
		if ok && gid != 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("装配的字体集合不含「中」字形（faces=%d, path=%s）", len(faces), rep.Resolved)
	}

	// 记录装配对拉丁度量的影响（A 卡风险 3）：装配会改变 defaultFaces 查询，
	// 因此拉丁文本宽度可能变化。这里只记录不断言——判断"变化正确 vs 回归"
	// 需要在全量测试里比对。
	base := material.NewTheme()
	t.Logf("拉丁度量对比（px=16, \"ABCD\"）: 装配前=%v 装配后=%v —— 差异即装配的已知影响",
		advanceOf(t, base.Shaper, "ABCD", 16), advanceOf(t, th.Shaper, "ABCD", 16))
}

// advanceOf 返回用给定 Shaper 排版文本后的总 advance（单位 1/64 px）。
func advanceOf(t *testing.T, sh *text.Shaper, s string, px int) fixed.Int26_6 {
	t.Helper()
	sh.LayoutString(text.Parameters{PxPerEm: fixed.I(px), MaxWidth: 1 << 20}, s)
	var adv fixed.Int26_6
	for {
		g, ok := sh.NextGlyph()
		if !ok {
			break
		}
		adv += g.Advance
	}
	return adv
}
