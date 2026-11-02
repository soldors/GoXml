package cli

// ============================================================================
// PRODUCT-R5 后续（night-zcode-maintenance-r2）：孤儿清理的
// 所有权一致性与失败记账。
//
// 切片 1（所有权）：孤儿可删的判据从「保护头存在」收紧为三条同时成立：
//   1) 登记的源文件已消失（既有）；
//   2) 产物保护头的 `// Source:` 与该条登记的源基名一致——不一致说明
//      该文件的归属存在歧义（可能来自其他同名源），按用户侧处理跳过；
//   3) 没有其它**源仍存在**的登记声称同一输出——所有权有争议时绝不删除，
//      避免删掉仍被存活源引用的共享产物。
//
// 切片 2（失败记账）：删除/保存失败的路径不再丢弃已完成事实——
// 结构化 cleanFailure 携带 deleted/skipped/failed/pending 四类清单，
// cause 经 Unwrap 保留（errors.Is/As 可穿透），文案逐项诚实。
// ============================================================================

import (
	"fmt"
	"os"
	"strings"
)

// cleanFailure 是孤儿清理失败的结构化记录（切片 2）。
//
//   - op：失败发生的操作（删除 / 保存 Manifest）。
//   - path：触发失败的具体路径（绝对）。
//   - cause：原始错误，Unwrap 保留（errors.Is/As 穿透到 syscall/权限层）。
//   - deleted：本轮**已成功删除**的产物（相对路径，事实保留——不因后续失败丢弃）。
//   - skipped：已按规则跳过的产物及原因（用户接管 / 归属歧义 / 存活源争议）。
//   - failed：删除失败的产物（本次失败项）。
//   - pending：登记为孤儿但**尚未尝试**清理的产物（按 Manifest 顺序）。
type cleanFailure struct {
	op   string
	path string
	// causes 是本轮**全部**失败的原始原因（多个 remove 失败时逐项保留）。
	// 经 Unwrap() []error 全部暴露，errors.Is/As 可穿透到每一个失败
	// （片2 非作者审修：此前只保留最后一个 cause，前面的失败原因丢失）。
	causes  []error
	deleted []string
	skipped []string
	failed  []string
	pending []string
}

func (e *cleanFailure) Error() string {
	var b strings.Builder
	b.WriteString(e.op + "失败")
	if e.path != "" {
		fmt.Fprintf(&b, "（%s）", e.path)
	}
	if len(e.causes) > 0 {
		for i, cause := range e.causes {
			if i == 0 {
				b.WriteString(": ")
			} else {
				b.WriteString("; ")
			}
			fmt.Fprint(&b, cause)
		}
	}
	if len(e.deleted) > 0 {
		fmt.Fprintf(&b, "；已删除 %d 项（%s）", len(e.deleted), strings.Join(e.deleted, "; "))
	}
	if len(e.skipped) > 0 {
		fmt.Fprintf(&b, "；已跳过 %d 项（%s）", len(e.skipped), strings.Join(e.skipped, "; "))
	}
	if len(e.failed) > 0 {
		fmt.Fprintf(&b, "；本次删除失败 %d 项（%s）", len(e.failed), strings.Join(e.failed, "; "))
	}
	if len(e.pending) > 0 {
		fmt.Fprintf(&b, "；尚未执行 %d 项（%s）", len(e.pending), strings.Join(e.pending, "; "))
	}
	b.WriteString("。解除占用或修复后重新运行 --clean 可继续剩余维护")
	return b.String()
}

// Unwrap 返回全部失败原因（片2 非作者审修：多项删除失败时 errors.Is/As 可穿透
// 每一个，而不是只留最后一个）。
func (e *cleanFailure) Unwrap() []error { return e.causes }

// cleanOps 是孤儿清理的文件操作集，按值注入以便受控故障复现
// （与 commit.go 的 fileOps 同模式；生产路径恒用 realCleanOps）。
type cleanOps struct {
	remove   func(string) error
	lstat    func(string) (os.FileInfo, error)
	readFile func(string) ([]byte, error)
	// saveManifest 注入用于测试 Manifest 保存失败（真实路径恒为 saveManifest）。
	saveManifest func(string, *manifest) error
}

var realCleanOps = cleanOps{
	remove:       os.Remove,
	lstat:        os.Lstat,
	readFile:     os.ReadFile,
	saveManifest: saveManifest,
}
