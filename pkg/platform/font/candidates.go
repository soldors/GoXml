// Package font 提供各平台的 CJK 字体候选路径（字体规范 §2.3）。
//
// # 平台差异的封闭范围
//
// 规范 §1.6：跨平台行为差异只允许存在于 `pkg/platform` 与 `pkg/runtime/theme`。
// 本包只提供**路径常量与选择函数**，不做任何读取、解析或依赖判断。
//
// # 为什么三张表在同一个文件里，而不是 build tag 隔离
//
// `TASK-FONT-001-A` 的验收矩阵（A 卡 §5）要求「三平台候选表都要能被逻辑单测
// 覆盖，用 GOOS 注入」。若用 build tag 拆文件，非宿主机的候选表在本机**不可见**，
// 该验收项在本机就永远只能 SKIP——等于把「待验证」退化成「无法验证」。
// 因此改为：三张表同时存在，由 `CandidatesFor(goos)` 选择，
// `CJKCandidates()` 取 `runtime.GOOS`。平台差异仍完全封闭在本包内。
//
// 这是对 A 卡 §5「build tag 隔离」的一处**偏离**，理由与影响已登记在
// dev-log 2026-10-20 §24。
package font

import (
	"os"
	"path/filepath"
	"runtime"
)

// WindowsCJKCandidates 返回 Windows 的 CJK 候选字体（顺序即优先级）。
//
// 目录取自 `SystemRoot` 环境变量，缺失时回退 `C:\Windows\Fonts`
// （硬编码盘符在任何 Windows 上都成立，但优先尊重系统实际安装位置）。
func WindowsCJKCandidates() []string {
	dir := windowsFontDir()
	return []string{
		filepath.Join(dir, "msyh.ttc"),   // 微软雅黑
		filepath.Join(dir, "msyhbd.ttc"), // 微软雅黑 Bold
		filepath.Join(dir, "simsun.ttc"), // 宋体
		filepath.Join(dir, "simhei.ttf"), // 黑体
	}
}

func windowsFontDir() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "Fonts")
	}
	return `C:\Windows\Fonts`
}

// DarwinCJKCandidates 返回 macOS 的 CJK 候选字体。
func DarwinCJKCandidates() []string {
	return []string{
		"/System/Library/Fonts/PingFang.ttc",
		"/System/Library/Fonts/STHeiti Light.ttc",
		"/System/Library/Fonts/Hiragino Sans GB.ttc",
	}
}

// LinuxCJKCandidates 返回 Linux 的 CJK 候选字体。
//
// ⚠️ 明确**不包含** `fc-match`：规范 §2.3 规定外部命令只能作诊断信息，
// 不得作为实现路径（与平台矩阵「不伪装平台实现」一致）。
func LinuxCJKCandidates() []string {
	return []string{
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
		"/usr/share/fonts/truetype/arphic/uming.ttc",
	}
}

// CandidatesFor 返回指定 GOOS 的候选表；未知平台返回 nil。
//
// 导出它是为了让**任何**宿主机都能对三张表做逻辑断言（A 卡 §5）。
func CandidatesFor(goos string) []string {
	switch goos {
	case "windows":
		return WindowsCJKCandidates()
	case "darwin":
		return DarwinCJKCandidates()
	case "linux":
		return LinuxCJKCandidates()
	default:
		return nil
	}
}

// CJKCandidates 返回当前运行平台的候选表。
func CJKCandidates() []string {
	return CandidatesFor(runtime.GOOS)
}
