# 2026-11-06 R24-A FONT-ACCEPTANCE-RUNNER（DeepSeek）dev-log

基线 `0e4b69340b63fe328388f99828fd9ef809c016fe`（开工核验 clean，主库 porcelain 20，索引空）。分支 `codex/r24-deepseek`，树 `r24-deepseek-font-runner`。所有权：`scripts/font/` 新编排入口及专属测试、`字体正式验收执行方案-R23R1B.md` 必要同步、本 dev-log。产品源码（theme/app）与冻结 `font_lazy_strict_test.go` **未动**。
记录提交：`0e4b693`（base）→ `c5776f8`（A1/A2 入口+合成测试+方案同步）→ 本 dev-log。

## A1 盘点与实现
- 读现行执行方案 §8、上轮最终复核（repair-review/final-review.md）与 `font-plan-template.ps1`、`plan-fixture-verified.json`、mock。
- 复用现有 `scripts/font/collect-m1.ps1`（退出码：0 ok / 5 无效样本 / 2 usage / 3 构建失败 / 4 锁超时 / 6 源变 / 7 子进程超界 / 8 证据不可持久化）与 `summarize-m1.ps1`（`-AllowMixed` 分组、伪造记录拒绝、零值不丢弃）；**不新建第二套收集器**。
- 新增 `scripts/font/run-font-acceptance.ps1`（随仓入口）：预登记交替序列（`-Order`/`-OrderFile`）冻结到 `order.json`；每样本独立进程 `-Runs 1`；全部共用**公共锁** `-LockFile`；原件不改；派生 `samples.jsonl` 每条附 `aggregationSource{path,sha256,originalId}`、派生 id 加运行目录命名空间；显式 `-AllowMixed` 派生汇总。
- **fail-closed**：证据目录已存在拒绝；样本 `samples.jsonl` 必须恰 1 行；原 id 缺失拒绝；collector `rc∉{0,5}` 立即停止调度（保留已录证据）；`rc=5` 无效样本保留后继续。
- **准备态**（不带 `-Execute`）：仅冻结序列 + 校验 + dry-run argv，写 `order.json`/`prepare-plan.json`，**退出码 2（准备态≠通过）**，不调用采集器、不产样本。
- 编码：两个 `.ps1` 全 ASCII（避免 Windows PowerShell 5.1 ANSI 误读中文注释导致解析错误——本线踩坑并修复）。
- 参数名坑：`[string]$Order` 参数与局部 `$order` 在 PowerShell 大小写不敏感下同名冲突；已将局部改为 `$orderSeq`。

## A2 mock 合成回归（`scripts/font/test-font-acceptance-plan.ps1`，仅临时 mock，不采样）
临时 mock collector/summarizer + 合成样本，覆盖 20 项断言：
- 交替 6 样本全 0 → rc0；`order.json` 按预登记冻结；派生 6 条；manifest 6 条；派生 id 唯一（原 id 均 0001 被目录命名空间区分）；记录附原 path/SHA/originalId。
- 源 `samples.jsonl` 哈希 = manifest.sha256（原件未改）。
- 无效样本（rc=5）保留后继续（整体 rc0，仍产派生）。
- 故障码 rc=7 立即停止：预登记 6 样本只跑到 index1（2 个样本目录）后停。
- 缺 `samples.jsonl` fail-closed；已存在证据目录拒绝；`none` 模式拒绝；准备态 rc=2 且不产样本目录、写 `prepare-plan.json`。
- **两条行为变异**（克隆入口脚本注入缺陷）：变异A「故障后继续」→ 执行 >2 样本（目标断言失败）；变异B「不命名空间（直接拼接重复原 ID）」→ 派生 id 重复（唯一性断言失败）；健康脚本同材料 PASS。

## A3 新旧字体脚本合成回归 + 人工观察/决策输入
- 旧 `test-comparison.ps1`（解析/统计，合成 fixture）**未改动**，仍 PASS（52 checks）；新 `test-font-acceptance-plan.ps1` PASS（20 checks）。二者独立，`scripts/font` 现有 collector/summarizer 只读。
- 真实执行 argv 模板（准备态已产出 dry-run argv，示例见 raw/prep-check 与 prepare-rec）；预登记样本规模建议 N≥30（决策输入，由负责人定）；统计口径沿用 `font-init-v1`（median / nearest-rank p95，跨组不合并）。
- **人工观察表**（真实采样授权后填写，非本轮）：窗口正常出现并渲染、首屏 CJK 字形、无花屏/空字、标题栏可关闭、退出码 0；视觉判定单列，不与耗时样本混。
- **等待裁决项**：正式性能阈值（M1 与 sync-vs-lazy）、真实采样授权、跨平台、第二成功 family、IMPL-3 冻结下游断言解冻。本轮**不设阈值、不采样、不开窗、不安装**。

## 门禁
两个 `.ps1` 全 ASCII；`scripts/font` 现有脚本未改；产品 Go 源码未动（theme/app test 不受影响，本线未运行真实 Go 采样）；台账 `commands.jsonl` 3 条（合成回归/旧回归/准备态）verify PASS；`docgate` 见提交后复验。

## 未验证边界
真实字体性能采样未执行（未授权）；阈值未定；跨 platform/第二 family 未做；本线只交付"随仓入口 + 合成回归 + 方案同步"，不宣布正式字体验收通过。

## 五态
实现 done(`c5776f8`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
