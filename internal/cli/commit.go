package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ============================================================================
// 1-5：两阶段事务提交（TASK-GENERATE-CLI-001-5，G4 已冻结）
//
// 冻结语义：
//   - 全或无（策略 A）：批次是原子单元；
//   - 阶段一（暂存）：全部文件生成并写入各自的临时文件；已有文件**不被触碰**；
//   - 阶段二（提交）：全部成功后逐个 os.Rename 覆盖落盘；
//   - 失败回滚：任一文件失败，删除本批次全部临时文件——原有目标的
//     内容、权限、mtime 保持原始未动；
//   - 回滚本身失败（临时文件删不掉）必须上报，不得静默。
//
// 回滚对象 = 本批次目标文件；已有文件在批次成功前不被触碰。
// ============================================================================

// stagedFile 是一个已暂存的待提交产物。
type stagedFile struct {
	// target 是最终目标路径（绝对路径）。
	target string
	// tmp 是临时文件路径（与 target 同目录，保证 rename 原子性）。
	tmp string
}

// stageBatch 执行阶段一：把全部产物写入各自目标目录的临时文件。
//
// 返回 (已暂存列表, 失败错误)。任一失败即删除**已写入的**临时文件并返回错误；
// 未处理到的文件不产生任何磁盘痕迹。
//
// write 函数由调用方注入（真实实现写 Artifact.Source；测试可注入失败），
// 收到临时文件路径与序号。
func stageBatch(targets []string, write func(tmpPath string, i int) error) ([]stagedFile, error) {
	var staged []stagedFile
	for i, target := range targets {
		dir := filepath.Dir(target)
		// 目标目录必须已存在（由调用方创建）；不存在是调用方错误。
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			cleanup(staged)
			return nil, fmt.Errorf("目标目录 %s 不可用: %w", dir, err)
		}
		tmp, err := os.CreateTemp(dir, ".goxml-*.tmp")
		if err != nil {
			cleanup(staged)
			return nil, fmt.Errorf("创建临时文件: %w", err)
		}
		tmpName := tmp.Name()
		if err := write(tmpName, i); err != nil {
			tmp.Close()
			_ = os.Remove(tmpName)
			cleanup(staged)
			return nil, fmt.Errorf("写入暂存 %s: %w", filepath.Base(target), err)
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpName)
			cleanup(staged)
			return nil, fmt.Errorf("关闭暂存文件: %w", err)
		}
		staged = append(staged, stagedFile{target: target, tmp: tmpName})
	}
	return staged, nil
}

// commitBatch 执行阶段二：把全部暂存文件原子 rename 到目标。
//
// rename 极少失败（同目录、目标无只读锁时）；一旦失败，已 rename 的
// 文件**无法**逐字节还原（rename 不是 copy），此时按冻结语义上报错误——
// 这属于环境级故障（磁盘满/权限突变），不是本工具能在进程内回滚的。
//
// 失败时必须清掉**本批全部**临时文件，包括当前这个失败的：它已经躺在目标
// 目录里，留着就是在用户的包目录里丢下一个 `.goxml-*.tmp`
// （TASK-GENERATE-CLI-001 退出条件 5「临时文件在失败后清理干净」）。
// 已经 rename 成功的条目也放进清理列表：它们的临时文件已被搬走，
// os.Remove 返回 IsNotExist，cleanup 按不存在忽略，不会误删已提交的产物
// （cleanup 只碰 tmp 路径，永远不碰 target）。
func commitBatch(staged []stagedFile) error {
	for _, sf := range staged {
		if err := os.Rename(sf.tmp, sf.target); err != nil {
			cleanupErr := cleanup(staged)
			if cleanupErr != nil {
				return fmt.Errorf("提交 %s 失败: %w；且回滚也失败: %w", filepath.Base(sf.target), err, cleanupErr)
			}
			return fmt.Errorf("提交 %s 失败: %w（已提交的文件保留；这是环境级故障，非进程内可回滚）", filepath.Base(sf.target), err)
		}
	}
	return nil
}

// cleanup 删除临时文件；失败收集为错误（不静默）。
func cleanup(staged []stagedFile) error {
	var errs []string
	for _, sf := range staged {
		if err := os.Remove(sf.tmp); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("%s: %v", sf.tmp, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("回滚时清理临时文件失败（需要人工检查）: %s", strings.Join(errs, "; "))
	}
	return nil
}

// sortTargets 返回排序后的目标路径（保证错误信息与处理顺序确定）。
func sortTargets(targets []string) []string {
	out := append([]string(nil), targets...)
	sort.Strings(out)
	return out
}
