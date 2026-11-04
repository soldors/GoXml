# R12-C --check 失败路径 E2E（workbuddy 线）

- 任务：PRODUCT-R12-C PROJECT-CHECK-FAILURE-E2E（指令随派发下达；任务包目录
  `D:/Codes/Goxml-agent-rounds/2026-11-04/product-r12/` 在本线开工后被协调者补建，
  其中只有 deepseek/workbuddy 两个子目录，无 input-manifest，本轮按派发指令执行）
- 固定基线：`b12980c1c939892cb90221a620121e9070f55ef3`（已整合 R11 三线：
  A 的 precommit 检查、B 的 codegen、C 的一键入口）
- 工作树：`C:/Users/admin/.codex/worktrees/product-r12-workbuddy/Goxml`
  分支 `codex/product-r12-workbuddy-20261104`；不 push、不合主仓
- 提交：`6c274ce`（失败路径 E2E + 取证缺陷修复）→ 本 dev-log

## 做了什么

R11 只验证过一键工程 `--check` 的**成功**路径；本轮补**失败**路径，全部真实命令取证：

| 文件 | 职责 |
|---|---|
| `scripts/test-precommit-fail-e2e.py`（新增，仅标准库） | 4 条用例 + 影子/变异仓库生成 |
| `scripts/verify-projects.py`（修 1 处真实缺陷） | 失败阶段也记录 --check 观察证据 |

五例内置示例、锁/Job 助手、CLI/Compiler/runtime/正式契约**零改动**。

### 场景构造（任务核心）

在目标包用户源 `view/edit/doc.go` 引入 `var _ = noSuchSymbolR12C`。该错误
**恰好**穿过失败路径前的所有关卡：发现（go list 不做类型检查）、包名解析
（go list 只看 package 子句）、命名预检（existingPackageSymbols 纯语法）、
GXML 分析（Analyzer 只加载 VM=model 包）——然后在阶段一 B 的提交前检查
（`go build` 目标包整目录）爆出 `undefined: noSuchSymbolR12C`。

### 用例 1 `cli-failure-e2e`（自有工程 S1→S6，实测）

| 步 | 命令 | 实测 |
|---|---|---|
| S1 | 两个 View 各自 `generate --check` | rc=0，miss，「提交前检查通过」，产物+Manifest+2 条成功缓存 |
| S2 | 再跑 `--check` | rc=0，**全命中仍执行检查**（stdout 出现检查通过标记），零重写 |
| S3 | doc.go 加类型错误 → `--check` | **rc=2**，stderr「提交前检查失败…undefined: noSuchSymbolR12C」；旧产物/Manifest/成功缓存**字节与 mtime 均未变** |
| S4 | 整目录批次 `generate view --check` | edit miss + summary **hit**，检查失败（rc=2），全或无保旧 |
| S5 | 无 --check 对照 | 生成 rc=0「已生成」；失败暴露在**后续入口构建**（go build rc=1，同一 undefined） |
| S6 | 恢复 doc.go → `--check` | rc=0，**命中**+检查通过，入口构建恢复 rc=0 |

### 用例 2-4 `entry-*`（一键入口，独占输出目录）

- **E1**（模板内置同一类型错误 + `--check`）：退 4，失败阶段=`generate:view/edit/edit.gxml`，
  `build-entry`/`test` **未启动**；产物/Manifest/成功缓存零落盘；报告
  `check={forwarded:true, passed:false}`、generate 阶段 `checkReported:false`、
  `checkFailureObserved:true`；锁按 token 释放。
- **E2**（同一坏模板、无 `--check` 对照）：generate 两阶段 rc=0，失败阶段=
  `build-entry`（后续入口构建失败），test 未启动——与 E1 的区分就是任务要的三态。
- **E3**（影子模板恢复字节 + 新目录 + `--check`）：全过，check.passed=true，
  离屏 11 用例全绿（含共享同一 ViewModel 的跨视图断言）。

### 发现并修复的取证脚本真实缺陷（E1 复现）

基线 `verify-projects.py` 把 `checkReported` 赋值放在 `run_stage` 之后——
失败时 `run_stage` 先抛 `Refused`，赋值永远走不到。于是 `--check` 失败路径上
报告丢失「原生提交前检查已执行并失败」的证据，`check_status` 只能报
`passed=None` 的「仅证明转发」，与任务要求的三态区分直接冲突。
修复：`observe_check_markers` 在成功/失败两条路径都记录
`checkReported`/`checkFailureObserved`，`check_status` 的 note 相应区分。
修后基线 25 例回归全绿（`review-check-status` 的 passed 语义不变）。

### 判别变异（两条，都可编译、实测红、可复现）

| 变异 | 改法（internal/cli/precommit_check.go） | 红 |
|---|---|---|
| `skip-check` | `if len(cands) == 0` → `if true`（检查整体跳过） | S3/S4/S5 全红：坏工程 rc=0、产物被重写、缓存被污染 |
| `ignore-build-error` | `if err == nil` → `if err == nil \|\| true`（吞检查子进程失败） | 同上 |

复现：`--make-mutated-repo <dest> --mutate <name>` 再 `--repo <dest> --case cli-failure-e2e`。

## 实测数据（台账 5 行全 PASS，verify-evidence sha `8b6feda9a923`）

| 台账 id | 命令 | 结果 |
|---|---|---|
| `r12c-e2e-all` | 4 条用例全量 | rc=0，59.859 s |
| `r12c-baseline-25` | 基线 test-verify-projects 25 例回归 | rc=0，38.250 s |
| `r12c-entry-check-success` | 真实仓一键入口 + `--check`（成功路径回归） | rc=0，11.500 s |
| `r12c-mut1-skip-check` | 变异 1（期望红） | rc=1（判据翻红），9.875 s |
| `r12c-mut2-ignore-error` | 变异 2（期望红） | rc=1，17.250 s |

外层台账行不嵌套取锁（脚本/用例自持公共锁、finally 按自有 token 释放）；
锁释放证据在各用例报告 `lockReleased=true` 与运行后锁文件不存在。

## 一个值得记录的设计事实（不是缺陷）

「全命中 + 检查失败」在 E2E 级别**不可构造**：缓存键按设计覆盖目标包源
（K3）+ VM 依赖闭包（K8）+ 构建上下文（K10），而检查编译的模块内输入恰
好就是这三样——所以任何模块内的类型错误必然先让键变化（实测 miss 原因
`package-sources-changed`；破坏 go.mod 也会经解析层变成 miss）。这正是缓存
可靠性设计的直接后果。本线的处理：
- 「全命中仍执行检查」用 S2 验证（hit + 检查标记出现），且变异 1 能让它翻红
  （检查被跳过时标记消失）；
- 「命中候选也参与检查失败」用 S4 验证（批次内 hit+miss 混合、检查失败、保旧）；
- 纯「全命中且失败」在 A 线单测（TestCheckUsesCandidateContentNotDisk 的
  命中分支）已有覆盖，E2E 不重复构造。

## 边界

- 零原生窗口：窗口入口只构建、不运行（所有用例都不执行 preferences.exe）。
- 不安装、不联网（GOPROXY=off）、不跑采样；公共锁自持 token、finally 释放。
- 保旧对照在自有测试工程完成（允许反复写同一目录）；一键入口用例一律
  独占新建输出目录，所有权/重解析点/token 指纹约束沿用基线强化版未动。
- Python 用例只在 Windows 本机实测，不宣称跨平台。

## 审 B 线（ZCode）：PENDING

截至本轮结束：`product-r12-zcode/Goxml` HEAD 仍是基线 `b12980c`、工作树
干净、无提交；`product-r12/zcode/` 交付目录不存在 → **无冻结 SHA，记 PENDING**。
「用跨包 preferences 工程验证生成/构建/共享 ViewModel 离屏行为」在基线上
已由 `r12c-entry-check-success`（11 用例全绿）覆盖，但**不等于**对 B 冻结
SHA 的独立复核——待 B 出 SHA 后由复核者以其工作树重跑该入口。
另：A 线已有 `ec1f601`（K4 输出修订进缓存身份）并交付，未冻结，与本线
改动正交（本线未动缓存键与 CLI）。
