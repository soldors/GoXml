# 2026-11-06 R24-A FONT-ACCEPTANCE-RUNNER 集中返修 R1（DeepSeek）dev-log

父冻结 `43caac7cf074f0a9828b1c8b01e8a6db57fb0867`（协调者 BLOCK）；主库现 `bd874d7119dbf26dd91e9a5f24accf8d1de9a15a`（B/C 已发布，9 WIP，**未回滚**）。分支 `codex/r24-deepseek-repair-r1`，树 `r24-deepseek-repair1`。只关闭 F-A1~A4；旧报告/raw 保留。
记录提交：`43caac7`（父）→ `a74b33b`（F-A1~A4 修复+真实 summarizer 测试）→ 本 dev-log。

## F-A1：汇总器 rc0 被当成全部记录可用
真实 `summarize-m1.ps1` 在解析完成、记录被拒收时**仍 exit 0**；原入口只看 `$LASTEXITCODE`。
**修复**：入口在派生后读 `summary/summary.json`，fail-closed：`rejected==0` 且 `accepted==derived` 且
`invalid==expected(rc=5)` 且 `valid==derived-invalid`（`accepted=valid+invalid`，invalid 是已标记子集、非拒收）；
不把合法 invalid 与 forged 混同；不设阈值。
**实测**（协调者同 fixture+真实 summarizer）：
- 健康 `sync,lazy`：summary `accepted2/rejected0/valid2/groupCount2`，入口 **rc0**。
- forged-v2（`observed.fontReadyElapsedNs=999` 仍 valid=true）：summary `accepted0/rejected2/invalid0`，入口 **rc1 fail-closed**（原 rc0）。
证据：raw/coord-healthy/、raw/coord-forged/。

## F-A2：未执行交替/平衡约束
原只检查每项 ∈{sync,lazy}，`sync,sync` 亦接受。
**修复**：要求**偶数、sync==lazy 数量、严格逐项交替（相邻不同）**；奇数/不平衡/相邻重复拒绝并给明确消息（奇数策略=拒绝，非静默平衡）。
**实测**：`sync,sync` → 入口 **rc1**（`order not balanced: sync=2 lazy=0`）。证据 raw/coord-nonaltern/。

## F-A3：计划与真实来源无绑定
原先只拼 `caseName/id`，未核 `record.load/baseSHA`；`ExpectedHead` 只校验长度>=7 却称 full SHA。
**修复**：`ExpectedHead` 须匹配 `^[0-9a-f]{40}$`；每条记录 `load == 预登记 mode` 且 `baseSHA == ExpectedHead`，不符在**派生前**拒绝且停止调度、原件保留。
**prepare vs execute 范围**：prepare 只校验 argv/序列/SHA 格式/目录唯一（不声称来源或汇总有效）；execute 才绑定来源并核 summary。
**实测**：wrong-mode（第二次 lazy 却 `load=sync`）→ 入口 **rc1**（`record load='sync' != preregistered mode 'lazy'`）。证据 raw/coord-wrongmode/。

## F-A4：测试链路不实
**修复** `test-font-acceptance-plan.ps1`：改用**真实 `summarize-m1.ps1`**（非 mock）+ mock **collector** 产**真实结构**记录（含 `observed.*`、合法零 `m1ElapsedNs=0`、合法 invalid）。
覆盖（同一健康合同守卫）：健康交替 rc0（summary accepted2/rejected0/groups2）、合法 invalid 保留（rc0，summary invalid=1，非 rejected）、forged(真实 summarizer 拒收→入口 rc1)、非交替(rc1)、wrong-mode(rc1)、bad-base(rc1)、missing-id(rc1)、rows!=1(rc1)、bad-head(rc1)、旧目录(rc1)、prepare(rc2 无样本)。
**两条行为变异**（克隆入口脚本）跑**同一健康守卫**：`continue-after-fatal` → 执行 >2 样本（真实 FAIL）；`no-namespace duplicate id` → 派生 id 重复（真实 FAIL）；健康脚本 PASS。**不**以语法/构建失败充红；有界、脚本可执行。
**结果**：test PASS **20 checks**；旧 `test-comparison.ps1`（未改）仍 **52 checks PASS**。

## 环境说明
- 顶层用 PowerShell 7.6.9；入口内部按真实生产调用 `powershell.exe`（5.1）。PS5.1 `Get-FileHash` 本次**实测可用**（协调者早前两次 auto-load 失败属瞬时/模块加载，非策略问题，未改策略或系统）。
- 两 `.ps1` 全 ASCII（0 非 ASCII 字节）。私有 mock 仅测试临时目录；无生产测试退化开关。

## 门禁
仅 `scripts/font/{run-font-acceptance,test-font-acceptance-plan}.ps1` 改动；产品 Go 源码与 `font_lazy_strict_test.go` 未改；`scripts/font` 现有 collector/summarizer 未改；台账 verify PASS；docgate 见提交后复验。无窗口/无采样/无安装/无阈值。

## 未验证边界
真实字体性能采样未执行（未授权）；阈值未定；跨平台/第二 family 未做。本次仅关闭 F-A1~A4 工具与测试缺陷，**不宣布正式字体验收通过**。

## 五态
实现 done(`a74b33b`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
