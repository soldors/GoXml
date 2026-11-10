# 2026-11-09 · R28-C LIFECYCLE-COMBINATION（HY4 独立复核线）

日期：2026-11-09/10（Asia/Shanghai）。只做 Lifecycle / Router / App，不切 Hydration、
DataGrid，不复用其他 agent 的旧上下文。

| 项 | 值 |
|---|---|
| 主库基线 | `255c39251127c39d1a684baa881767a5fb7677b9`（主库 HEAD 实测、status 空、全程只读） |
| 作者冻结参考 | `363c9e728048fb4145b25ef9850d1500c3d20034`（实测为基线后代） |
| 分支 | `codex/r28-hy4-lifecycle-combination` |
| 全新独立树 | `C:/Users/admin/.codex/worktrees/r28-hy4-lifecycle-combination/Goxml` |
| 交付 | `D:/Codes/Goxml-agent-rounds/2026-11-09/product-r28/hy4/delivery/` |

## 本线真实提交（docgate 登记）

- `4b2196f576ccf52cf40b8fa81e7474149503a52d`（`4b2196f`）
  `feat(lifecycle): R28-C 组合候选在 HY4 全新独立树按字节重建`
  —— 7 个文件：`pkg/runtime/lifecycle/{lifecycle.go,lifecycle_test.go}`、
  `pkg/runtime/router/{lifecycle.go,lifecycle_bench_test.go,lifecycle_composition_test.go,lifecycle_race_test.go,router.go}`。
- `c6d4ae23cf4235b2a3cd9307753344b525271c4f`（`c6d4ae2`）
  `test(lifecycle): HY4 独立复核守卫（真实 DestroyEvent 组合与最小接线）`
  —— `pkg/runtime/app/hy4_combination_test.go`、
  `pkg/runtime/router/hy4_combination_review_test.go`。

**是否使用作者提交**：未 cherry-pick、未切换/检出作者分支、未在作者树修改。
`363c9e7` 仅作**只读复核材料**，用 `git show <commit>:<path>` 按字节重建到全新树，
本线两个提交均为独立新提交；未继承作者 dev-log。

**链差异实证（必须保留，不复用旧链）**：用户列出的
`91a223b / 00c5270 / 5cbe465 / 3a4efb3 / 43cab58` 均可解析为 commit，
但 `merge-base --is-ancestor <each> 363c9e7` 全部为否；与基线 `255c3925`
的 merge-base 均为 `a2dcff949602337475ad3daabc090c6400ded831`。
该链 `3a4efb3 feat(app): expose controlled destroy callback wrapper` 重加了
基线 `255c3925` 已存在的 `WithOnDestroy`/`notifyViewDestroyed`，语义与
R27 发布基线不等价。按「禁止复用其他 agent 的旧上下文」只登记、不复用。
作者真实冻结链为 `51bdc08 → 1d3d68a → 961c46d → 363c9e7`。

## 独立复核内容

- **真实 DestroyEvent 接线 vs 受控事件源边界分开记录**：生产
  `app.loopEvents` 的 `case app.DestroyEvent → notifyViewDestroyed(a.view)`
  接线真实存在（基线 R27 已发布，本线零改动）；HY4 守卫用内存 channel
  合成 `gioapp.DestroyEvent{}` 直接喂给 `loopEvents(next)`，不经过
  `a.w.Event` / Win32 driver / 原生窗口。销毁回调 → `Router.LifecycleFinalize`
  → `Owner.Finalize` 清理恰好一次，重复销毁幂等。
- `app.WithOnDestroy` / `NotifyOwnerDestroyed` 审核：默认未 opt-in 路径
  逐字不变；`Run` / `(*App).Run` / `RunWith` / `RunWindow` /
  `RunWindowWithInvalidator` 签名全部未改；`pkg/runtime/app/app.go` 与基线
  逐字节一致（SHA-256 `edd58fdf2cfee61766c2c36dea1a0902b06cfa727bddfa5ed5615e6daa7b7524`）。
  `integration-proposal.diff` 的 `NotifyOwnerDestroyed` **未应用，仍为 proposal**；
  已做 `git apply --check` rc=0 与外部 overlay 编译 rc=0。
- 生命周期顺序：成功发布后首次业务 Layout 前才 Mount；旧页先取消再一次
  锁外 OnUnmount；失败工厂保旧且不 Mount；合并导航只构造最终目标；
  `Shutdown` 只 `CancelOnly` 不执行 UI OnUnmount；两个 Router/App 实例隔离；
  不反射 VM（生命周期相关 6 个源码文件零 `reflect` 引用，且有行为守卫）。

## 门禁与变异（原始双通道在 `delivery/raw/cmdlogs/<id>/c1/`）

全部正式命令经公共锁 `D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock`
+ 随仓 `scripts/record-command.py`，单层 `acquire`、仅释放自有 token。

- 三包与联合回归：**210 PASS / 0 FAIL / 1 既有 SKIP**；HY4 定向守卫
  app 4 PASS + router 6 PASS（零空跑）。
- **MinGW race 真实完成**：`gcc.exe (MinGW-W64 x86_64-ucrt-posix-seh) 16.1.0`、
  `CGO_ENABLED=1`、`GORACE=halt_on_error=1`；三条 race 命令 rc=0、
  `0 DATA RACE`（全量三包 / HY4 守卫 / 作者并发族）。不是「环境不可用」。
- CGO=0 `go build ./...`、vet、`gofmt -l` 空、diagdoc、docgate 全 rc=0。
- 覆盖率（同源码分包 profile，不混分母）：lifecycle **92.7%**、
  router **94.5%**、app **92.0%**，均 ≥85%。
- **三条外部 Go overlay 行为变异**（外部副本，工作树全程不改）：
  - `M1-no-destroy-event-wiring`：删除 DestroyEvent 接线 → 编译 rc=0 →
    `TestHY4DestroyEventWiringFinalizesOwnerExactlyOnce` rc=1 → 移除 overlay rc=0。
  - `M2-mount-at-factory-construction`：Mount/接缝提前到工厂构造 → 编译 rc=0 →
    `TestHY4LifecycleOrderRequiresCommitBeforeMount` rc=1 → 移除 overlay rc=0。
  - `M3-error-path-runs-ui-callback`：错误路径误执行 UI OnUnmount → 编译 rc=0 →
    `TestHY4FailedFactoryKeepsOldPageAndNoUIUnmount` rc=1 → 移除 overlay rc=0。
  - 恢复判据为生产文件**字节 SHA-256**（不是 `git diff`）：
    `app.go edd58fdf…`、`router.go 6fa7879e…`、`lifecycle.go 5135991…`
    变异前后一致 `byteIdentical=true`；首败与全部原件保留。

## 五态与未证边界

- 实现：完成（独立树重建 + 独立守卫，两个精确本地提交）。
- 自测：**验收结论：通过**（限无窗口受控集合）；**复核状态：待独立复核**。
- 独立复核：本线即复核线；对作者 `363c9e7` 的材料与三条关键接线已重放。
  协调者对 HY4 冻结 SHA 的再复核仍**未验证**。
- 主库：未合入；主库 HEAD 仍 `255c3925`、status 空。
- 远端：未 push；本轮未执行 `git ls-remote`，远端状态**无法核对**。

**验收结论：未验证** 单列：原生窗口 / 来自原生窗口的真实 DestroyEvent /
真实 native race / 跨平台运行 / 多窗口 / DPI / IME / 人工视觉验收 /
整应用性能采样。HY4 的「真实 DestroyEvent 接线」仅指生产代码分支被受控
合成事件真实执行，**不等于**原生窗口销毁事件已被验证。
新 API、`EnableLifecycle` opt-in、切换先后策略、`LifecyclePage` 包装、
`NotifyOwnerDestroyed` 是否应用均为**隔离候选 / 待裁决**，技术通过不自动
代表新公共语义获批。冻结不等于批准，不合并、不推送。
