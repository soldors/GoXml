// Package cli 的 generate 执行阶段（任务卡 1-2）：
// 接通 Parser → Expander → Analyzer → Codegen → Commit。
//
// 范围纪律：目录发现与 --out-dir 在 1-3；Manifest 与 --clean 归 1-6。
// 多文件提交语义 = G4 冻结的两阶段事务（commit.go）：全或无。
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goxml/goxml/internal/compiler/codegen"
	"github.com/goxml/goxml/internal/compiler/pipeline"
)

// pipelineFunc 是 generate 执行阶段的函数形态，包级变量便于测试注入。
var pipelineFunc = runPipeline

// pipelineHooks 是传给每个文件管线的钩子；零值即真实实现。
//
// 存在的唯一理由与 pipelineFunc 相同：让测试统计 **Codegen 被调用次数**，
// 用执行计数证明「缓存命中真的没有跑生成」，而不是拿耗时下降来推断
// （任务卡 §8 第 1 条）。
var pipelineHooks pipeline.Hooks

// runPipeline 执行生成（1-3）：发现输入 → 逐文件 管线 → Commit。
//
// 决策落实（见 GENERATE-CLI-001-短检查记录 §6 与 1-3 边界冻结）：
//   - G2：SourceDir = 每个 .gxml 实际所在目录（单文件解析上下文）；
//   - 输入路径与输出路径**分开计算**：SourceDir 服务解析，--out-dir 服务布局；
//   - 多文件**逐文件提交、无批次事务**——部分失败的回滚归 1-5（G4 未冻结，
//     本切片明确不宣称原子性），失败前已成功的文件保持原样；
//   - G6：--check 暂与默认行为相同（L1+L2 已是默认路径）。
func runPipeline(opts *generateOptions, stdout, stderr io.Writer) error {
	if len(opts.paths) > 1 {
		// 多个独立输入参数仍不支持：批量形态是「一个目录」，
		// 多根发现的归并留待需要时再议（避免与相对路径映射规则纠缠）。
		return fmt.Errorf("当前支持一个输入（文件或目录）；多个独立输入参数未支持")
	}
	// 发现：单文件或目录递归，结果已按 rel 排序（确定性，G5）。
	inputs, err := discoverInputs(opts.paths[0])
	if err != nil {
		return err
	}

	// 输出根：默认与各文件同目录（契约 §3.2）；
	// --out-dir 时映射为 outRoot/rel（去掉 .gxml 换 _gen.go）。
	var outRoot string
	if opts.outDir != "" {
		outRoot, err = filepath.Abs(opts.outDir)
		if err != nil {
			return fmt.Errorf("解析输出根目录: %w", err)
		}
		if fi, err := os.Stat(outRoot); err == nil && !fi.IsDir() {
			return fmt.Errorf("输出根 %q 不是目录", outRoot)
		}
	}

	// 所有输入必须属于**同一个 Go module**（Manifest 在项目根维护，契约 §10）。
	var moduleRoot string
	for _, in := range inputs {
		mr, err := findModuleRoot(in.abs)
		if err != nil {
			return err
		}
		if moduleRoot == "" {
			moduleRoot = mr
		} else if moduleRoot != mr {
			return fmt.Errorf("输入跨越多个 Go module（%s 与 %s）；Manifest 在项目根维护，不支持跨 module 批次", moduleRoot, mr)
		}
	}

	// 输出冲突检测：不同输入不得映射到同一目标文件。
	seen := make(map[string]string, len(inputs))

	// ---------- 阶段零：解析输入与目标（不写盘、不生成）----------
	resolved := make([]*resolvedInput, 0, len(inputs))
	targets := make([]string, 0, len(inputs))
	for _, in := range inputs {
		r, err := resolveInput(in, moduleRoot, outRoot)
		if err != nil {
			return err
		}
		if prev, dup := seen[r.target]; dup {
			return fmt.Errorf("输出冲突：%q 与 %q 都映射到 %s", prev, in.rel, r.target)
		}
		seen[r.target] = in.rel
		resolved = append(resolved, r)
		targets = append(targets, r.target)
	}

	// ---------- 阶段零 B：缓存判定（读 Manifest、算键、建 K3 排除集）----------
	man, err := loadManifest(manifestPath(moduleRoot))
	if err != nil {
		return err
	}
	exclude := fingerprintExclusion(manifestOutputPaths(moduleRoot, man), targets)
	index := loadSourceIndex(moduleRoot)

	type decision struct {
		r      *resolvedInput
		key    string
		hit    bool
		reason cacheMissReason
	}
	decisions := make([]decision, 0, len(resolved))
	for _, r := range resolved {
		key, err := cacheKeyFor(r, exclude)
		if err != nil {
			return err
		}
		hit, reason, err := decideCacheHit(moduleRoot, r, key, man, index, opts.noCache)
		if err != nil {
			return err
		}
		decisions = append(decisions, decision{r: r, key: key, hit: hit, reason: reason})
	}

	// ---------- 阶段零 C：包级符号预检（TASK-CODEGEN-NAMING-001 卡 §6.1）----------
	// 两阶段流程的"门"：预检通过才进阶段一。此处之前只有解析与缓存判定
	// （都是读），阶段一才会 MkdirAll / 生成 / 暂存，所以在这里返回即
	// 「磁盘无任何变更」（§6.2）；放到阶段一之后就保不住这条。
	missResolved := make([]*resolvedInput, 0, len(decisions))
	for _, d := range decisions {
		if !d.hit {
			missResolved = append(missResolved, d.r)
		}
	}
	// preparedFE 缓存未命中输入的编译前端结果：预检要用它算 bindSlots，
	// 阶段一要用它生成。跑两遍会让每次生成多花 ~285 ms/文件（实测，见 dev-log §6）。
	preparedFE := make(map[*resolvedInput]*pipeline.Prepared, len(missResolved))
	if err := precheckPackageSymbols(missResolved, moduleRoot, preparedFE, stderr); err != nil {
		return err
	}

	// ---------- 阶段一：只生成未命中的，并全部暂存（已有文件不被触碰）----------
	type prepared struct {
		r        *resolvedInput
		key      string
		artifact *pipeline.Artifact
	}
	var preparedFiles []prepared
	missTargets := make([]string, 0, len(decisions))
	hits := 0

	for _, d := range decisions {
		if d.hit {
			hits++
			fmt.Fprintf(stdout, "跳过 %s（缓存命中）\n", d.r.outputRel)
			continue
		}
		// 输出目录只为真正要写的文件创建：命中的文件不改动磁盘（连目录都不建）。
		if err := os.MkdirAll(d.r.targetDir, 0o755); err != nil {
			return fmt.Errorf("创建输出目录 %s: %w", d.r.targetDir, err)
		}

		// 复用阶段零 C 已算好的前端结果；缺失时（理论上不该发生）现算，
		// 宁可慢一次也不要静默跳过生成。
		prep, ok := preparedFE[d.r]
		if !ok {
			prep = pipeline.Prepare(d.r.sourcePath, d.r.source, pipeline.Config{
				ModuleRoot:  moduleRoot,
				SourceDir:   d.r.in.sourceDir,
				PackageName: d.r.pkgName,
				SourcePath:  d.r.sourcePath,
			}, pipelineHooks)
		}
		res := pipeline.RunPrepared(prep, pipelineHooks)

		for _, dg := range res.Diags {
			// 统一诊断格式：contracts.CompilerError.Error() 给出
			// `file:line:col: CODE: message`（契约 §13）。不要在这里手拼
			// 三元组——那样会丢掉行列，用户可见输出就与目录/规范不一致。
			fmt.Fprintln(stderr, dg.Error())
		}
		if !res.OK() {
			// 阶段一失败：尚未触碰任何磁盘文件（G4：全或无）。
			return fmt.Errorf("生成失败（%s，状态 %v，%d 条诊断）——批次全部放弃，磁盘无任何变更", d.r.in.rel, res.Status, len(res.Diags))
		}
		// --no-cache 不报原因：用户明确要求绕过，报「未命中」只是噪声。
		if d.reason != reasonBypassed {
			fmt.Fprintf(stdout, "缓存未命中 %s：%s\n", d.r.outputRel, d.reason)
		}
		preparedFiles = append(preparedFiles, prepared{r: d.r, key: d.key, artifact: res.Artifact})
		missTargets = append(missTargets, d.r.target)
	}

	// ---------- 阶段二：有未命中才写盘（全或无）；命中不碰任何文件 ----------
	if len(preparedFiles) > 0 {
		sorted := sortTargets(missTargets)
		byTarget := make(map[string]prepared, len(preparedFiles))
		for _, p := range preparedFiles {
			byTarget[p.r.target] = p
		}
		staged, err := stageBatch(sorted, func(tmpPath string, i int) error {
			return os.WriteFile(tmpPath, byTarget[sorted[i]].artifact.Source, 0o644)
		})
		if err != nil {
			if strings.Contains(err.Error(), "回滚时清理") {
				return fmt.Errorf("%w", err)
			}
			return fmt.Errorf("暂存失败（磁盘无任何变更）: %w", err)
		}

		// ---------- 阶段二 B：全部成功，原子提交 ----------
		if err := commitBatch(staged); err != nil {
			return fmt.Errorf("提交失败: %w", err)
		}
		for _, t := range sorted {
			fmt.Fprintf(stdout, "已生成 %s\n", t)
		}
		if len(sorted) > 1 {
			fmt.Fprintf(stdout, "共 %d 个文件生成成功（按路径排序处理）\n", len(sorted))
		}

		// ---------- Manifest 登记（契约 §10）----------
		// Source/Output 用项目相对路径（/ 分隔）；inputHash 用本次源内容。
		entries := make([]entry, 0, len(preparedFiles))
		for _, p := range preparedFiles {
			entries = append(entries, entry{
				Source:          p.r.sourceRel,
				Output:          p.r.outputRel,
				InputHash:       p.r.sourceHash,
				CompilerVersion: codegen.GoxmlVersion,
			})
		}
		if err := updateManifestAfterGenerate(moduleRoot, entries); err != nil {
			return fmt.Errorf("更新 Manifest: %w", err)
		}

		// ---------- 缓存登记（顺序：产物 → Manifest → 缓存，任务卡 §4）----------
		// 顺序不可倒置：先写缓存会让中断后留下「指向旧产物的有效缓存」= 假命中。
		for _, p := range preparedFiles {
			rec := newCacheRecordFor(p.r, p.key, p.artifact.Source)
			if err := saveCacheRecord(moduleRoot, rec); err != nil {
				// D-4 裁决：缓存写失败只警告——缓存不能成为新的失败源。
				fmt.Fprintf(stderr, "警告：缓存写入失败（不影响生成结果）：%v\n", err)
			}
		}
	}

	if hits > 0 {
		fmt.Fprintf(stdout, "命中 %d，生成 %d\n", hits, len(preparedFiles))
	}

	// ---------- --clean（在本次登记之后执行，保证 keep 集合正确）----------
	if opts.clean {
		// keep 必须包含**本轮全部在用源**（命中的也算）：只取 entries 会把
		// 刚被判为命中、本次没有重写的产物当孤儿删掉。
		keep := make(map[string]bool, len(resolved))
		for _, r := range resolved {
			keep[r.sourceRel] = true
		}
		deleted, skipped, err := cleanOrphans(moduleRoot, keep)
		if err != nil {
			return fmt.Errorf("清理失败: %w", err)
		}
		for _, d := range deleted {
			fmt.Fprintf(stdout, "已清理 %s\n", d)
		}
		for _, s := range skipped {
			fmt.Fprintf(stdout, "跳过 %s\n", s)
		}
		fmt.Fprintf(stdout, "清理完成：删除 %d，跳过 %d\n", len(deleted), len(skipped))
	}
	return nil
}

// precheckPackageSymbols 对本次**将要写入**的输入做包级符号预检。
//
// 只取未命中（miss）的输入，理由有两条：
//   - 命中的产物本次不会被重写；把它算进批次会让「全部命中、磁盘毫无变化」
//     的运行因既有状态而失败——那不是本次生成引入的问题，不该由 CLI 拦下；
//   - 命中的产物仍在磁盘上，会被 existingPackageSymbols 扫到，因此
//     「命中产物 × 未命中输入」的撞名照样报得出来，只是归类为 existing
//     而不是 same-batch（语义仍然成立：那个符号确实已被包内文件占用）。
//
// C5 起按 `bindSlots > 0` 收窄（卡 §10.4.2 第 1 条限制的消除）：
// 静态页与纯事件页的产物**不声明** `goxmlTextCache<Source>`，因此不再把它
// 计入符号集合；否则包里只要恰好有同名用户符号，合法工程就生成不了。
// 槽位数由 pipeline.SlotsFor（与生成共用同一条编译前端）算出。
//
// 前端未通过（语法/绑定错误）时**回退为保守上界**：那种情况阶段一会报编译
// 错误，预检不该抢在前面声称自己"确定"槽位数，更不能把语法错误伪装成命名冲突。
//
// 有诊断即返回 error：调用方（runPipeline）在阶段一之前调用，故磁盘零变更。
func precheckPackageSymbols(rs []*resolvedInput, moduleRoot string, preparedFE map[*resolvedInput]*pipeline.Prepared, stderr io.Writer) error {
	if len(rs) == 0 {
		return nil
	}
	targets := make([]codegen.NamingTarget, 0, len(rs))
	for _, r := range rs {
		cfg := pipeline.Config{
			ModuleRoot:  moduleRoot,
			SourceDir:   r.in.sourceDir,
			PackageName: r.pkgName,
			SourcePath:  r.sourcePath,
		}
		// 前端只跑一次：结果同时供预检（槽位数）与阶段一（生成）使用。
		prep := pipeline.Prepare(r.sourcePath, r.source, cfg, pipelineHooks)
		preparedFE[r] = prep

		// 槽位数：known=false 表示前端未通过，预检按保守上界处理。
		slots, slotsKnown := 0, false
		if n, err := prep.Slots(); err == nil {
			slots, slotsKnown = n, true
		}
		targets = append(targets, codegen.NamingTarget{
			SourceAbs:      r.in.abs,
			SourcePath:     r.sourcePath,
			TargetDir:      r.targetDir,
			OutputPath:     r.target,
			BindSlots:      slots,
			BindSlotsKnown: slotsKnown,
		})
	}
	diags, err := codegen.CheckNamingConflicts(targets)
	if err != nil {
		return fmt.Errorf("包级符号预检失败: %w", err)
	}
	if len(diags) == 0 {
		return nil
	}
	for _, dg := range diags {
		// 走统一格式化入口：命名冲突的 span 是零值，输出 `file:0:0: CODE: message`
		// ——正是卡 §10.7.3 冻结的形态。手拼 "%s: %s: %s" 会丢掉 `:0:0:`。
		fmt.Fprintln(stderr, dg.Error())
	}
	return fmt.Errorf("包级符号冲突（%d 条）——批次全部放弃，磁盘无任何变更", len(diags))
}

// findModuleRoot 从 start 向上找 go.mod。
//
// 失败信息必须给出**可执行的修复动作**（AC-002-01 实测：临时目录试用时
// 「未找到 go.mod」不告知解法，用户无从下手）。
func findModuleRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s 不在任何 Go module 内：Goxml 生成文件必须落在 Go 包里；请先在工程根执行 `go mod init <module路径>`", start)
		}
		dir = parent
	}
}

// packageDirName 读目录内任一 .go 文件的 package 子句作为包名。
//
// 目录没有 Go 文件时报错——生成文件必须落在真实 Go 包里（契约 §3.2：
// 输出目录必须能解析为明确的 Go package）。
func packageDirName(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("读取目录 %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if name, ok := parsePackageName(data); ok {
			return name, nil
		}
	}
	return "", fmt.Errorf("目录 %s 中没有 Go 源文件，无法确定包名", dir)
}

// parsePackageName 从 Go 源码提取 package 子句（极简实现，够用即可）。
func parsePackageName(src []byte) (string, bool) {
	for _, line := range errors_(src) {
		line = trimSpace(line)
		if rest, ok := cutPrefix(line, "package "); ok && rest != "" {
			return rest, true
		}
	}
	return "", false
}

// ---- 轻量文本辅助（避免为三行解析引入 go/ast）----

// errors_ 把源码按行切出（名字带下划线避免与 errors 包冲突的阅读负担）。
func errors_(src []byte) []string {
	return splitLines(string(src))
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}

// runGeneratePipeline 执行 generate 的运行阶段（1-1 的入口点，1-2 接真实管线）。
func runGeneratePipeline(opts *generateOptions, stdout, stderr io.Writer) error {
	if pipelineFunc == nil {
		return errors.New("internal: generate pipeline is nil")
	}
	return pipelineFunc(opts, stdout, stderr)
}
