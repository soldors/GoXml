# 2026-11-05 R22-A ANALYZER-OVERLAY-CONSISTENCY 集中返修 R1（DeepSeek）dev-log

父冻结 `a1e3d9103035b8b1fd4346c60b164169e8d9174b`（协调者 BLOCK），基线 `39f45ea07fdbd1c2aaab4fff24a1c452e4773a11`。分支 `codex/r22-deepseek-repair-r1`，树 `r22-deepseek-repair-r1`。旧冻结交付与协调者证据只读。
记录提交链：`39f45ea`（基线 docs）→ `d753d7a`（A2 初版修复）→ `a1e3d91`（dev-log）→ 本线 `5f67df3`（R1 返修）。

## 必修（协调者 review.md + probes/results.json + excluded-platform/results.json）

### F1/P1 跨包 VM 假接受/假拒绝 —— 修复
根因：`pipeline` 用 `r.in.sourceDir` 当 vmDirs，`views/home` 而 VM 在 `model` 时 `model/vm.go` 用户映射不进前端。
修复：改按 **Go 权威选源**。`scopedUserOverlayForAnalyzer(goExe,moduleRoot,vmImportPaths,userMap)` 对每个 VM 导入路径执行 `go list -deps -e -json -overlay=<用户overlay>`，收集 Go 实际选中文件，只对这些读替换源。`vmImportPathOf(r)` 由 `dependencyVM`+`typeExprDir`+modulePath 计算（同包=源目录包；跨包=modulePath/dir）。
实测（raw/repair-r1-raw/probes/results.json）：cross-field 由假接受 rc0 → **rc2 GXML-BIND-004**、不写盘；cross-add-method 由假拒绝 → **rc0 生成 + generatedBuild rc0**；same-field（同包正控制）仍 rc2 BIND-004。

### F2/P1 依赖闭包类型变更 —— 修复
`go list -deps` 天然覆盖 VM 包的本模块依赖闭包，故 `data.Label`(string→int) 被选中并读取。dependency-field 由假接受 rc0 → **rc2 GXML-BIND-004（data.Label=int）**、不写盘。
契约措辞：compiler-owned 内存快照仍成立；「用户来源」= GOFLAGS/GOENV 的 `-overlay` 中**被 Go 选中**的文件，仅读其替换源字节，不扩展 analyzer.Config.Overlay 形态、不放任意跨模块路径。未扩展正式契约，无独立候选。

### F3/P2 未选中替换源被预读 —— 修复
不再按目录全读；仅当 `from` 属于 `go list -deps` 选中文件集才读。ignored-nongo（非 Go notes.txt→缺失）与 excluded-platform（平台排除 platform_linux.go→缺失）由 **rc2 误拒绝 → rc0 生成**（真实 Go list/build 亦 rc0）。删除语义、未用缺失映射边界保留（未选中即不读）。

### F4/P2 回归测试链路不实 —— 修复
`analyzer_overlay_r22_test.go` 重写：
- helper 单元 `TestScopedUserOverlayMapsOnlyGoSelectedFiles`（准确命名：只证明映射筛选，未声称跑完整前端）；
- **行为回归**（真实生产入口 `Run(["generate",...])`）：`TestBehaviorCrossFieldRejected` / `TestBehaviorDependencyFieldRejected`（断言 BIND-004 且失败不写盘）/ `TestBehaviorCrossAddMethodAccepted`（生成 + 真实 `go build` 通过）/ `TestBehaviorUnusedMissingNotBlocking`。
- **两条可编译退化变异**（`overlay_r1_mutation_test.go`，`r1MutationMode` 开关，默认空）：
  - 退化① `skip-go-selection`（不给 Analyzer 用户 overlay=看磁盘）→ 跨包字段**假接受**（写坏产物，不报 BIND）——证明行为红。
  - 退化② `ignore-selection-filter`（读全部映射）→ 非 Go 未用缺失**误拒绝**——证明行为红。
  - 健康对照 `TestMutationHealthyControlDefaultPasses` 同材料绿。

## R21 保留 + 复验
归一化重复拒绝（`TestScopedUserOverlayDuplicateFailClosed`）、虚拟目录、旧产物所有权、大小写、删除、未用映射边界均保留。GOENV 持久化入口实测 cross-field **rc2 BIND-004**（一致）。缓存真命中、内容变化失效、失败保旧恢复、混合命中、Image 对照（A3-acceptance.json）。25 目标：静态页 **2 次** 定名（不变）；binding 页实测基线 70 vs 修复 65 次 go list（**不增反减**，无退化）。

## 版本
`cacheFormatVersion=8`、`generatorOutputRevision="gen-3"` **均不变**：本修复只改 Analyzer 看到的有效源，不改生成字节、不改缓存键结构（K10/K11/K12 不变）。

## 门禁
build ok / vet rc0 / gofmt 空 / **docgate ok** / cli **85.9%** · pipeline **86.3%** · analyzer **89.7%** / 台账 verify PASS。

## 五态
实现 done(`5f67df3`) / 自测 pass / 独立复核 PENDING（协调者重放同材料）/ 主仓未合 / 远端未推送。
