package docgate

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ============================================================================
// 门禁：dev-log 提交覆盖
//
// 设计约束见 docgate.go 的包注释。核心两条：
//  1. 排除 HEAD（自引用问题）；
//  2. git 不可用或失败必须让测试失败，绝不静默通过。
// ============================================================================

// recentCommits 是默认检查的提交数（不含 HEAD）。
const recentCommits = 10

// repoRoot 从包目录向上定位模块根。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("向上未找到 go.mod")
		}
		dir = parent
	}
}

// readGitLog 返回「跳过 HEAD 之后」的最近 n 个提交，格式为 "hash subject"。
func readGitLog(root string, n int) ([]string, error) {
	// -n (n+1) 然后 --skip=1：多取一个再跳过 HEAD。
	args := []string{"-C", root, "log", "-n", itoa(n + 1), "--skip=1", "--format=%h %s"}
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// gitLog 返回「跳过 HEAD 之后」的最近 n 个提交，格式为 "hash subject"。
// 任何 git 失败都直接 t.Fatal——门禁不能在无法检查时通过。
func gitLog(t *testing.T, root string, n int) []string {
	t.Helper()
	lines, err := readGitLog(root, n)
	if err != nil {
		t.Fatalf("git log 执行失败（拒绝以通过蒙混）: %v", err)
	}
	return lines
}

// devLogText 拼接所有 dev-log 内容。
func devLogText(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "docs", "05-开发", "dev-log")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 dev-log 目录失败: %v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", e.Name(), err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// isLogMaintenance 判定一个提交是否属于「日志维护提交」。
//
// 这类提交只改 dev-log 或 CHANGELOG，其 hash 不可能出现在**自己**新增的
// 日志文本里——这是自引用问题的一个变体。规则必须是机械可判定的，
// 因此只看提交改了哪些文件，不看提交信息措辞（措辞可以被随意书写）。
//
// 判定：本次提交的文件改动**全部**落在 dev-log 目录或 CHANGELOG.md 内。
//
// `-c core.quotePath=false` 不是可选项：默认开启时，git 会把非 ASCII 路径
// 输出成 `docs/05-\345\274\200\345\217\221/dev-log/...`，下面的
// `strings.Contains(f, "docs/05-开发/dev-log/")` 永远匹配不上，
// 于是**每一个**只改 dev-log 的提交都会被误判为「非维护提交」而报红
// （a02edd6 就是这样被抓出来的——它此前是 HEAD，被门禁排除，直到 HEAD 前移）。
func isLogMaintenance(t *testing.T, root, hash string) bool {
	t.Helper()
	cmd := exec.Command("git", "-c", "core.quotePath=false", "-C", root,
		"show", "--name-only", "--format=", hash)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git show %s 失败（拒绝以通过蒙混）: %v", hash, err)
	}
	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	any := false
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		any = true
		inDevLog := strings.Contains(f, "docs/05-开发/dev-log/")
		isChangelog := strings.HasSuffix(f, "CHANGELOG.md")
		if !inDevLog && !isChangelog {
			return false
		}
	}
	return any
}

// TestDevLogCoversRecentCommits 是门禁本体。
func TestDevLogCoversRecentCommits(t *testing.T) {
	root := repoRoot(t)
	logs := devLogText(t, root)
	commits := gitLog(t, root, recentCommits)

	missing, exempt, err := uncoveredCommits(logs, commits, func(hash string) bool {
		return isLogMaintenance(t, root, hash)
	})
	if err != nil {
		t.Fatal(err)
	}
	if exempt > 0 {
		t.Logf("已豁免 %d 个日志维护提交（提交内容仅涉及 dev-log/CHANGELOG）", exempt)
	}
	if len(missing) > 0 {
		for _, m := range missing {
			t.Errorf("未在 dev-log 记录: %s", m)
		}
		t.Logf("提示：dev-log 只检查提交号是否出现。内容准确性与 CHANGELOG 语义判断" +
			"不在本门禁范围内。")
	}
}

func uncoveredCommits(logs string, commits []string, isMaintenance func(string) bool) ([]string, int, error) {
	if len(commits) == 0 {
		return nil, 0, fmt.Errorf("提交范围为空——这是配置问题，不是通过")
	}
	var missing []string
	var exempt int
	for _, commit := range commits {
		hash := strings.SplitN(commit, " ", 2)[0]
		if strings.Contains(logs, hash) {
			continue
		}
		// 日志维护提交不要求在自身新增的日志里引用自己。
		if isMaintenance(hash) {
			exempt++
			continue
		}
		missing = append(missing, commit)
	}
	return missing, exempt, nil
}

// ============================================================================
// 门禁自身的负例：证明它真的会失败
//
// 不做反向验证的门禁等于没有门禁——这是项目在 diagdoc 上踩过的坑
// （清单声称未实现，而测试只校验清单自己，一直是绿的）。
// ============================================================================

// TestGateDetectsMissingCommit 证明「未记录的提交」会被抓出。
func TestGateDetectsMissingCommit(t *testing.T) {
	commits := []string{"abc1234 change one", "def5678 change two"}
	missing, _, err := uncoveredCommits("本日志没有引用任何提交号。", commits, func(string) bool {
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != len(commits) {
		t.Fatalf("负例失效：应当全部判为未记录，实际只识别出 %d/%d", len(missing), len(commits))
	}
}

// TestGateDetectsEmptyRange 证明「范围内没有提交」不会被当成通过。
func TestGateDetectsEmptyRange(t *testing.T) {
	_, _, err := uncoveredCommits("任意日志", nil, func(string) bool { return false })
	if err == nil || !strings.Contains(err.Error(), "提交范围为空") {
		t.Fatalf("门禁没有拒绝空提交范围：%v", err)
	}
}

// TestGitFailureIsFatal 证明 git 不可用时门禁不会静默通过。
func TestGitFailureIsFatal(t *testing.T) {
	// 用一个不存在的 PATH 让 exec.Command 找不到 git。
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", filepath.Join(repoRoot(t), "nonexistent-bin-dir"))

	if _, err := readGitLog(repoRoot(t), recentCommits); err == nil {
		t.Skip("环境中 git 通过绝对路径仍可执行，本负例不适用")
	}
	// 门禁包装函数将此错误升级为 t.Fatal；底层真实读取路径必须先报告失败。
}

// ============================================================================
// 门禁：Markdown 本地链接与 WBS 任务关系
// ============================================================================

var (
	markdownLinkPattern = regexp.MustCompile(`\]\(([^)]*)\)`)
	markdownFenceStart  = regexp.MustCompile("^\\s*(```+|~~~+)")
	markdownScheme      = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	taskIDPattern       = regexp.MustCompile(`TASK-[A-Z0-9]+(?:-[A-Z0-9]+)*`)
)

type markdownLinkScan struct {
	files int
	links int
	errs  []string
}

// scanMarkdownLinks 检查仓库 Markdown 中的行内本地链接目标是否存在。
// 外部 URL、纯锚点和围栏代码块中的示例不参与检查；标题锚点不做语义校验。
func scanMarkdownLinks(root string) markdownLinkScan {
	var scan markdownLinkScan
	err := filepath.WalkDir(root, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(file), ".md") {
			return nil
		}
		scan.files++

		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		var fence string
		for lineNo, line := range strings.Split(string(data), "\n") {
			if marker := markdownFenceStart.FindString(line); marker != "" {
				if fence == "" {
					fence = strings.TrimSpace(marker)
				} else if strings.HasPrefix(strings.TrimSpace(marker), fence[:3]) {
					fence = ""
				}
				continue
			}
			if fence != "" {
				continue
			}
			for _, match := range markdownLinkPattern.FindAllStringSubmatch(line, -1) {
				target := strings.TrimSpace(match[1])
				if target == "" {
					continue
				}
				if strings.HasPrefix(target, "<") {
					if end := strings.IndexByte(target, '>'); end >= 0 {
						target = target[1:end]
					}
				} else if fields := strings.Fields(target); len(fields) > 0 {
					target = fields[0]
				}
				if target == "" || strings.HasPrefix(target, "#") ||
					strings.HasPrefix(target, "//") || markdownScheme.MatchString(target) {
					continue
				}
				if hash := strings.IndexByte(target, '#'); hash >= 0 {
					target = target[:hash]
				}
				if query := strings.IndexByte(target, '?'); query >= 0 {
					target = target[:query]
				}
				if target == "" {
					continue
				}
				scan.links++

				decoded, decodeErr := url.PathUnescape(target)
				if decodeErr != nil {
					scan.errs = append(scan.errs, fmt.Sprintf("%s:%d: invalid link escape %q",
						filepath.ToSlash(file), lineNo+1, target))
					continue
				}
				var resolved string
				if strings.HasPrefix(decoded, "/") {
					resolved = filepath.Join(root, filepath.FromSlash(strings.TrimLeft(decoded, "/")))
				} else {
					resolved = filepath.Join(filepath.Dir(file), filepath.FromSlash(decoded))
				}
				if _, statErr := os.Stat(resolved); statErr != nil {
					rel, _ := filepath.Rel(root, file)
					scan.errs = append(scan.errs, fmt.Sprintf("%s:%d: missing local link target %q",
						filepath.ToSlash(rel), lineNo+1, decoded))
				}
			}
		}
		return nil
	})
	if err != nil {
		scan.errs = append(scan.errs, "Markdown scan failed: "+err.Error())
	}
	sort.Strings(scan.errs)
	return scan
}

func TestMarkdownLocalLinksExist(t *testing.T) {
	root := repoRoot(t)
	scan := scanMarkdownLinks(root)
	if len(scan.errs) > 0 {
		t.Fatalf("发现 Markdown 本地链接错误:\n%s", strings.Join(scan.errs, "\n"))
	}
	t.Logf("检查 %d 份 Markdown、%d 个行内本地链接目标", scan.files, scan.links)
}

func TestMarkdownLinkGateDetectsMissingTarget(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"),
		[]byte("[missing](docs/not-found.md)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scan := scanMarkdownLinks(root)
	if len(scan.errs) != 1 || !strings.Contains(scan.errs[0], "docs/not-found.md") {
		t.Fatalf("门禁未识别断链：%v", scan.errs)
	}
}

func validateWBS(contents string, cardExists func(string) bool) []string {
	var errs []string
	graph := make(map[string][]string)
	rows := make(map[string]bool)
	for lineNo, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 4 {
			continue
		}
		id := strings.TrimSpace(cells[0])
		if !strings.HasPrefix(id, "TASK-") {
			continue
		}
		if rows[id] {
			errs = append(errs, fmt.Sprintf("line %d: duplicate task %s", lineNo+1, id))
		}
		rows[id] = true
		if !cardExists(id) {
			errs = append(errs, fmt.Sprintf("missing task card for %s", id))
		}
		for _, dep := range taskIDPattern.FindAllString(cells[3], -1) {
			graph[id] = append(graph[id], dep)
		}
	}
	for id, deps := range graph {
		for _, dep := range deps {
			if !rows[dep] {
				errs = append(errs, fmt.Sprintf("%s depends on missing WBS task %s", id, dep))
			}
		}
	}

	state := make(map[string]uint8)
	var visit func(string)
	visit = func(id string) {
		if state[id] == 1 {
			errs = append(errs, fmt.Sprintf("dependency cycle includes %s", id))
			return
		}
		if state[id] == 2 {
			return
		}
		state[id] = 1
		for _, dep := range graph[id] {
			if rows[dep] {
				visit(dep)
			}
		}
		state[id] = 2
	}
	for id := range rows {
		visit(id)
	}
	sort.Strings(errs)
	return errs
}

func TestWBSHasTaskCardsAndAcyclicDependencies(t *testing.T) {
	root := repoRoot(t)
	wbsPath := filepath.Join(root, "docs", "04-WBS", "WBS总表.md")
	data, err := os.ReadFile(wbsPath)
	if err != nil {
		t.Fatal(err)
	}
	cardDir := filepath.Join(root, "docs", "04-WBS", "任务卡")
	entries, err := os.ReadDir(cardDir)
	if err != nil {
		t.Fatal(err)
	}
	var cardIDs []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "TASK-") && strings.HasSuffix(entry.Name(), ".md") {
			cardIDs = append(cardIDs, strings.TrimSuffix(entry.Name(), ".md"))
		}
	}
	errs := validateWBS(string(data), func(id string) bool {
		_, err := os.Stat(filepath.Join(cardDir, id+".md"))
		return err == nil
	})
	errs = append(errs, orphanTaskCards(string(data), cardIDs)...)
	sort.Strings(errs)
	if len(errs) > 0 {
		t.Fatalf("WBS 任务卡/依赖检查失败:\n%s", strings.Join(errs, "\n"))
	}
}

func orphanTaskCards(contents string, cards []string) []string {
	declared := make(map[string]bool)
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) > 0 {
			id := strings.TrimSpace(cells[0])
			if strings.HasPrefix(id, "TASK-") {
				declared[id] = true
			}
		}
	}
	var errs []string
	for _, id := range cards {
		if !declared[id] {
			errs = append(errs, fmt.Sprintf("task card %s is absent from WBS", id))
		}
	}
	sort.Strings(errs)
	return errs
}

func TestWBSValidatorDetectsMissingCardsDependenciesAndCycles(t *testing.T) {
	const wbs = `| Task ID | Module | Title | Dependencies | Acceptance |
|---|---|---|---|---|
| TASK-A-001 | test | A | TASK-B-001 | - |
| TASK-B-001 | test | B | TASK-A-001, TASK-MISSING-001 | - |
| TASK-C-001 | test | C | - | - |
`
	cards := map[string]bool{
		"TASK-A-001": true,
		"TASK-B-001": true,
	}
	errs := validateWBS(wbs, func(id string) bool { return cards[id] })
	joined := strings.Join(errs, "\n")
	for _, expected := range []string{
		"dependency cycle",
		"depends on missing WBS task TASK-MISSING-001",
		"missing task card for TASK-C-001",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("门禁未检测 %q；结果为:\n%s", expected, joined)
		}
	}
	if orphans := orphanTaskCards(wbs, []string{"TASK-ORPHAN-001"}); len(orphans) != 1 {
		t.Errorf("门禁未识别未登记的任务卡：%v", orphans)
	}
}
