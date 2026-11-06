# 2026-11-06 R24-B THREE-GO-WIP-CLOSURE（ZCode）dev-log

基线 `0e4b69340b63fe328388f99828fd9ef809c016fe`；树
`C:/Users/admin/.codex/worktrees/r24-zcode-wip/Goxml`（分支 codex/r24-zcode）。
所有权：contracts/ir_schema_test.go、examples/demo/main.go、
pkg/runtime/app/testdata/concurrencyprobe/main.go 三个 Go WIP 的适用性盘点、
最小候选与验证。主库 20 项 WIP 与原件只读（input/wip-bytes 冻结原件字节采纳）。

## B1 逐 hunk 分类

| WIP | hunk | 分类 | 依据 |
|---|---|---|---|
| contracts/ir_schema_test.go | 前缀枚举表补 `CodePrefixCodegenAsset` | **有效用户增量** | 基线 ir_schema.go:69 已定义 CODEGEN-ASSET（R22-A Image 合入），但枚举测试的前缀表未覆盖它——格式/唯一性校验存在真实缺口 |
| contracts/ir_schema_test.go | 新增 TestCodegenAssetPrefixIsSeparate | **有效用户增量** | 镜像既有 TestCodegenNamePrefixIsSeparate；基线无此断言（grep 证） |
| examples/demo/main.go | Config 显式 `TargetDir: examples/demo` | **有效增量（显式化）** | 字段基线已存在（pipeline.go:36）；与 SourceDir 对齐，防默认值变化漂移；一行最小改动 |
| concurrencyprobe/main.go | 38 行：崩溃形态注释重写 + 预期文案修正 | **有效增量（诊断定位）** | D 卡 §4.3.2 六种实测崩溃形态；原硬编码单一定型（fatal map write）与 2026-10-23 复跑（index out of range ×2）不符；recover 机制差异（fatal vs 跨 goroutine panic）说明补齐 |
| —— | 其余 | **无过期/风险项** | 三份 WIP 与基线无 API 冲突（TargetDir 字段在、CODEGEN-ASSET 常量在） |

## B2 采纳与验证

三份原件**字节一致**采纳（sha256 与 wip-snapshot.json 逐一核对 ✓）：
- contracts：TestCodegenAssetPrefixIsSeparate + 枚举表扩展 → rc=0（含新测试 PASS，
  raw/r24b-contracts-test）；
- examples/demo：显式路径 build rc=0（**不启动 GUI**——demo main 无离屏模式，
  管线部分经构建+vet 验证，raw/r24b-build-three-2）；
- concurrencyprobe：显式路径 build rc=0（**预期崩溃程序不执行**——默认不运行、
  未注册为正常示例；诊断定位见 B3）。

判别力变异（WIP1）：overlay 将 ir_schema.go 的 CodePrefixCodegenAsset 改为
CODEGEN-ASSET2 → 新测试 **FAIL**（`生成层前缀 = "CODEGEN-ASSET2"，期望
CODEGEN-ASSET`，rc=1，raw/r24b-mut-red）；恢复（无 overlay）rc=0
（raw/r24b-restore）——新守卫有判别力且未削弱断言。

## B3 门禁与处置建议

- contracts test rc=0；demo/probe 显式路径 build rc=0；三文件 gofmt 空、
  vet 干净（contracts+demo+probe 显式路径）。
- 行为变异评估：WIP1 新增测试的判别力已证（上）；WIP2/WIP3 无生产行为变化
  （显式化与注释/文案），不制造无意义变异。
- **probe 诊断定位**：故意触发错误的诊断口径=「进程以退出码 2 终止（崩溃形态
  不固定）」，实测清单引用 D 卡 §4.3.2 与 2026-10-23 t7_conc.txt 原始日志；
  禁止把预期写成单一文案（本 WIP 修正的正是这一点）。
- **原件处置建议（协调者执行）**：三份主库 WIP 原件与本候选提交内容一致
  （字节采纳）→ 复核后可按本提交合入并清除对应 WIP 项；若负责人否决某份，
  候选提交可按文件拆分回退（三文件相互独立）。
- 35c9ff3 Router 预算实现保持隔离，本线未触碰 Router。
