package preview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// hostModuleName 是隔离宿主临时模块的 module 路径。
//
// 它只存在于本线拥有的临时目录里，永远不与用户模块路径冲突。
const hostModuleName = "goxmlpreviewhost"

// defaultToolRoot 从本包源码位置上溯定位 goxml 模块根。
//
// 宿主 go.mod 的 `replace github.com/goxml/goxml => <toolRoot>` 需要它：
// 生成 View 与 pkg/runtime/app 必须来自**同一次构建所用的源码树**，
// 否则宿主编译的是另一个版本的 runtime，预览就不再同源。
//
// 失败语义（fail-closed，不猜）：
//   - 二进制用 -trimpath 构建时 runtime.Caller 给出的是模块相对路径，
//     上溯找不到 go.mod → 明确报错，要求经 Options.ToolRoot 显式指定；
//   - 找到 go.mod 但 module 路径不是 github.com/goxml/goxml → 同样报错，
//     不把「碰巧叫这个名字的目录」当源码根。
func defaultToolRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("preview: 无法定位本包源码路径（宿主 replace 需要 goxml 源码根）；请经 Options.ToolRoot 显式指定")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 10; i++ {
		if isGoxmlModuleDir(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("preview: 无法从源码路径上溯定位 goxml 模块根；-trimpath 构建的二进制不含磁盘路径，请经 Options.ToolRoot 显式指定")
}

// isGoxmlModuleDir 只做**身份核对**（module 行是否为 goxml），不是语义校验。
func isGoxmlModuleDir(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest) == "github.com/goxml/goxml"
		}
	}
	return false
}

// resolveToolRoot 解析 Options.ToolRoot（显式优先）或自动探测。
func resolveToolRoot(explicit string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("preview: 解析 ToolRoot: %w", err)
		}
		if !isGoxmlModuleDir(abs) {
			return "", fmt.Errorf("preview: ToolRoot %s 不是 github.com/goxml/goxml 模块根（go.mod module 行不匹配）", abs)
		}
		return abs, nil
	}
	return defaultToolRoot()
}
