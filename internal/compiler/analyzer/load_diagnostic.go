package analyzer

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// PackageLoadError 是包加载失败的结构化诊断。
//
// 背景（R2-qcode-09 / R3 切片1）：go/packages 的 Error.Pos 在语法/类型错误
// 场景可能为空，真正位置常内嵌在 Error.Msg（编译器格式 "file:line:col: msg"）。
// 旧实现只取 Msg 且不做解析，丢掉了可定位的文件/行列；本类型把位置结构化保留。
//
// 位置解析纪律：
//   - 绝不按「冒号首段」切分路径（会截断 Windows 盘符 C:\ 与含冒号内容）；
//   - 优先用 Error.Pos；为空则从 Msg 的行内 `*.go:line[:col]:` 令牌解析；
//   - 相对路径按 module root 解析为绝对路径，保证可定位；
//   - 解析不可靠时 HasPos=false：不伪造文件:行:列，但保留原文与包路径。
type PackageLoadError struct {
	ImportPath string
	File       string
	Line       int
	Col        int
	HasPos     bool
	RawPos     string
	Msg        string
}

func (e *PackageLoadError) Error() string {
	if e.HasPos {
		if e.Col > 0 {
			return fmt.Sprintf("package %q failed to load: %s:%d:%d: %s", e.ImportPath, e.File, e.Line, e.Col, e.Msg)
		}
		return fmt.Sprintf("package %q failed to load: %s:%d: %s", e.ImportPath, e.File, e.Line, e.Msg)
	}
	if strings.TrimSpace(e.RawPos) != "" {
		return fmt.Sprintf("package %q failed to load (pos %q): %s", e.ImportPath, e.RawPos, e.Msg)
	}
	return fmt.Sprintf("package %q failed to load: %s", e.ImportPath, e.Msg)
}

// parseErrorPos 从 go/packages 的 Error.Pos（"file:line[:col]"）解析文件与行列。
//
// 从右往左取数字段，避免把 Windows 盘符 C: 当作分隔；同时兼容仅 file:line。
func parseErrorPos(pos string) (file string, line, col int, ok bool) {
	pos = strings.TrimSpace(pos)
	if pos == "" {
		return "", 0, 0, false
	}
	i := strings.LastIndexByte(pos, ':')
	if i < 0 {
		return "", 0, 0, false
	}
	last, err := strconv.Atoi(pos[i+1:])
	if err != nil || last <= 0 {
		return "", 0, 0, false
	}
	rest := pos[:i]
	if rest == "" {
		return "", 0, 0, false
	}
	// 尝试第二段为行号；若其不是数字，则回退为「file:line」（唯一数字是行，列为 0）。
	if j := strings.LastIndexByte(rest, ':'); j >= 0 {
		if second, err2 := strconv.Atoi(rest[j+1:]); err2 == nil {
			file = rest[:j]
			if file == "" || second <= 0 {
				return "", 0, 0, false
			}
			return file, second, last, true
		}
	}
	return rest, last, 0, true
}

// msgPosRe 匹配编译器消息行内的位置令牌：任意（含 Windows 盘符/空格/Unicode）到 .go，
// 后接 :line 或 :line:col，且后面紧跟 ':' 或行尾。
var msgPosRe = regexp.MustCompile(`^(.+?\.go):(\d+)(?::(\d+))?:`)

// parseMessagePos 从编译器消息文本中提取第一个可靠位置。
func parseMessagePos(msg string) (file string, line, col int, ok bool) {
	for _, raw := range strings.Split(msg, "\n") {
		lineStr := strings.TrimSpace(raw)
		if lineStr == "" || strings.HasPrefix(lineStr, "#") {
			continue
		}
		m := msgPosRe.FindStringSubmatch(lineStr)
		if m == nil {
			continue
		}
		f := strings.TrimSpace(m[1])
		l, err1 := strconv.Atoi(m[2])
		c := 0
		if m[3] != "" {
			cc, err2 := strconv.Atoi(m[3])
			if err2 == nil {
				c = cc
			}
		}
		if f == "" || err1 != nil || l <= 0 {
			continue
		}
		return f, l, c, true
	}
	return "", 0, 0, false
}

// formatLoadFailure 把 go/packages 的 Errors 转成结构化错误。
//
// 只取第一条错误（与既有确定性契约一致：p.Errors 顺序由 go list 决定，不在此重排）；
// 保留原始 Msg 作为原因；位置优先 Pos，其次 Msg 文本；相对路径按 root 解析为绝对。
func formatLoadFailure(importPath, root string, errs []packages.Error) error {
	if len(errs) == 0 {
		return fmt.Errorf("package %q failed to load", importPath)
	}
	first := errs[0]
	e := &PackageLoadError{ImportPath: importPath, RawPos: first.Pos, Msg: first.Msg}

	f, l, c, ok := parseErrorPos(first.Pos)
	if !ok {
		f, l, c, ok = parseMessagePos(first.Msg)
	}
	if ok {
		if !filepath.IsAbs(f) && root != "" {
			f = filepath.Join(root, f)
		}
		e.File, e.Line, e.Col, e.HasPos = f, l, c, true
	}
	return e
}
