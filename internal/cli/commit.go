package cli

import (
	"errors"
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
//   - 阶段二（提交）：全部成功后逐个 rename 覆盖落盘；
//   - 失败回滚：提交前备份已有普通目标；任一文件失败时恢复已替换目标、
//     撤回本批新建目标并清理临时文件，保留旧内容、权限、mtime；
//   - 回滚本身失败（临时文件删不掉）必须上报，不得静默。
//
// 回滚对象 = 本批次已提交目标及已备份的当前目标；回滚本身失败须报告。
//
// 失败一律以 *txnError 结构化上报（op/path/cause/residue），调用方用
// errors.As/Is 消费，不再靠匹配中文错误串来分类——后者既脆弱又会把
// 「清理失败」与「普通暂存失败」混为一谈。
// ============================================================================

// stagedFile 是一个已暂存的待提交产物。
type stagedFile struct {
	// target 是最终目标路径（绝对路径）。
	target string
	// tmp 是临时文件路径（与 target 同目录，保证 rename 原子性）。
	tmp string
}

// txnError 是一次事务失败的结构化记录。
//
//   - op：失败发生的操作（暂存 / 提交 / 恢复…），供调用方分类，不靠解析文本；
//   - path：触发失败的具体路径（绝对），便于定位；
//   - cause：原始**主**错误，Unwrap 保留它，errors.Is/As 可一路穿透；
//   - extraCauses：伴随主错误发生的**次**错误（写入失败后 close 也失败、清理时
//     多个 remove 失败）。N1/N2：这些成因此前被 `_ = ops.close(tmp)` 与只返回
//     路径的 cleanup 丢弃，调用方无从追因。保留它们经 Unwrap 一并暴露，
//     errors.Is/As 能追到初始、close、remove 每一个原因；主错误 cause 不被顶替。
//   - residue：回滚清理后仍删不掉、需要人工检查的临时/备份文件（绝对路径）。
//     它非空就意味着「磁盘并非无变更」，调用方据此给出诚实的措辞。
//
// 提交期失败还会带上回滚的实际结果（其余阶段为空）：
//   - restored：已用备份恢复原样的已有目标（字节/权限/mtime 原样）；
//   - withdrawn：已撤回的新建目标（本批之前不存在，失败后删掉）；
//   - unrestored：恢复或撤回本身失败、未能回到原状的目标（需人工处理）。
type txnError struct {
	op          string
	path        string
	cause       error
	extraCauses []error
	residue     []string
	restored    []string
	withdrawn   []string
	unrestored  []string
}

func (e *txnError) Error() string {
	var b strings.Builder
	b.WriteString(e.op)
	b.WriteString("失败")
	if e.path != "" {
		fmt.Fprintf(&b, "（%s）", e.path)
	}
	fmt.Fprintf(&b, ": %v", e.cause)
	// N1/N2：次错误（close/remove 成因）如实 surfaced，不只丢给 errors.Is。
	for _, ec := range e.extraCauses {
		fmt.Fprintf(&b, "；附带失败: %v", ec)
	}
	if len(e.restored) > 0 || len(e.withdrawn) > 0 || len(e.unrestored) > 0 {
		fmt.Fprintf(&b, "；回滚结果：已恢复原目标 %d，已撤回新建 %d，未恢复 %d",
			len(e.restored), len(e.withdrawn), len(e.unrestored))
		if len(e.unrestored) > 0 {
			fmt.Fprintf(&b, "（需人工处理: %s）", strings.Join(e.unrestored, "; "))
		}
	}
	if len(e.residue) > 0 {
		fmt.Fprintf(&b, "；回滚未净，残留 %d 项需人工检查: %s", len(e.residue), strings.Join(e.residue, "; "))
	}
	return b.String()
}

// Unwrap 返回主错误与全部次错误（N1/N2）。errors.Is/As 会遍历每一个，
// 因此初始、close、remove 的原始 cause 都可穿透；主错误排在首位、不被顶替。
// 无次错误时仅含主错误，链形态与单 cause 等价。
func (e *txnError) Unwrap() []error {
	return append([]error{e.cause}, e.extraCauses...)
}

// fileOps 是事务用到的可失败文件系统操作，按值参数注入以便受控故障复现。
//
// 生产路径恒用 realOps；测试构造局部副本替换其中某个函数，**从不改写包级
// 变量或全局函数**（避免污染其它用例）。Close 失败在本地 NTFS 上几乎无法用
// 真实手段强制，故经由这里受控注入；rename/remove 失败另有真实文件系统复现
// （rename 到已存在目录、syscall.Open 持无 DELETE 共享句柄）。
type fileOps struct {
	lstat  func(string) (os.FileInfo, error)
	close  func(*os.File) error
	remove func(string) error
	rename func(oldpath, newpath string) error
}

var realOps = fileOps{
	lstat:  os.Lstat,
	close:  func(f *os.File) error { return f.Close() },
	remove: os.Remove,
	rename: os.Rename,
}

// stageBatch 执行阶段一：把全部产物写入各自目标目录的临时文件（真实操作）。
func stageBatch(targets []string, write func(tmpPath string, i int) error) ([]stagedFile, error) {
	return stageBatchOps(targets, write, realOps)
}

// stageBatchOps 是阶段一的实现，ops 注入文件系统操作。
//
// 返回 (已暂存列表, 失败错误)。任一失败即清理**已写入的**临时文件（含当前
// 失败项自身），并把清理不掉的收进 txnError.residue；未处理到的文件不产生
// 任何磁盘痕迹。write 由调用方注入（真实实现写 Artifact.Source；测试可注入失败）。
func stageBatchOps(targets []string, write func(tmpPath string, i int) error, ops fileOps) ([]stagedFile, error) {
	var staged []stagedFile
	for i, target := range targets {
		dir := filepath.Dir(target)
		// 目标目录必须已存在（由调用方创建）；不存在或不是目录都是调用方错误。
		fi, statErr := os.Stat(dir)
		if statErr != nil || !fi.IsDir() {
			cause := statErr
			if cause == nil {
				cause = errors.New("目标路径不是目录")
			}
			residue, removeCauses := cleanup(staged, ops)
			return nil, &txnError{op: "暂存", path: dir, cause: cause, extraCauses: removeCauses, residue: residue}
		}
		tmp, err := os.CreateTemp(dir, ".goxml-*.tmp")
		if err != nil {
			residue, removeCauses := cleanup(staged, ops)
			return nil, &txnError{op: "暂存", path: dir, cause: fmt.Errorf("创建临时文件: %w", err), extraCauses: removeCauses, residue: residue}
		}
		tmpName := tmp.Name()
		if err := write(tmpName, i); err != nil {
			// N1：写入失败后仍要 close；close 若也失败，其成因不能丢——作为次
			// 错误保留（主错误仍是写入失败，不被 close 顶替）。
			var extra []error
			if cerr := ops.close(tmp); cerr != nil {
				extra = append(extra, fmt.Errorf("关闭暂存文件: %w", cerr))
			}
			// 当前失败项的临时文件也要尝试清理：它已经躺在目标目录里。
			residue, removeCauses := cleanup(append(staged, stagedFile{target: target, tmp: tmpName}), ops)
			extra = append(extra, removeCauses...)
			return nil, &txnError{op: "暂存", path: tmpName, cause: fmt.Errorf("写入暂存: %w", err), extraCauses: extra, residue: residue}
		}
		if err := ops.close(tmp); err != nil {
			residue, removeCauses := cleanup(append(staged, stagedFile{target: target, tmp: tmpName}), ops)
			return nil, &txnError{op: "暂存", path: tmpName, cause: fmt.Errorf("关闭暂存文件: %w", err), extraCauses: removeCauses, residue: residue}
		}
		staged = append(staged, stagedFile{target: target, tmp: tmpName})
	}
	return staged, nil
}

// committedFile 记录一个已成功 rename 落盘的产物及其回滚所需信息。
type committedFile struct {
	target string
	// backup 是提交前移走的旧目标（同目录）；为空表示该目标本批之前不存在（新建）。
	backup string
}

// commitBatch 执行阶段二（真实操作）。返回的 error 非 nil 即批次失败；
// 成功时即便有清理不掉的旧备份也不视为失败（见 commitBatchOps）。
func commitBatch(staged []stagedFile) error {
	_, err := commitBatchOps(staged, realOps)
	return err
}

// commitBatchOps 是阶段二的实现，ops 注入文件系统操作。
//
// 契约依据（代码生成输出契约 §9）：「磁盘写入失败」时「旧生成文件必须保持不变」。
// 逐个 rename 是**单文件**原子替换，但批次跨多个文件：若第 k 个 rename 失败，
// 前 k-1 个已被新产物覆盖，旧文件就不复存在——这违反 §9。因此提交前先把每个
// **已有普通文件**目标 rename 到同目录备份（rename 不改字节/权限/mtime），失败时
// 用备份恢复旧目标、撤回新建目标，使整批回到提交前的磁盘状态。
//
// 这是**进程内可检测错误**的回滚，不是多文件 OS 原子，也不保证断电/崩溃恢复：
// 进程一旦中途消失，备份可能仍在而产物已部分覆盖，那不在本机制承诺之内。
//
// 安全边界：目标若是目录等非普通文件，**绝不备份移走**（否则成功路径删备份会
// 连用户目录一起删掉）；显式拒绝提交并回滚此前已提交项，当前目标原样保留。
//
// 返回值：
//   - 成功：err=nil；successResidue 列出删不掉的旧备份（产物已正确落盘，按警告
//     处理而非失败，避免阻断后续 Manifest/缓存登记——与缓存写失败同策）；
//   - 失败：err=*txnError，带 restored/withdrawn/unrestored/residue 的实际回滚状态，
//     Unwrap 保留原始 rename 错误；本批临时文件全部尝试清理。
func commitBatchOps(staged []stagedFile, ops fileOps) (successResidue []string, err error) {
	var committed []committedFile
	// fail: E1/E4/E5 与 rename(tmp→target) 失败的**统一收口**。
	// rollbackList 为已 rename 落盘、需要恢复的项（tmp→target 失败时另把当前已备份旧目标追加进来）。
	fail := func(path string, cause error, rollbackList []committedFile) error {
		restored, withdrawn, unrestored, bakResidue := rollbackCommit(rollbackList, ops)
		cleanResidue, cleanCauses := cleanup(staged, ops)
		residue := append(bakResidue, cleanResidue...)
		return &txnError{
			op: "提交", path: path, cause: cause, extraCauses: cleanCauses, residue: residue,
			restored: restored, withdrawn: withdrawn, unrestored: unrestored,
		}
	}
	for _, sf := range staged {
		backup := ""
		fi, statErr := ops.lstat(sf.target)
		switch {
		case statErr == nil && fi.Mode().IsRegular():
			// 已有普通文件：先移到同目录备份，保留其字节/权限/mtime。
			backup = sf.tmp + ".bak"
			if err := ops.rename(sf.target, backup); err != nil {
				// E1：备份旧目标失败 → 本项旧目标仍在原地，但**此前已提交项**已被新内容
				// 覆盖，必须恢复它们，不能只清 tmp 就返回。
				return nil, fail(sf.target, fmt.Errorf("备份原目标: %w", err), committed)
			}
		case statErr == nil:
			// E5：Lstat 识别为**非普通文件**（符号链接/目录/junction/reparse 等）。
			// 不备份、不替换、不删除用户对象：显式拒绝进入提交，并回滚此前已提交项。
			// （正常 CLI 已有叶子 symlink 前置拒绝；这是事务层防御。）
			return nil, fail(sf.target,
				fmt.Errorf("拒绝提交到非普通文件目标（mode=%s）", fi.Mode().String()), committed)
		case os.IsNotExist(statErr):
			// 新建目标：无备份。
		default:
			// E4：Lstat 返回非 IsNotExist 的真实错误。当前目标不动，同时回滚此前已提交项，
			// 与 E1/E5 同一收口。
			return nil, fail(sf.target, statErr, committed)
		}

		if err := ops.rename(sf.tmp, sf.target); err != nil {
			// 本项若已把旧目标移到备份，恢复时也要算上它（新产物没落盘，旧目标得回去）。
			rollbackList := committed
			if backup != "" {
				rollbackList = append(rollbackList, committedFile{target: sf.target, backup: backup})
			}
			return nil, fail(sf.target, err, rollbackList)
		}
		committed = append(committed, committedFile{target: sf.target, backup: backup})
	}

	// 全部成功：旧备份已完成使命，删除之（只删本批自己产生的备份）。
	for _, c := range committed {
		if c.backup == "" {
			continue
		}
		if err := ops.remove(c.backup); err != nil && !os.IsNotExist(err) {
			successResidue = append(successResidue, c.backup)
		}
	}
	return successResidue, nil
}

// rollbackCommit 把已提交项恢复原状：有备份的 rename 回目标（字节/权限/mtime 原样），
// 新建的 remove 撤回。恢复/撤回本身失败的记入 unrestored，删不掉的备份记入 residue。
// 逐项处理、互不影响：一项恢复失败不阻断其余项恢复。
func rollbackCommit(committed []committedFile, ops fileOps) (restored, withdrawn, unrestored, residue []string) {
	for _, c := range committed {
		if c.backup != "" {
			if err := ops.rename(c.backup, c.target); err != nil {
				unrestored = append(unrestored, c.target)
				residue = append(residue, c.backup)
			} else {
				restored = append(restored, c.target)
			}
			continue
		}
		if err := ops.remove(c.target); err != nil && !os.IsNotExist(err) {
			unrestored = append(unrestored, c.target)
		} else {
			withdrawn = append(withdrawn, c.target)
		}
	}
	return restored, withdrawn, unrestored, residue
}

// cleanup 删除本批临时文件，返回删不掉而残留的路径**及其原始 remove 错误**
// （N2：不静默、不丢结果、不丢成因）。已不存在（IsNotExist，例如已 rename 走的）
// 按成功忽略，既不残留也不产生 cause。
func cleanup(staged []stagedFile, ops fileOps) (residue []string, causes []error) {
	for _, sf := range staged {
		if err := ops.remove(sf.tmp); err != nil && !os.IsNotExist(err) {
			residue = append(residue, sf.tmp)
			causes = append(causes, err)
		}
	}
	return residue, causes
}

// reportStageFailure 把暂存失败转成对磁盘现状**诚实**的错误声明。
//
// createdDirs 是本次真正新建的输出目录（已存在的不计）。三态：
//   - 有残留（txnError.residue 非空）：删不掉的临时文件还在，绝不能说无变更，
//     直接透传 txnError（其 Error() 已带残留清单）；
//   - 无残留但建过目录：产物没落盘，可目录确实新建了，如实点名；
//   - 既无残留也没建目录：这才是真正的「磁盘无任何变更」。
//
// 分类只看结构化字段（errors.As + residue/createdDirs），不匹配任何中文串。
func reportStageFailure(err error, createdDirs []string) error {
	var te *txnError
	if errors.As(err, &te) && len(te.residue) > 0 {
		return fmt.Errorf("暂存失败: %w", err)
	}
	if len(createdDirs) > 0 {
		return fmt.Errorf("暂存失败（已创建输出目录 %s；产物未落盘）: %w", strings.Join(createdDirs, "; "), err)
	}
	return fmt.Errorf("暂存失败（磁盘无任何变更）: %w", err)
}

// reportCommitFailure 把提交失败转成对磁盘现状诚实的错误声明。
//
// 提交失败已尝试进程内回滚（见 commitBatchOps）。三态：
//   - 回滚不彻底（unrestored 或 residue 非空）：透传 txnError，其 Error() 已列
//     未恢复目标与残留，绝不再声称恢复原状；
//   - 回滚彻底但建过目录：旧目标已恢复、新建已撤回，可空目录还在，如实点名；
//   - 回滚彻底且没建目录：磁盘真正恢复原状。
//
// 非 *txnError 的提交错误（理论上不该出现）保守透传，不妄称已回滚。
func reportCommitFailure(err error, createdDirs []string) error {
	var te *txnError
	if !errors.As(err, &te) {
		return fmt.Errorf("提交失败: %w", err)
	}
	if len(te.unrestored) > 0 || len(te.residue) > 0 {
		return fmt.Errorf("提交失败: %w", err)
	}
	if len(createdDirs) > 0 {
		return fmt.Errorf("提交失败（已回滚；新建输出目录 %s 保留）: %w", strings.Join(createdDirs, "; "), err)
	}
	return fmt.Errorf("提交失败（已回滚，磁盘恢复原状）: %w", err)
}

// sortTargets 返回排序后的目标路径（保证错误信息与处理顺序确定）。
func sortTargets(targets []string) []string {
	out := append([]string(nil), targets...)
	sort.Strings(out)
	return out
}
