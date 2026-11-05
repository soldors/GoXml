# 2026-11-05 R22-A 第二次窄返修 R2（DeepSeek）dev-log

父冻结 `5d0a487d8e88908d28d643a896548fd13efe3881`（R1，协调者 BLOCK）；主库基线 `39f45ea07fdbd1c2aaab4fff24a1c452e4773a11`。
分支 `codex/r22-deepseek-repair-r2`，树 `r22-deepseek-repair-r2`。旧冻结/协调者证据只读。
提交链：`39f45ea` → `d753d7a` → `a1e3d91` → `5f67df3` → `5d0a487` → 本线 `283ec97`。

## F5/P1 混合替换+删除丢失删除语义 —— 修复
根因：`scopedUserOverlayForAnalyzer` 只快照非删除选中源；一旦返回**非空** `Config.Overlay`，go/packages 的 `-overlay` 覆盖 GOFLAGS 用户 overlay，而快照又不表达删除项，使被删文件重新从磁盘进入加载。
修复：删除项（`to==""`）——若其 `from` 是**已加载包目录内、磁盘存在的非测试 Go 源**，用 `//go:build ignore\n\npackage ignore\n` 占位字节表达删除（与工具自有旧产物 ignore 同机制，`Config.Overlay` 无法表达"不存在"，用 ignore 标签等效排除）。替换项仅当 Go 选中才读；新增虚拟源 Go 选中则读入。工具自有条目优先、ModuleRoot 范围、未用源不预读全部保留。
- **F5-1 mixed-delete-method**（vm.go 同字节替换 + 删除 actions.go，Click=Inc）：基线 EVENT-003，R1 假接受 rc0；**修复 rc2 GXML-EVENT-003，零产物/Manifest**。
- **F5-2 mixed-delete-conflict**（删 actions.go + 加 new_actions.go 同方法）：真实 Go build rc0，R1 误报 ROOT-002 重复；**修复 rc0 生成 + 同环境真实构建 rc0**。
（raw/repair-r2-raw/probes/mixed-results.json）

## F6/P2 测试退化进入生产 —— 修复
移除 `generated_overlay.go` 中 `r1MutationMode` 包级可变全局、两条退化分支与只服务变异的 `legacyDirScopeRead`。改为 `overlay_r1_mutation_test.go` 用 **Go `-overlay` 作用于独立源码副本**：
- 复制 `generated_overlay.go` 到临时文件并文本变异（① 不喂 overlay→看磁盘；② 不按选中过滤→读全部）；
- 用 `go test -overlay=<json>` 把副本映射回去，跑**健康行为回归**（`TestBehaviorCrossFieldRejected` / `TestBehaviorUnusedMissingNotBlocking`）→ 断言目标测试实际 **FAIL**（rc!=0 + `--- FAIL`）；
- 恢复（不加 overlay）同一测试 **PASS**。真实生产入口回归保留。

## 查询成本（真实 overlay，纠正范围）raw/repair-r2-raw/A3-querycount-realoverlay.json
25 个跨包 VM 目标 + **真实用户 overlay**（替换各自 model/vm.go）：
| | total | naming | deps |
|---|---|---|---|
| true baseline 39f45ea | 67 | 42 | 25 |
| R1 5d0a487 | 96 | 46 | 50 |
| R2 283ec97 | 89 | 39 | 50 |
- **deps 分解**：25 = go/packages 内部 `-deps=true`（预存）；+25 = 本修复**每 distinct VM import path 一次** `go list -deps` 选源（有界、按 import path 去重，最小）。
- **R2 vs R1**：deps 不变（50）、naming −7、total −7（不增反减）。**R2 vs truebaseline**：+25 deps（即上述选源查询，必要），naming 更少。
- **可避免重复：无**（25 distinct 各一次为最小，已去重）；未为降计数绕过一致性。

## 重放与保留
协调者原六场景全绿（同包字段/跨包字段/依赖字段 rc2 BIND-004 不写盘；跨包新增方法 rc0+构建；ignored-nongo rc0；deleted-method rc2 EVENT-003）。excluded-platform rc0 生成。GOENV 入口一致；缓存真命中/失败保旧恢复/混批/Image 见既有与 R2 复跑。

## 版本
`cacheFormatVersion=8`、`generatorOutputRevision="gen-3"` **均不变**：只改 Analyzer 有效前端视图，不改生成字节、不改缓存键结构。

## 门禁
build ok / vet rc0 / gofmt 空 / docgate ok / cli·pipeline·analyzer 测试全绿（含 F6 嵌套 mutation）/ 台账 verify PASS。

## 五态
实现 done(`283ec97`) / 自测 pass / 独立复核 PENDING（协调者重放 mixed 两场景 + 原六 + 查询分解）/ 主仓未合 / 远端未推送。
