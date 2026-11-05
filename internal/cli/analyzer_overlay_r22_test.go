package cli

// R22-A / R1 判别回归：Analyzer 必须消费用户 GOFLAGS/GOENV 的 -overlay，
// 且按 **Go 权威选源** 决定读哪些替换文件（不按目录无差别预读）。
//
// 结构：
//   - helper 单元：scopedUserOverlayForAnalyzer 只并入 Go 选中的文件，未选中不读。
//     该测试只证明映射筛选，不声称运行了完整前端（命名与说明准确）。
//   - 行为回归（真实生产入口 Run → generate）：跨包字段/依赖字段应被拒（GXML-BIND）且不写盘；
//     跨包新增方法应成功生成并可真实构建；被平台排除/非 Go 的未用缺失替换不阻断。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func r22Write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// r1Project 建最小工程（module example.com/r1probe），返回 root。
func r1Project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo, _ := filepath.Abs("..")
	repo = filepath.Dir(repo)
	gomod, _ := os.ReadFile(filepath.Join(repo, "go.mod"))
	var lines []string
	for _, ln := range strings.Split(string(gomod), "\n") {
		if strings.HasPrefix(ln, "module ") {
			ln = "module example.com/r1probe"
		}
		lines = append(lines, ln)
	}
	r22Write(t, filepath.Join(root, "go.mod"), strings.TrimRight(strings.Join(lines, "\n"), "\n")+
		"\n\nrequire github.com/goxml/goxml v0.0.0\n\nreplace github.com/goxml/goxml => "+filepath.ToSlash(repo)+"\n")
	sum, _ := os.ReadFile(filepath.Join(repo, "go.sum"))
	r22Write(t, filepath.Join(root, "go.sum"), string(sum))
	return root
}

func r1SetEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", `-mod=readonly -overlay=user.json`)
	t.Setenv("GOWORK", "off")
}

func runCLIStrings(t *testing.T, gxml string) (int, string, string) {
	t.Helper()
	var so, se strings.Builder
	code := Run([]string{"generate", gxml}, &so, &se)
	return code, so.String(), se.String()
}

// TestScopedUserOverlayMapsOnlyGoSelectedFiles helper 单元（准确命名）：
// 只并入 Go 选中的文件；未选中（非 Go 的 notes.txt）不读、不入表。
func TestScopedUserOverlayMapsOnlyGoSelectedFiles(t *testing.T) {
	root := r1Project(t)
	r22Write(t, filepath.Join(root, "ui", "vm.go"), "package ui\ntype VM struct{ Title string }\n")
	r22Write(t, filepath.Join(root, "ui", "doc.go"), "package ui\n")
	r22Write(t, filepath.Join(root, "ui", "vm_repl.go"), "package ui\ntype VM struct{ Title int }\n")
	r22Write(t, filepath.Join(root, "ui", "notes.txt"), "hello")
	r22Write(t, filepath.Join(root, "user.json"),
		`{"Replace":{"ui/vm.go":"ui/vm_repl.go","ui/notes.txt":"ui/absent.txt"}}`)
	um, err := userOverlayMap(`-mod=readonly "-overlay=user.json"`, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := scopedUserOverlayForAnalyzer(r21Go(t), root, []string{"example.com/r1probe/ui"}, um)
	if err != nil {
		t.Fatalf("未选中的缺失替换不应阻断（由 Go 决定）：%v", err)
	}
	if _, ok := got[filepath.Join(root, "ui", "vm.go")]; !ok {
		t.Fatal("应并入 Go 选中的 ui/vm.go")
	}
	if _, ok := got[filepath.Join(root, "ui", "notes.txt")]; ok {
		t.Fatal("不得并入非 Go 的 ui/notes.txt（未选中，不得预读）")
	}
}

// TestScopedUserOverlayDuplicateFailClosed R21 保留：归一化重复仍 fail-closed。
func TestScopedUserOverlayDuplicateFailClosed(t *testing.T) {
	root := r1Project(t)
	r22Write(t, filepath.Join(root, "ui", "vm.go"), "package ui\n")
	r22Write(t, filepath.Join(root, "ui", "r.go"), "package ui\n")
	r22Write(t, filepath.Join(root, "user.json"),
		`{"Replace":{"ui/vm.go":"ui/r.go","./ui/vm.go":"ui/r.go"}}`)
	_, err := userOverlayMap(`-mod=readonly "-overlay=user.json"`, root)
	if err == nil {
		t.Fatal("归一化重复路径必须 fail-closed（R21 保留）")
	}
}

// TestBehaviorCrossFieldRejected 跨包字段类型改（model.VM.Title int）应被 Analyzer 拒（BIND-004）且不写盘。
func TestBehaviorCrossFieldRejected(t *testing.T) {
	root := r1Project(t)
	r22Write(t, filepath.Join(root, "model", "vm.go"), "package model\ntype VM struct { Title string }\n")
	r22Write(t, filepath.Join(root, "vm.txt"), "package model\ntype VM struct { Title int }\n")
	r22Write(t, filepath.Join(root, "views", "home", "doc.go"), "package home\n")
	r22Write(t, filepath.Join(root, "views", "home", "home.gxml"),
		`<Goxml Version="1" ViewModel="model.VM"><Page Route="/"><Text Value="{Title}"/></Page></Goxml>`)
	r22Write(t, filepath.Join(root, "user.json"), `{"Replace":{"model/vm.go":"vm.txt"}}`)
	r1SetEnv(t)
	code, _, se := runCLIStrings(t, filepath.Join(root, "views", "home", "home.gxml"))
	if code == ExitOK {
		t.Fatalf("跨包字段类型改应被拒，实际成功；stderr=%s", se)
	}
	if !strings.Contains(se, "GXML-BIND-004") {
		t.Fatalf("应报 GXML-BIND-004，got: %s", se)
	}
	if _, err := os.Stat(filepath.Join(root, "views", "home", "home_gen.go")); !os.IsNotExist(err) {
		t.Fatal("失败时不得写产物")
	}
}

// TestBehaviorDependencyFieldRejected 依赖闭包字段（data.Label string→int）应被拒（BIND-004）且不写盘。
func TestBehaviorDependencyFieldRejected(t *testing.T) {
	root := r1Project(t)
	r22Write(t, filepath.Join(root, "model", "vm.go"), "package model\nimport \"example.com/r1probe/data\"\ntype VM struct { Title data.Label }\n")
	r22Write(t, filepath.Join(root, "model", "doc.go"), "package model\n")
	r22Write(t, filepath.Join(root, "data", "types.go"), "package data\ntype Label = string\n")
	r22Write(t, filepath.Join(root, "vm.txt"), "package model\nimport \"example.com/r1probe/data\"\ntype VM struct { Title data.Label }\n")
	r22Write(t, filepath.Join(root, "types.txt"), "package data\ntype Label = int\n")
	r22Write(t, filepath.Join(root, "model", "home.gxml"),
		`<Goxml Version="1" ViewModel="VM"><Page Route="/"><Text Value="{Title}"/></Page></Goxml>`)
	r22Write(t, filepath.Join(root, "user.json"), `{"Replace":{"model/vm.go":"vm.txt","data/types.go":"types.txt"}}`)
	r1SetEnv(t)
	code, _, se := runCLIStrings(t, filepath.Join(root, "model", "home.gxml"))
	if code == ExitOK {
		t.Fatalf("依赖闭包字段改应被拒，实际成功；stderr=%s", se)
	}
	if !strings.Contains(se, "GXML-BIND-004") {
		t.Fatalf("应报 GXML-BIND-004，got: %s", se)
	}
	if _, err := os.Stat(filepath.Join(root, "model", "home_gen.go")); !os.IsNotExist(err) {
		t.Fatal("失败时不得写产物")
	}
}

// TestBehaviorCrossAddMethodAccepted 跨包 overlay 新增方法应成功生成且真实构建通过。
func TestBehaviorCrossAddMethodAccepted(t *testing.T) {
	root := r1Project(t)
	r22Write(t, filepath.Join(root, "model", "vm.go"), "package model\ntype VM struct {}\n")
	r22Write(t, filepath.Join(root, "vm.txt"), "package model\ntype VM struct {}\nfunc (*VM) Inc() {}\n")
	r22Write(t, filepath.Join(root, "views", "home", "doc.go"), "package home\n")
	r22Write(t, filepath.Join(root, "views", "home", "home.gxml"),
		`<Goxml Version="1" ViewModel="model.VM"><Page Route="/"><Button Text="go" Click="Inc"/></Page></Goxml>`)
	r22Write(t, filepath.Join(root, "user.json"), `{"Replace":{"model/vm.go":"vm.txt"}}`)
	r1SetEnv(t)
	code, _, se := runCLIStrings(t, filepath.Join(root, "views", "home", "home.gxml"))
	if code != ExitOK {
		t.Fatalf("跨包新增方法应成功生成，got rc=%d stderr=%s", code, se)
	}
	if _, err := os.Stat(filepath.Join(root, "views", "home", "home_gen.go")); err != nil {
		t.Fatal("应生成 home_gen.go")
	}
	cmd := exec.Command(r21Go(t), "build", "./views/home")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOENV=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly -overlay=user.json")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("生成后真实构建应通过: %v\n%s", err, out)
	}
}

// TestBehaviorUnusedMissingNotBlocking 非 Go 的未用缺失替换不应阻断（Go 决定）。
func TestBehaviorUnusedMissingNotBlocking(t *testing.T) {
	root := r1Project(t)
	r22Write(t, filepath.Join(root, "ui", "vm.go"), "package ui\ntype VM struct { Title string }\n")
	r22Write(t, filepath.Join(root, "ui", "home.gxml"),
		`<Goxml Version="1" ViewModel="VM"><Page Route="/"><Text Value="{Title}"/></Page></Goxml>`)
	r22Write(t, filepath.Join(root, "user.json"), `{"Replace":{"ui/notes.txt":"not-present.txt"}}`)
	r1SetEnv(t)
	code, _, se := runCLIStrings(t, filepath.Join(root, "ui", "home.gxml"))
	if code != ExitOK {
		t.Fatalf("非 Go 未用缺失替换不应阻断，got rc=%d stderr=%s", code, se)
	}
}
