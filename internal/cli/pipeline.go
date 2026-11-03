// Package cli 的 generate 执行阶段：发现输入、校验路径与所有权、判定缓存，
// 再接通 Parser → Expander → Analyzer → Codegen → 批量暂存与提交。
package cli

import (
	"errors"
	"fmt"
	"go/build"
	"go/token"
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

// runPipeline 执行生成：只读预检和生成全部成功后才暂存、提交本批产物。
//
// 决策落实（见 GENERATE-CLI-001-短检查记录 §6 与 1-3 边界冻结）：
//   - G2：SourceDir = 每个 .gxml 实际所在目录（单文件解析上下文）；
//   - 输入路径与输出路径**分开计算**：SourceDir 服务解析，--out-dir 服务布局；
//   - 多文件在提交前完成整批生成与暂存；暂存失败保留已有产物；
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

	// ---------- 阶段零前：测量一次有效 Go 构建上下文（R7-B B1）----------
	//
	// CLI 选择输出包名（packageDirName）、Analyzer 加载（go/packages 子进程）
	// 与缓存指纹（K10）必须看到**同一份**有效构建上下文。唯一可信来源是
	// go 自身（go env 含 GOENV 持久化值），每批测量一次、批内共享；
	// 不做跨调用缓存——两次 generate 之间 GOENV 变化必须可见。
	//
	// 测量失败 fail-closed（保留 cause）：包名若退回 build.Default 的
	// 环境投影，就是 R7-B 复现的静默分叉（GOENV 持久化 -tags/GOOS 下
	// 产物包名与 go list 相反）；这里没有比报错更安全的回退。
	// Analyzer 本就依赖 go 子进程（go/packages），该约束不新增环境要求。
	goExe := resolveGoExe()
	ctxValues, ctxErr := measureContextValues(goExe, moduleRoot)
	if ctxErr != nil {
		return fmt.Errorf("测量有效 Go 构建上下文失败（拒绝猜测包名/文件选择）: %w", ctxErr)
	}
	// A measurable workspace can still generate; only cache reuse is unsafe.
	cacheCtx, cacheCtxErr := ctxValues.cacheString()
	pkgCtxt, ctxErr := buildContextFromMeasured(ctxValues)
	if ctxErr != nil {
		return fmt.Errorf("解析有效 Go 构建上下文失败: %w", ctxErr)
	}
	// R9-A S1：构造批级「目标包权威解析器」（go list -json，批内复用）。
	// env=nil → 继承真实环境（与 measureContextValues 生产调用一致，二者看到同一
	// GOENV/GOFLAGS/GOEXPERIMENT/CPU）。仅用于 packageDirName 定名；不改 VerifiedDoc 准入。
	//
	// 定名时旧生成物仍在目标目录里：用 -overlay 把**本工具已登记产物**映射为
	// `//go:build ignore`（与 staleGeneratedOverlay 同语义），使 go list 只看到真实
	// 用户源，避免旧产物包名参与本次定名（任务要求保留自有旧生成物排除）。
	// 提前只读加载 Manifest（原在阶段零 B）；无 Manifest 时视为空。
	nameMan, manErr := loadManifest(manifestPath(moduleRoot))
	if manErr != nil {
		return manErr
	}
	overlayForDir := func(dir string) (map[string][]byte, error) {
		return targetNameOverlay(moduleRoot, nameMan, dir)
	}
	pkgResolver := newTargetPkgResolver(goExe, moduleRoot, nil, overlayForDir)

	// ---------- 阶段零：解析输入与目标（不写盘、不生成）----------
	resolved := make([]*resolvedInput, 0, len(inputs))
	targets := make([]string, 0, len(inputs))
	for _, in := range inputs {
		r, err := resolveInput(in, moduleRoot, outRoot, pkgCtxt, pkgResolver)
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
	man := nameMan // S1 定名阶段已只读加载，复用避免重复 IO。
	if err := validateManifestOutputPaths(moduleRoot, man); err != nil {
		return err
	}
	// 跨批次的产物所有权冲突（R2-qcode-03）：单文件形态 + --out-dir 会把输入
	// 相对路径压平（discoverInputs 对单文件只给 basename），于是
	//   goxml generate ui/home.gxml --out-dir gen    → gen/home_gen.go
	//   goxml generate admin/home.gxml --out-dir gen  → gen/home_gen.go（同一个文件）
	// 同批次内有 seen 拦着，跨批次此前无人拦：后一次静默覆盖前一次的产物，
	// Manifest 出现两条登记指向同一个文件，之后每次来回生成都判
	// output-modified 互相覆盖（真实 CLI 实测见 dev-log §2）。
	// 放在阶段零 B 末尾：此时仍然只有读操作，拦下来就是「磁盘无任何变更」。
	if err := checkOutputOwnership(moduleRoot, man, resolved); err != nil {
		return err
	}
	exclude := fingerprintExclusion(manifestOutputPaths(moduleRoot, man), targets)
	index := loadSourceIndex(moduleRoot)

	// 有效构建上下文已在阶段零前测量（每批一次，见上）；此处只把它
	// 绑定到本批每个输入，供 K10 与保守 miss 判定使用。
	for _, r := range resolved {
		r.goExe = goExe
		r.cacheContext = cacheCtx
		r.cacheContextReady = true
		r.dependencyUnsafe = cacheCtxErr != nil
	}

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
	goOverlay, err := staleGeneratedOverlay(moduleRoot, man, missResolved)
	if err != nil {
		return err
	}
	if err := precheckPackageSymbols(missResolved, moduleRoot, preparedFE, stderr, goOverlay); err != nil {
		return err
	}

	// ---------- 阶段一：只为未命中的输入生成产物（全部在内存中）----------
	// 本阶段必须保持**只读**：不建目录、不写文件、不碰磁盘现状。
	// 这样阶段一失败时，下面的「磁盘无任何变更」才是真话。
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

		// 复用阶段零 C 已算好的前端结果；缺失时（理论上不该发生）现算，
		// 宁可慢一次也不要静默跳过生成。
		prep, ok := preparedFE[d.r]
		if !ok {
			prep = pipeline.Prepare(d.r.sourcePath, d.r.source, pipeline.Config{
				ModuleRoot:           moduleRoot,
				SourceDir:            d.r.in.sourceDir,
				PackageName:          d.r.pkgName,
				OutputDir:            d.r.targetDir,
				ViewModelPackageName: d.r.srcPkgName,
				SourcePath:           d.r.sourcePath,
				GoOverlay:            goOverlay,
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
		// 输出目录**集中在此**创建，不准前移到阶段一的循环里。
		//
		// 反例（本文件曾经的真实形态）：MkdirAll 放在阶段一每个未命中输入之前，
		// 于是「阶段一生成失败」时目标目录已经被建出来，而错误信息却写着
		// 「磁盘无任何变更」——声称与事实不符。把写副作用全部收拢到阶段二开头，
		// 阶段一就真的是纯内存阶段，那条错误信息才站得住。
		// stageBatch 要求目标目录已存在，因此这一步必须排在它之前。
		// createdDirs 只记**真正新建**的目录（先 Stat 判存在）：暂存失败时
		// reportStageFailure 据此给出诚实措辞——建过目录就不能再声称「磁盘无任何变更」。
		var createdDirs []string
		for _, p := range preparedFiles {
			if _, err := os.Stat(p.r.targetDir); os.IsNotExist(err) {
				if err := os.MkdirAll(p.r.targetDir, 0o755); err != nil {
					return fmt.Errorf("创建输出目录 %s: %w", p.r.targetDir, err)
				}
				createdDirs = append(createdDirs, p.r.targetDir)
			} else if err != nil {
				return fmt.Errorf("检查输出目录 %s: %w", p.r.targetDir, err)
			}
		}
		sorted := sortTargets(missTargets)
		byTarget := make(map[string]prepared, len(preparedFiles))
		for _, p := range preparedFiles {
			byTarget[p.r.target] = p
		}
		staged, err := stageBatch(sorted, func(tmpPath string, i int) error {
			return os.WriteFile(tmpPath, byTarget[sorted[i]].artifact.Source, 0o644)
		})
		if err != nil {
			return reportStageFailure(err, createdDirs)
		}

		// ---------- 阶段二 B：提交（单文件原子替换；批次失败进程内回滚）----------
		commitResidue, err := commitBatchOps(staged, realOps)
		if err != nil {
			return reportCommitFailure(err, createdDirs)
		}
		// 产物已正确落盘；删不掉的旧备份只警告，不阻断 Manifest/缓存登记（与缓存写失败同策）。
		for _, r := range commitResidue {
			fmt.Fprintf(stderr, "警告：旧产物备份清理失败（不影响生成结果）：%s\n", r)
		}
		for _, t := range sorted {
			// 措辞按 TASK-GENERATE-CACHE-001 §5 冻结为 `已生成 <rel-output>`。
			// 同一轮里 `跳过 <rel-output>（缓存命中）` 与
			// `缓存未命中 <rel-output>：<原因>` 两行本来就是项目相对路径，只有这一行
			// 打绝对路径：它把用户机器的目录结构写进构建日志，脚本换机器后无法比对，
			// 而路径含空格/非 ASCII 时还会让同一份诊断在不同机器上形态不同。
			fmt.Fprintf(stdout, "已生成 %s\n", byTarget[t].r.outputRel)
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
func precheckPackageSymbols(rs []*resolvedInput, moduleRoot string, preparedFE map[*resolvedInput]*pipeline.Prepared, stderr io.Writer, goOverlay map[string][]byte) error {
	if len(rs) == 0 {
		return nil
	}
	targets := make([]codegen.NamingTarget, 0, len(rs))
	for _, r := range rs {
		cfg := pipeline.Config{
			ModuleRoot:           moduleRoot,
			SourceDir:            r.in.sourceDir,
			PackageName:          r.pkgName,
			OutputDir:            r.targetDir,
			ViewModelPackageName: r.srcPkgName,
			SourcePath:           r.sourcePath,
			GoOverlay:            goOverlay,
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

var errPackageUninitialized = errors.New("package directory is uninitialized")

// readPackageFile 是包名判定读取 Go 源文件的入口（包级变量仅为测试注入，
// 生产即 os.ReadFile）。R7-B B2：读取失败必须可消费，不得当作「文件不存在」
// 跳过——那会把损坏/受限的包目录伪装成空目录，走源包名回退写出错误产物。
var readPackageFile = os.ReadFile

// packageDirName 读目录内**非测试** Go 源文件的 package 子句作为包名。
//
// 目录没有可定名的 Go 源文件时报错——生成文件必须落在真实 Go 包里（契约 §3.2：
// 输出目录必须能解析为明确的 Go package）。
//
// 两条契约依据（`代码生成输出契约.md` §3.2、§4.1）：
//   - 「如果输出目录存在多个非测试 package，生成失败」→ 这里必须把冲突报出来，
//     而不是抓到一个算一个；
//   - 包名要能「从目标目录正确解析」→ 外部测试包 `package foo_test` 里的名字
//     **不是**该目录的构建包名。
//
// 为什么要专门排除 `*_test.go`（R2-qcode-02，本轮实测）：老实现按 ReadDir 顺序
// 取**第一个** `.go` 的 package 子句，测试文件也在候选里。`ui/home_test.go`
// （`package ui_test`）在字母序上排在 `ui/view.go` 前面，于是产物写成
// `package ui_test`，CLI 却报「已生成」退出码 0：
//
//	go build ./ui → found packages ui_test (home_gen.go) and ui (view.go)
//
// 这是任何「先写了外部测试文件、再生成页面」的普通工程都会踩到的形态，
// 不是构造出来的边界。排除测试文件同时与 K3 指纹的口径对齐
// （cache_wire.go 的 packageSourcesFingerprint 本来就不收 `_test.go`）。
//
// R7-B B2 边界冻结：只有**确实未初始化**（不存在/为空/只剩测试文件）的目录
// 走 errPackageUninitialized 供 --out-dir 回退源包名；匹配到构建选择的文件
// 读取失败、没有合法 package 子句、多包、上下文错误一律 fail-closed，
// 保留 cause——被选中的文件在 go build 眼里必然参与编译，CLI 静默跳过
// 等于把「坏包」当「空包」处理。
func packageDirName(dir string, ctxt build.Context, resolvers ...targetPkgResolverFn) (string, error) {
	// R9-A S1 生产接线：若注入了权威解析器（pipeline 阶段零按批设置），优先用 Go
	// 工具对目标有效环境的 `go list -json` 结果定名与校验选文件——这是唯一与 go list
	// 同源的口径，避免把宿主 build.Default.ToolTags 当目标（R8-A F3 分叉根因）。
	// 返回 ok=false（目录真正未初始化）时返回结构化未初始化错误，保留
	// 「首建新目录回退源包名」等既有兼容边界；返回 err（多包/读错/解析失败）则 fail-closed。
	if len(resolvers) > 0 && resolvers[0] != nil {
		s1 := resolvers[0]
		if name, ok, err := s1(dir); err != nil {
			return "", err
		} else if ok {
			return name, nil
		}
		return "", fmt.Errorf("目录 %s 没有可定名的非测试 Go 源文件: %w", dir, errPackageUninitialized)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("读取目录 %s: %w", dir, err)
	}
	// 按 ReadDir 的文件名序收集，包名集合至多一个才可用；报错信息里的顺序也是确定的。
	var name string
	var others []string
	seen := make(map[string]bool, 2)
	for _, e := range entries {
		name0 := e.Name()
		if e.IsDir() || filepath.Ext(name0) != ".go" || strings.HasSuffix(name0, "_test.go") {
			continue
		}
		// Match Go's build selection before collecting package declarations:
		// generator scripts and files for other platforms are not this package.
		match, err := ctxt.MatchFile(dir, name0)
		if err != nil {
			return "", fmt.Errorf("匹配 Go 构建文件 %s: %w", filepath.Join(dir, name0), err)
		}
		if !match {
			continue
		}
		data, err := readPackageFile(filepath.Join(dir, name0))
		if err != nil {
			// [R7-B B2] 读取失败不再 continue：该文件已通过构建选择，
			// go build/go list 也会读它。吞掉错误会让唯一真实源缺席，
			// 目录被误判为未初始化（--out-dir 下回退源包名 → 错误产物）。
			return "", fmt.Errorf("读取包源码 %s: %w", filepath.Join(dir, name0), err)
		}
		// A protection header alone is not ownership: an unregistered user file
		// may carry the same text. Match the manifest as well before excluding it.
		if codegen.HasProtectedHeader(data) {
			owned, err := packageArtifactOwned(filepath.Join(dir, name0), data)
			if err != nil {
				return "", err
			}
			if owned {
				continue
			}
		}
		pkg, ok := parsePackageName(data)
		if !ok {
			// [R7-B B2] 无合法 package 子句不再 continue：与 go 一致，
			// 被构建选择命中的文件必须有 package 子句，否则该目录在
			// go build 下本来就编译失败；静默跳过会把坏包当空包回退。
			return "", fmt.Errorf("文件 %s 没有可识别的 package 子句（契约 §3.2，目标目录须是明确的 Go package）",
				filepath.Join(dir, name0))
		}
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		if name == "" {
			name = pkg
			continue
		}
		others = append(others, pkg)
	}
	if name == "" {
		return "", fmt.Errorf("目录 %s 中没有可定名的非测试 Go 源文件，无法确定包名；请先在该目录初始化目标 package: %w", dir, errPackageUninitialized)
	}
	if len(others) > 0 {
		return "", fmt.Errorf("目录 %s 存在多个非测试 package（%s），无法确定生成文件的包名（契约 §3.2）",
			dir, strings.Join(append([]string{name}, others...), " / "))
	}
	return name, nil
}

// packageArtifactOwned checks the same two ownership conditions as cleanup.
// A directory outside a module cannot have a project manifest, so its source
// remains part of package naming. Manifest errors remain visible to the caller.
func packageArtifactOwned(path string, data []byte) (bool, error) {
	root, err := findModuleRoot(filepath.Dir(path))
	if err != nil {
		return false, nil
	}
	m, err := loadManifest(manifestPath(root))
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false, err
	}
	for _, e := range m.Entries {
		same, err := sameProjectFile(root, e.Output, filepath.ToSlash(rel), os.Stat)
		if err != nil {
			return false, err
		}
		if same && headerSourceMatches(data, filepath.Base(filepath.FromSlash(e.Source))) {
			return true, nil
		}
	}
	return false, nil
}

// parsePackageName 从 Go 源码提取 package 子句的名字。
//
// 这里有三处不能省，每一处都对应一个真实故障形态：
//
//  1. TrimSpace 必须裁**两端**，且要处理 `\r`：只裁前导空格的做法让
//     CRLF 存盘的文件得到 `mypkg\r` 这样的包名，生成产物的 package 子句
//     因此非法。Windows 上手写 .go 文件默认是 CRLF，这不是边缘场景。
//  2. 必须按空白取**第一个** token：`package main // 说明` 这种带行注释的
//     包声明，会把整条注释当成包名的一部分一起返回。
//  3. 名字必须是合法标识符：非法的包名不会在这里报错，而是一路流到 Codegen
//     才暴露，届时报错点离真正的成因很远，排查成本高得多。
//
// 轻量原则仍然成立——只引 go/token 做判定，不为三行解析引入 go/ast。
func parsePackageName(src []byte) (string, bool) {
	for _, line := range sourceLines(src) {
		line = strings.TrimSpace(line)
		rest, ok := cutPrefix(line, "package ")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if !token.IsIdentifier(name) {
			continue
		}
		return name, true
	}
	return "", false
}

// ---- 轻量文本辅助（避免为三行解析引入 go/ast）----

// sourceLines 把源码按换行切出。
//
// 原名 `errors_` 是为避开 errors 包而造的怪名，读起来像错误集合；
// 这里改成表意的名字，行为不变（保留最后一段未以换行结尾的内容）。
func sourceLines(src []byte) []string {
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

// cutPrefix 返回去掉前缀后的剩余部分（自实现只为避开 Go 版本差异，
// 语义与 strings.CutPrefix 一致）。
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
