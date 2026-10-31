package theme

import (
	"errors"
	"reflect"
	"testing"
)

// 本文件补强「字体报告隔离」的测试强度（对应代码评审 A2/A3），不改生产实现。
//
// 共享边界（务必看清，不做通用深拷贝承诺）：
//   - 深拷贝责任在 copyProbeReport；以下引用字段被深拷贝：
//       ProbeReport.Candidates（切片）、ProbeReport.Embedded（指针）、
//       ProbeReport.Coverage（指针）、CoverageResult.Missing（切片）。
//   - 以下 error 接口字段按约定**不可变共享**（不深拷贝）：
//       ProbeReport.Err、CandidateResult.Err、EmbeddedResult.Err。
//   - 仅覆盖报告数据结构（ProbeReport 及其嵌套类型）；不承诺对任意对象通用深拷贝。
//   - 新增 slice/map/指针/非 error 接口字段时，copyProbeReport 必须补拷贝，且本守卫的
//     allowlist 必须同步更新（否则本测试会变红提醒补测）。

// buildRefPopulatedReport 构造一个所有引用字段都非 nil/非空的报告，用于就地污染断言。
func buildRefPopulatedReport() *ProbeReport {
	return &ProbeReport{
		Family:   "CJK",
		ID:       "id-1",
		ProbeRan: true,
		Candidates: []CandidateResult{
			{Path: "c0.otf", Status: StatusOK},
			{Path: "c1.otf"},
		},
		Embedded: &EmbeddedResult{Attempted: true, Bytes: 42},
		Coverage: &CoverageResult{Required: 2, Hit: 1, Missing: []rune{'甲', '乙'}},
		Err:      errors.New("top-error"),
	}
}

// A2（核心）：copyProbeReport 对 Coverage.Missing 做的是真深拷贝。
// 关键：污染必须**就地写元素**（Missing[0]=…），不能用 append 冒充——
// append 在扩容时会换底层数组，污染不到共享内存，从而无法证明深拷贝。
func TestCopyProbeReportMissingInPlaceIsolation(t *testing.T) {
	orig := buildRefPopulatedReport()
	cp := copyProbeReport(orig)
	if cp == nil {
		t.Fatal("copyProbeReport 返回 nil")
	}
	if cp == orig {
		t.Fatal("copyProbeReport 返回了同一指针（未拷贝）")
	}
	if cp.Coverage == orig.Coverage || cp.Embedded == orig.Embedded {
		t.Fatal("嵌套指针 Coverage/Embedded 仍是同一指针（浅拷贝）")
	}

	// 就地污染副本的 Coverage.Missing[0]。
	cp.Coverage.Missing[0] = 0xFFFD
	if got := orig.Coverage.Missing[0]; got != '甲' {
		t.Fatalf("就地修改副本 Missing 污染了原对象：orig.Missing[0]=%U", got)
	}

	// 其它嵌套引用也应隔离：就地改 Candidates[0].Path 与 Embedded.Bytes。
	cp.Candidates[0].Path = "HACKED"
	cp.Embedded.Bytes = -1
	if orig.Candidates[0].Path != "c0.otf" {
		t.Fatalf("就地修改副本 Candidates 污染了原对象：%q", orig.Candidates[0].Path)
	}
	if orig.Embedded.Bytes != 42 {
		t.Fatalf("就地修改副本 Embedded 污染了原对象：%d", orig.Embedded.Bytes)
	}

	// 反向：就地改原对象，不应影响已取出的副本。
	orig.Coverage.Missing[1] = 0xFFFE
	if cp.Coverage.Missing[1] != '乙' {
		t.Fatalf("就地修改原对象 Missing 污染了副本：cp.Missing[1]=%U", cp.Coverage.Missing[1])
	}
}

// A2（真实消费者路径 1）：Provider.LastReport 每次返回独立副本，
// 调用方就地改 Missing 不得影响 Provider 内部历史（p.last）或下一次读取。
func TestLastReportMissingInPlaceIsolation(t *testing.T) {
	p := &Provider{}
	p.last = &ProbeReport{
		Family:   "CJK",
		Coverage: &CoverageResult{Required: 2, Hit: 1, Missing: []rune{'甲', '乙'}},
	}
	r1 := p.LastReport()
	if r1 == nil || r1.Coverage == nil || len(r1.Coverage.Missing) == 0 {
		t.Fatal("fixture 无效：LastReport 副本缺少非空 Missing")
	}
	r1.Coverage.Missing[0] = 0xFFFD // 就地污染第一次拿到的副本

	r2 := p.LastReport()
	if r2.Coverage.Missing[0] != '甲' {
		t.Fatalf("两次 LastReport 共享了 Missing 底层数组：r2.Missing[0]=%U", r2.Coverage.Missing[0])
	}
	if p.last.Coverage.Missing[0] != '甲' {
		t.Fatalf("调用方就地修改污染了 Provider 内部报告 p.last：%U", p.last.Coverage.Missing[0])
	}
}

// A2（真实消费者路径 2）：Lazy.Report 每次返回独立副本（经 copyProbeReport）。
func TestLazyReportMissingInPlaceIsolation(t *testing.T) {
	lz := new(Lazy)
	lz.rep.Probe = &ProbeReport{
		Family:   "CJK",
		Coverage: &CoverageResult{Required: 2, Hit: 1, Missing: []rune{'甲', '乙'}},
	}
	a := lz.Report()
	if a.Probe == nil || a.Probe.Coverage == nil || len(a.Probe.Coverage.Missing) == 0 {
		t.Fatal("fixture 无效：Lazy.Report 副本缺少非空 Missing")
	}
	a.Probe.Coverage.Missing[0] = 0xFFFD // 就地污染

	b := lz.Report()
	if b.Probe.Coverage.Missing[0] != '甲' {
		t.Fatalf("两次 Lazy.Report 共享了 Missing 底层数组：%U", b.Probe.Coverage.Missing[0])
	}
	if lz.rep.Probe.Coverage.Missing[0] != '甲' {
		t.Fatalf("调用方就地修改污染了 Lazy 内部报告：%U", lz.rep.Probe.Coverage.Missing[0])
	}
}

// A3（结构守卫）：报告结构新增**可变引用字段**（slice/map/指针/非 error 接口）时，
// copyProbeReport 若不补拷贝，本测试不会静默通过——字段巡检会变红要求补测。
// 用反射做字段巡检（仅测试；生产/生成运行时代码不新增反射）。
func TestProbeReportMutableRefFieldsGuarded(t *testing.T) {
	// allowlist：已确认的引用字段路径 → 处理方式说明。
	// “shared error” 表示按约定共享的不可变 error；其余为“已深拷贝”。
	known := map[string]string{
		"ProbeReport.Candidates":         "deep-copied []CandidateResult",
		"ProbeReport.Embedded":           "deep-copied *EmbeddedResult",
		"ProbeReport.Coverage":           "deep-copied *CoverageResult",
		"ProbeReport.Err":                "shared error (immutable by convention)",
		"ProbeReport.Candidates[].Err":   "shared error",
		"ProbeReport.Embedded[].Err":     "shared error",
		"ProbeReport.Coverage[].Missing": "deep-copied []rune",
	}
	seen := map[string]bool{}
	var walk func(ty reflect.Type, path string)
	walk = func(ty reflect.Type, path string) {
		if ty.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < ty.NumField(); i++ {
			f := ty.Field(i)
			if !f.IsExported() {
				continue
			}
			p := path + f.Name
			switch f.Type.Kind() {
			case reflect.Slice, reflect.Map, reflect.Ptr:
				seen[p] = true
				if _, ok := known[p]; !ok {
					t.Errorf("报告结构新增可变引用字段 %q（%s）：请在 copyProbeReport 深拷贝，并把该字段加入本守卫 allowlist", p, f.Type)
				}
				if (f.Type.Kind() == reflect.Slice || f.Type.Kind() == reflect.Ptr) && f.Type.Elem().Kind() == reflect.Struct {
					walk(f.Type.Elem(), p+"[].")
				}
			case reflect.Interface:
				seen[p] = true
				if _, ok := known[p]; !ok {
					t.Errorf("报告结构新增接口字段 %q（%s）：若为可变对象需处理；若为不可变（如 error）请加入 allowlist 并注明共享", p, f.Type)
				}
			case reflect.Struct:
				walk(f.Type, p+".")
			}
		}
	}
	walk(reflect.TypeOf(ProbeReport{}), "ProbeReport.")

	// allowlist 中若有字段已不存在（重命名/删除），也要提醒更新守卫，避免陈旧 allowlist 掩盖新增字段。
	for k := range known {
		if !seen[k] {
			t.Errorf("allowlist 中的引用字段 %q 已不存在（结构变更）：请更新 copyProbeReport 与本守卫", k)
		}
	}
}

// 与结构守卫配套的**身份**断言：已声明“已深拷贝”的字段，副本与原件底层地址必须不同。
// 这能捕获“字段仍在、但 copyProbeReport 退化为浅拷贝”的回归（与 A2 行为断言互补）。
func TestProbeReportDeepCopiedFieldsHaveDistinctIdentity(t *testing.T) {
	orig := buildRefPopulatedReport()
	cp := copyProbeReport(orig)
	if cp.Candidates[0].Path != orig.Candidates[0].Path || cp.Coverage.Missing[0] != orig.Coverage.Missing[0] {
		t.Fatalf("副本内容被改动：cp=%+v", cp)
	}
	if reflect.ValueOf(cp.Candidates).Pointer() == reflect.ValueOf(orig.Candidates).Pointer() {
		t.Error("Candidates 切片共享底层数组（应为深拷贝）")
	}
	if cp.Embedded == orig.Embedded {
		t.Error("Embedded 指针共享（应为深拷贝）")
	}
	if cp.Coverage == orig.Coverage {
		t.Error("Coverage 指针共享（应为深拷贝）")
	}
	if reflect.ValueOf(cp.Coverage.Missing).Pointer() == reflect.ValueOf(orig.Coverage.Missing).Pointer() {
		t.Error("Coverage.Missing 切片共享底层数组（应为深拷贝）")
	}
}
