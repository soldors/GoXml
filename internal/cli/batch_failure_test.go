package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// PRODUCT-R2 切片 1：多页面生成的故障收口（提交期之前 / 之内的失败）
//
// 任务书要的是「前一页可生成、后一页语义失败」的真实工程形态，以及
// 「故障必须确实到达目标阶段」。这里覆盖三段此前没有被钉住的事实：
//
//  1. 语义失败（Analyzer StatusPartial）的批次在阶段一就停住，磁盘零痕迹——
//     已有的 TestEndToEndBatchAllOrNothing 用的是**语法**错误，
//     TestCacheNoWriteOnFailure 用的是**单文件**；两者都没有证明
//     「已经生成成功的另一页也不落盘」+「Manifest/缓存目录没被顺手建出来」。
//  2. 批次内多个失败输入时，报出哪一个必须是确定的（按排序后的第一个），
//     否则同一工程两次运行会给出不同诊断，用户与脚本都无法依赖。
//  3. 产物提交成功但 Manifest 保存失败时：产物保留、**不得**先写缓存
//     （任务卡 §4 的顺序：产物 → Manifest → 缓存），且错误必须可见（退出码 2）。
//
// 故障注入只用已有机制：测试专用的 manifestPathOverride + 一个普通文件
// 当目录占位（Windows 下 ReadFile 走 IsNotExist 分支、MkdirAll 真失败，
// 实测见交付目录 raw/）。不制造权限/进程残留，不改系统函数。
// ============================================================================

// writeSemanticPage 在 dir 写一页 .gxml 与其 ViewModel。
// field 为空时绑定一个不存在的字段，制造 Analyzer 语义失败（GXML-BIND-001）。
func writeSemanticPage(t *testing.T, dir, name, field string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := sanitizePackageName(filepath.Base(dir))
	vm := "package " + pkg + "\n\ntype VM struct{ Title string }\n"
	if err := os.WriteFile(filepath.Join(dir, "vm.go"), []byte(vm), 0o644); err != nil {
		t.Fatal(err)
	}
	if field == "" {
		field = "Missing"
	}
	gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/` + name + `"><Text Value="{` + field + `}"/></Page>
</Goxml>`
	if err := os.WriteFile(filepath.Join(dir, name+".gxml"), []byte(gxml), 0o644); err != nil {
		t.Fatal(err)
	}
}

// countTempResidue 统计 root 子树里工具临时文件的数量（.goxml-*.tmp）。
func countTempResidue(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), ".goxml-") && strings.HasSuffix(d.Name(), ".tmp") {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s: %v", root, err)
	}
	return found
}

// TestBatchSemanticFailureKeepsDiskPristine：前页可生成、后页语义失败 →
// 全批不落盘，且不留任何工具痕迹（产物 / Manifest / 缓存 / 临时文件）。
func TestBatchSemanticFailureKeepsDiskPristine(t *testing.T) {
	root := tempProjectInRepo(t)
	// 两个不同包（同包两产物会撞上独立的符号重名缺陷，见缓存任务卡 §13）。
	writeSemanticPage(t, filepath.Join(root, "alpha"), "row", "Title")
	writeSemanticPage(t, filepath.Join(root, "zeta"), "row", "")

	var out, errb bytes.Buffer
	code := Run([]string{"generate", root}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d，期望 %d；stdout:\n%s\nstderr:\n%s", code, ExitFailure, out.String(), errb.String())
	}

	// 诊断必须可定位：文件名 + 行号 + 诊断码都在。
	for _, want := range []string{"row.gxml:2:", "GXML-BIND-001"} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("stderr 应含 %q，实际:\n%s", want, errb.String())
		}
	}
	if !strings.Contains(errb.String(), "批次全部放弃") {
		t.Errorf("错误信息应说明全或无语义:\n%s", errb.String())
	}

	// 声称「磁盘无任何变更」就必须真的没有任何变更。
	for _, gen := range []string{
		filepath.Join(root, "alpha", "row_gen.go"),
		filepath.Join(root, "zeta", "row_gen.go"),
	} {
		if _, err := os.Stat(gen); !os.IsNotExist(err) {
			t.Errorf("失败批次留下了产物 %s（err=%v）", gen, err)
		}
	}
	if _, err := os.Stat(manifestPath(root)); !os.IsNotExist(err) {
		t.Errorf("失败批次不得写 Manifest: %v", err)
	}
	if _, err := os.Stat(cacheDir(root)); !os.IsNotExist(err) {
		t.Errorf("失败批次不得创建缓存目录: %v", err)
	}
	if residue := countTempResidue(t, root); len(residue) > 0 {
		t.Errorf("失败批次残留临时文件: %v", residue)
	}
}

// TestBatchFailureReportsFirstSortedInput：两个都坏时报出哪一个必须确定。
//
// 创建顺序与排序顺序**相反**（先建 zeta 再建 alpha），这样按文件系统返回
// 顺序实现的实现会在这里变红。
func TestBatchFailureReportsFirstSortedInput(t *testing.T) {
	root := tempProjectInRepo(t)
	// 文件名各不相同：诊断里出现哪一个，就直接证明阶段一处理到了哪一页。
	writeSemanticPage(t, filepath.Join(root, "zeta"), "page_zeta", "")
	writeSemanticPage(t, filepath.Join(root, "alpha"), "page_alpha", "")

	var out, errb bytes.Buffer
	code := Run([]string{"generate", root}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d，期望 %d", code, ExitFailure)
	}
	msg := errb.String()
	if !strings.Contains(msg, "page_alpha") {
		t.Fatalf("应报排序在前的 alpha/page_alpha.gxml，实际:\n%s", msg)
	}
	if strings.Contains(msg, "page_zeta") {
		t.Fatalf("同一批次不应同时报排序在后的输入（阶段一即中止）:\n%s", msg)
	}
}

// TestManifestSaveFailureKeepsProductAndDefersCache：产物提交成功、
// Manifest 保存失败 → 产物保留、退出码 2、**不写缓存**、无临时文件残留。
func TestManifestSaveFailureKeepsProductAndDefersCache(t *testing.T) {
	root := tempProjectInRepo(t)
	copyFixture(t, root)

	// 用普通文件占据 Manifest 的父目录：读 Manifest 走「不存在」分支（首次生成合法），
	// 写 Manifest 时 MkdirAll 真失败——故障恰好落在提交之后那一步。
	blocker := filepath.Join(root, "manifest-blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := manifestPathOverride
	manifestPathOverride = filepath.Join(blocker, manifestName)
	t.Cleanup(func() { manifestPathOverride = old })

	var out, errb bytes.Buffer
	code := Run([]string{"generate", filepath.Join(root, "home.gxml")}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d，期望 %d；stdout:\n%s\nstderr:\n%s", code, ExitFailure, out.String(), errb.String())
	}
	if !strings.Contains(errb.String(), "更新 Manifest") {
		t.Errorf("stderr 应指明失败发生在 Manifest 更新: %s", errb.String())
	}
	// 顺序证据：Manifest 失败即返回，缓存轮不到——若实现把缓存提到 Manifest 之前，
	// 这里会看到「缓存写入失败」的警告行。
	if strings.Contains(errb.String(), "缓存写入失败") {
		t.Errorf("Manifest 失败前不该已经尝试写缓存: %s", errb.String())
	}
	if _, err := os.Stat(filepath.Join(blocker, cacheDirName)); !os.IsNotExist(err) {
		t.Errorf("缓存目录被创建（说明写入顺序倒了）: %v", err)
	}
	// 已提交的产物必须保留（不能因为后续步骤失败而回滚掉）。
	product := filepath.Join(root, "home_gen.go")
	first, err := os.ReadFile(product)
	if err != nil {
		t.Fatalf("Manifest 保存失败不应毁掉已提交产物: %v", err)
	}
	if !strings.HasPrefix(string(first), "// Code generated by goxml. DO NOT EDIT.") {
		t.Errorf("产物保护头缺失: %q", first)
	}
	if residue := countTempResidue(t, root); len(residue) > 0 {
		t.Errorf("失败后残留临时文件: %v", residue)
	}

	// 自愈：清掉占位后重跑，产物逐字节一致、Manifest 正常登记。
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(manifestPathOverride), 0o755); err != nil {
		t.Fatal(err)
	}
	var out2, errb2 bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(root, "home.gxml")}, &out2, &errb2); code != ExitOK {
		t.Fatalf("修复占位后重跑应成功: 退出码 %d，stderr:\n%s", code, errb2.String())
	}
	second, err := os.ReadFile(product)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("重跑产物字节不一致（确定性契约 §6）：首轮 %d 字节，次轮 %d 字节", len(first), len(second))
	}
	m, err := loadManifest(manifestPath(root))
	if err != nil {
		t.Fatalf("重跑后 Manifest 应可读: %v", err)
	}
	if len(m.Entries) != 1 || m.Entries[0].Output == "" {
		t.Fatalf("Manifest 应登记 1 条，实际: %+v", m.Entries)
	}
}
