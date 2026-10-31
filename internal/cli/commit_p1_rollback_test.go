package cli

// R3-P1（DeepSeek 接管）：E1/E4/E5 三个提交失败入口的统一回滚回归。
//
// 断言基于**磁盘/错误状态**（旧字节恢复、新建撤回、tmp/bak 残留、residue/unrestored 字段），
// 不靠"符号不存在"或"错误串不含恢复"来冒充。私有 fileOps 注入，不改全局文件系统函数/ACL。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func newStaged(t *testing.T, targets ...string) ([]stagedFile, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	var staged []stagedFile
	tmps := map[string]string{}
	for _, tg := range targets {
		tp := filepath.Join(dir, filepath.Base(tg)+".tmp")
		if err := os.WriteFile(tp, []byte("NEW-"+filepath.Base(tg)), 0o644); err != nil {
			t.Fatal(err)
		}
		staged = append(staged, stagedFile{target: tg, tmp: tp})
		tmps[tg] = tp
	}
	return staged, tmps
}

// regularTarget 建一个有旧内容的普通文件目标，返回其旧字节。
func regularTarget(t *testing.T, path, old string) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	return []byte(old)
}

func dirEntries(t *testing.T, d string) []string {
	t.Helper()
	es, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// E1：备份旧目标失败（第 2 项），第 1 项已提交——必须恢复第 1 项旧字节，且错误带 restored。
func TestP1E1BackupFailureRollsBackPriorCommitted(t *testing.T) {
	dir := t.TempDir()
	t1 := filepath.Join(dir, "a_gen.go")
	t2 := filepath.Join(dir, "b_gen.go")
	regularTarget(t, t1, "OLD-A")
	regularTarget(t, t2, "OLD-B")
	staged, tmps := newStaged(t, t1, t2)

	ops := realOps
	ops.rename = func(old, new string) error {
		// 第 2 项备份（target→bak）失败；第 1 项的备份与 tmp→target 正常。
		if old == t2 && strings.HasSuffix(new, ".bak") {
			return errors.New("backup denied")
		}
		return os.Rename(old, new)
	}
	_, err := commitBatchOps(staged, ops)
	if err == nil {
		t.Fatal("应失败")
	}
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应 *txnError，实际 %T", err)
	}
	// 第 1 项旧字节必须恢复。
	if b, _ := os.ReadFile(t1); string(b) != "OLD-A" {
		t.Fatalf("E1：此前已提交项未恢复旧内容，得到 %q", b)
	}
	// 第 2 项旧目标未被覆盖（备份失败，rename 未发生 → 原样）。
	if b, _ := os.ReadFile(t2); string(b) != "OLD-B" {
		t.Fatalf("E1：当前失败项目标被改动，得到 %q", b)
	}
	if !containsPath(te.restored, t1) {
		t.Errorf("E1：restored 应含 %s，实际 %v", t1, te.restored)
	}
	// 清理：本批 tmp 不得残留。
	for _, tp := range tmps {
		if _, err := os.Stat(tp); err == nil {
			t.Errorf("E1：tmp 未清理 %s", tp)
		}
	}
	for _, tp := range tmps {
		if _, err := os.Lstat(tp + ".bak"); !os.IsNotExist(err) {
			t.Errorf("E1：真实备份应随恢复消耗或未产生: %s: %v", tp+".bak", err)
		}
	}
	if len(te.residue) != 0 || len(te.unrestored) != 0 || len(te.withdrawn) != 0 {
		t.Errorf("E1：回滚应无残留/未恢复/撤回: %+v", te)
	}
}

// E1 首项失败（committed 为空）：不需要恢复，但 tmp 清理且错误不带 restored。
func TestP1E1BackupFailureFirstItem(t *testing.T) {
	dir := t.TempDir()
	t1 := filepath.Join(dir, "a_gen.go")
	regularTarget(t, t1, "OLD-A")
	staged, tmps := newStaged(t, t1)
	ops := realOps
	ops.rename = func(old, new string) error { return errors.New("backup denied") }
	_, err := commitBatchOps(staged, ops)
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应 *txnError")
	}
	if len(te.restored) != 0 || len(te.withdrawn) != 0 {
		t.Errorf("首项失败不应有 restored/withdrawn：%v/%v", te.restored, te.withdrawn)
	}
	if b, _ := os.ReadFile(t1); string(b) != "OLD-A" {
		t.Fatalf("首项失败旧目标不应被动，得到 %q", b)
	}
	for _, tp := range tmps {
		if _, err := os.Stat(tp); err == nil {
			t.Errorf("首项失败 tmp 未清理 %s", tp)
		}
	}
}

// E4：Lstat 非 IsNotExist 错误（用私有 ops 注入 statErr 于第 2 项）——回滚第 1 项已有目标，
// 且当前目标不动、不误作新建撤回。
func TestP1E4LstatErrorRollsBackPrior(t *testing.T) {
	dir := t.TempDir()
	t1 := filepath.Join(dir, "a_gen.go")
	t2 := filepath.Join(dir, "b_gen.go")
	regularTarget(t, t1, "OLD-A")
	// t2 不存在（本应作为新建），但注入其 Lstat 返回权限错误。
	staged, tmps := newStaged(t, t1, t2)
	lstatBoom := errors.New("lstat EACCES")
	ops := realOps
	// 仅替换按值传递的私有 ops，不污染其它测试。
	ops.lstat = func(p string) (os.FileInfo, error) {
		if p == t2 {
			return nil, lstatBoom
		}
		return os.Lstat(p)
	}

	_, err := commitBatchOps(staged, ops)
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应 *txnError，实际 %T: %v", err, err)
	}
	if !errors.Is(err, lstatBoom) {
		t.Errorf("E4：主 cause 应保留 lstat 错误: %v", err)
	}
	if b, _ := os.ReadFile(t1); string(b) != "OLD-A" {
		t.Fatalf("E4：此前已提交项未恢复，得到 %q", b)
	}
	if _, err := os.Stat(t2); !os.IsNotExist(err) {
		t.Errorf("E4：当前失败项目标不应被创建: %v", err)
	}
	if containsPath(te.withdrawn, t2) {
		t.Errorf("E4：未提交的本项不得被当作新建目标撤回: %v", te.withdrawn)
	}
	if !containsPath(te.restored, t1) {
		t.Errorf("E4：restored 应含 %s，实际 %v", t1, te.restored)
	}
	for _, tp := range tmps {
		if _, err := os.Stat(tp); err == nil {
			t.Errorf("E4：tmp 未清理 %s", tp)
		}
	}
}

// E5：遇到非普通目标（符号链接）——显式拒绝、回滚此前已提交项、绝不替换链接本体、
// 不改 Readlink 目标与第三方文件字节。
func TestP1E5NonRegularTargetRejectedRollsBack(t *testing.T) {
	dir := t.TempDir()
	t1 := filepath.Join(dir, "a_gen.go")
	regularTarget(t, t1, "OLD-A")
	// 第三方真实文件 + 指向它的符号链接作为第 2 项目标。
	realFile := filepath.Join(dir, "real-target.txt")
	if err := os.WriteFile(realFile, []byte("THIRD-PARTY"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "b_gen.go")
	if err := os.Symlink(realFile, link); err != nil {
		t.Skipf("symlink 不受支持（需特权）：%v", err)
	}
	staged, tmps := newStaged(t, t1, link)
	_, err := commitBatchOps(staged, realOps)
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("E5：应 *txnError，实际 %T: %v", err, err)
	}
	// 第 1 项旧字节恢复。
	if b, _ := os.ReadFile(t1); string(b) != "OLD-A" {
		t.Fatalf("E5：此前已提交项未恢复，得到 %q", b)
	}
	// 链接本体仍在、仍为链接、Readlink 目标不变。
	fi, lerr := os.Lstat(link)
	if lerr != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("E5：链接被替换/删除（lstat err=%v mode=%v）", lerr, fi.Mode())
	}
	if dst, _ := os.Readlink(link); dst != realFile {
		t.Fatalf("E5：Readlink 目标被改：%q", dst)
	}
	// 第三方文件字节不变。
	if b, _ := os.ReadFile(realFile); string(b) != "THIRD-PARTY" {
		t.Fatalf("E5：第三方文件被改：%q", b)
	}
	// 链接未被当作「新建撤回」。
	if containsPath(te.withdrawn, link) || containsPath(te.unrestored, link) {
		t.Errorf("E5：非普通目标不应进入 withdrawn/unrestored：%v/%v", te.withdrawn, te.unrestored)
	}
	for _, tp := range tmps {
		if _, err := os.Stat(tp); err == nil {
			t.Errorf("E5：tmp 未清理 %s", tp)
		}
	}
}

// E5 单独链接（首项即非普通）：committed 为空，拒绝且不影响链接。
func TestP1E5NonRegularFirstItemOnly(t *testing.T) {
	dir := t.TempDir()
	realFile := filepath.Join(dir, "real.txt")
	os.WriteFile(realFile, []byte("X"), 0o644)
	link := filepath.Join(dir, "lonely_gen.go")
	if err := os.Symlink(realFile, link); err != nil {
		t.Skipf("symlink 不受支持：%v", err)
	}
	staged, _ := newStaged(t, link)
	_, err := commitBatchOps(staged, realOps)
	var te *txnError
	if !errors.As(err, &te) {
		t.Fatalf("应 *txnError")
	}
	if len(te.restored) != 0 || len(te.withdrawn) != 0 {
		t.Errorf("单独链接不应有 restored/withdrawn")
	}
	if fi, e := os.Lstat(link); e != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("链接本体应保留")
	}
}

// 成功/覆盖/新建守卫：普通覆盖与新建仍工作，成功时无残留。
func TestP1SuccessOverwriteAndCreateStillWork(t *testing.T) {
	dir := t.TempDir()
	t1 := filepath.Join(dir, "exists_gen.go")
	regularTarget(t, t1, "OLD")
	t2 := filepath.Join(dir, "new_gen.go") // 新建
	staged, _ := newStaged(t, t1, t2)
	residue, err := commitBatchOps(staged, realOps)
	if err != nil {
		t.Fatalf("成功路径不应失败: %v", err)
	}
	if len(residue) != 0 {
		t.Errorf("成功不应有 success residue: %v", residue)
	}
	if b, _ := os.ReadFile(t1); string(b) != "NEW-exists_gen.go" {
		t.Errorf("普通覆盖内容错：%q", b)
	}
	if b, _ := os.ReadFile(t2); string(b) != "NEW-new_gen.go" {
		t.Errorf("新建内容错：%q", b)
	}
	// 无 .bak 残留。
	for _, n := range dirEntries(t, dir) {
		if strings.HasSuffix(n, ".bak") {
			t.Errorf("成功路径残留备份: %s", n)
		}
	}
}

// 空批次不退化。
func TestP1EmptyBatchNoop(t *testing.T) {
	if _, err := commitBatchOps(nil, realOps); err != nil {
		t.Fatalf("空批次应成功: %v", err)
	}
}

// Review matrix: every early exit before installation, both empty and mixed prior commits.
func TestP1ReviewEarlyExitMatrix(t *testing.T) {
	for _, exit := range []string{"backup", "lstat", "directory"} {
		for _, prior := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/prior=%t", exit, prior), func(t *testing.T) {
				dir := t.TempDir()
				old := filepath.Join(dir, "a.go")
				fresh := filepath.Join(dir, "b.go")
				current := filepath.Join(dir, "c.go")
				regularTarget(t, current, "OLD-C")
				if exit == "directory" {
					if err := os.Remove(current); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(current, 0755); err != nil {
						t.Fatal(err)
					}
				}
				before, err := os.Lstat(current)
				if err != nil {
					t.Fatal(err)
				}
				targets := []string{current}
				if prior {
					regularTarget(t, old, "OLD-A")
					targets = []string{old, fresh, current}
				}
				staged, tmps := newStaged(t, targets...)
				boom := errors.New("review initial cause")
				ops := realOps
				ops.lstat = func(p string) (os.FileInfo, error) {
					if exit == "lstat" && p == current {
						return nil, boom
					}
					return os.Lstat(p)
				}
				ops.rename = func(a, b string) error {
					if exit == "backup" && a == current {
						return boom
					}
					if exit == "directory" && (a == current || b == current) {
						t.Fatal("nonregular target reached rename")
					}
					return os.Rename(a, b)
				}
				var removed []string
				ops.remove = func(p string) error { removed = append(removed, p); return os.Remove(p) }
				_, err = commitBatchOps(staged, ops)
				var te *txnError
				if !errors.As(err, &te) {
					t.Fatalf("expected txnError: %v", err)
				}
				if exit != "directory" && !errors.Is(err, boom) {
					t.Fatalf("lost cause: %v", err)
				}
				if containsPath(removed, current) {
					t.Fatal("uninstalled current target was removed")
				}
				after, e := os.Lstat(current)
				if e != nil {
					t.Fatal(e)
				}
				if after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
					t.Fatal("current target metadata changed")
				}
				if exit != "directory" {
					if b, e := os.ReadFile(current); e != nil || string(b) != "OLD-C" {
						t.Fatalf("current bytes changed: %q %v", b, e)
					}
				}
				if prior {
					if b, e := os.ReadFile(old); e != nil || string(b) != "OLD-A" {
						t.Fatalf("old not restored: %q %v", b, e)
					}
					if _, e := os.Lstat(fresh); !os.IsNotExist(e) {
						t.Fatalf("new not withdrawn: %v", e)
					}
					if len(te.restored) != 1 || te.restored[0] != old || len(te.withdrawn) != 1 || te.withdrawn[0] != fresh {
						t.Fatalf("wrong result: %+v", te)
					}
				} else if len(te.restored)+len(te.withdrawn) != 0 {
					t.Fatalf("unexpected rollback result: %+v", te)
				}
				if len(te.residue)+len(te.unrestored) != 0 {
					t.Fatalf("unexpected residue: %+v", te)
				}
				for _, tmp := range tmps {
					for _, p := range []string{tmp, tmp + ".bak"} {
						if _, e := os.Lstat(p); !os.IsNotExist(e) {
							t.Errorf("residue %s: %v", p, e)
						}
					}
				}
			})
		}
	}
}
