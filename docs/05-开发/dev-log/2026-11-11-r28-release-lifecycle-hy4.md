# 2026-11-11 · R28-C 后续：Lifecycle 原生关窗准备 / 静态资产缺口核查（HY4）

遵守 `D:/Codes/Goxml-agent-rounds/2026-11-11/product-r28-release-followup/README.md`。
范围仅 Lifecycle / App testdata / TASK-LIFE-001 补记；不改主库、不 push、
不改 Router 生产、公共 App API、Image/CLI/Compiler 生产、DataGrid/Hydration；
不安装依赖、不执行原生窗口程序。

| 项 | 值 |
|---|---|
| 组合基线 / 本线基点 | `611948d49525744cbb4127265983fd24236e6bf5`（`codex/r28-combined-acceptance` HEAD；父 `b3e502c`） |
| 分支 / 独立树 | `codex/r28-release-lifecycle` / `C:/Users/admin/.codex/worktrees/r28-release-lifecycle/Goxml` |
| 交付 | `D:/Codes/Goxml-agent-rounds/2026-11-11/product-r28-release-followup/delivery-c/` |
| 主库 | `255c39251127c39d1a684baa881767a5fb7677b9`，status 空，全程只读 |

## 两个切片独立提交（可分别验收）

- **切片 1**：`4d24c7fefcd034de1552b7c1f608be35b019f534`（`4d24c7f`）
  `test(app): R28-C 原生关窗 fixture（仅编译）+ CancelOnly 注释据实修正`
  —— 新增 `pkg/runtime/app/testdata/lifecyclenative/{main.go,README.md}`；
  只改 `pkg/runtime/lifecycle/lifecycle.go` 的 `CancelOnly` 注释（函数体一字未改）。
  父提交即组合基线 `611948d`。
- **切片 2**：`7aba43ce90ca34d02e8fcec25f3522fabdaab6b1`（`7aba43c`）
  `docs(TASK-LIFE-001): 据实修正 TASK-LIFE-001-3 文案并单列合同外裁决`
  —— 只改 `docs/04-WBS/任务卡/TASK-LIFE-001.md`（§2.2 逐条对账 / §2.3 裁决项）。
  父提交 `4d24c7f`。
- 本提交（dev-log 维护提交，按 docgate 自引用规则排除 HEAD）。

## 切片 1：真实销毁事件 fixture（只编译，未执行）

- `lifecyclenative` 在 `testdata/` 下且带 `//go:build nativewindow`：
  `go list ./...` 实测不含该包，默认 `go test/go build ./...` 不选中。
  本轮只编译普通与 race 二进制，**没有执行任何一个**。
- 只用公开 `app.WithOnDestroy` + `app.RunSingleWindowProcess` +
  `ProcessOptions{Observer}` + `ExitResult` 组合真实 `router.Router`
  （`EnableLifecycle` + `LifecyclePage`）。**不**手工触发回调、**不**合成
  `gioapp.DestroyEvent`、**不**加 `NotifyOwnerDestroyed`、**不**新增公开 API。
  真实关闭路径来自生产 `next = a.w.Event`；公开 `ProcessOptions` 无事件/错误/
  退出注入字段。
- fixture 的证据字段分别记录：首次业务 Layout 前 Mount、真实关闭路径、
  卸载前页面 Context 已取消（`context.Canceled`）、`OnUnmount` 恰一次、
  cleanup 后 observer、`ExitResult.Code`/进程 rc，以及必须由授权执行者填写的
  `no_residual_process` / `natural_titlebar_close_steps_performed` /
  `native_race_result` / `cross_platform_run_result`（`runner_fields_required`）。
  `natural` 与 `shutdown-first` 两场景区分后台 `Router.Shutdown()` 只
  `CancelOnly` 与 UI owner `LifecycleFinalize` 的一次清理。
  自然标题栏关闭步骤（8 步）在 testdata README，本轮**未执行**。
- 保留现有受控事件回归与两条生产循环守卫（`loopEvents` / `runWindowEvents`）：
  `c1-existing-loop-guards` 12 PASS / 0 FAIL；`lifecycle` 包全量 23 PASS；
  `TestCloseoutCancelOnlyContextOwnershipBoundary` 3 PASS。
- 二进制身份（`raw/fixture/binary-identity.json`）：
  普通 `CGO_ENABLED=0`、`-tags=nativewindow`、12,014,592 B、
  SHA-256 `4bb0cdc176395ff5bd05c05a02f9404cf8bad8ac6aa2e941aad39b8ed8ac58bd`；
  race `CGO_ENABLED=1` + MinGW、`-race=true`、19,495,198 B、
  SHA-256 `f4252866daaddd1f943b7597728fff854b646df4e960006f5da3eae335a307ad`。
- 跨平台编译性：linux/amd64 与 darwin/amd64 在 `CGO_ENABLED=0` 下均 rc=1
  （Gio 需 CGO=1 与系统库，见 AGENT.md / 平台依赖矩阵）。首两次按 rc=0 期望的
  尝试如实留痕，另以 `c1-platform-negative-*` 定义为**预期红**负例探针。
  **交叉编译只证明编译性，不证明运行**；本轮未做任何平台运行。
- `Owner.CancelOnly` 注释据实修正：函数只**直接**取消 owner 根 Context，
  不承诺取消独立 `parent` 的页面 Context；页面取消由 `Router.Shutdown` 或
  `Finalize` 的 `unmountCurrent()` 负责。只改注释，函数体 SHA-256 与基线一致，
  行为不变；与 TASK-LIFE-001 §2.1 及既有边界探针一致。

## 切片 2：TASK-LIFE-001-3 独立对账

- 结论：卡片 `Todo` / 「静态资源管线待完成」措辞**不精确**。子任务三段
  （静态资源发现、嵌入声明、生成代码引用）在 **ADR-0015（已批准）/ Image v1
  （已发布）合同内**已实现并有回归；据实修正文案，**不宣布整卡完成**。
- 逐条映射（卡片 §2.2 + 交付 `image-mapping.md`）：ADR-0015 §2.1–2.5、
  TASK-IMAGE-001-1…7、routed-gallery、既有 codegen/pipeline/CLI/diagdoc
  回归 → 三段；本轮真实复跑 codegen 窄测 5 PASS、pipeline 13 PASS、
  CLI 6 PASS、diagdoc 24 PASS，全 0 FAIL。
- 原证据分散，本线补**一条现合同内连接回归**
  （`delivery-c/raw/image/verify_life_asset_link.py`，只用公开 `goxml generate`
  与随仓模板，不改生产）：同一真实工程上 复制 → 构建 CLI → 一次目录生成两页
  （`--check`）→ 断言 `//go:embed` / `embed.FS` / `goxmlAssets…` /
  `ReadFile`+`image.Decode` / `img0 paint.ImageOp` / `widget.Image{Src: v.img0…}`
  → `CGO_ENABLED=0 go build ./...` rc=0 → 损坏资产 rc=2 +
  `CODEGEN-ASSET-002` + 「批次全部放弃」+ 两页产物与
  `generate-manifest.json` / `.goxml/cache/*.json` 字节与 mtime 全部保旧 →
  还原后重新生成 rc=0。**18/18 判据通过**。
- **首败保留**：首轮把被故意改坏的**输入资产**误算进「产物保旧」快照，
  1 条判据失败（对账脚本判据范围错误，非产品缺陷）；收窄到产物/清单后
  `-r2` 转绿，首轮报告与台账一行不改。
- 通用资产 API / 额外格式与资源类型 / 跨包资产复制与 manifest 语义 /
  绑定 `Src`/`Fit` 明确为 ADR-0015 §4 **现合同之外**，卡片 §2.3 单列负责人裁决；
  裁决前不得改生产编译器、CLI 缓存、语言规范或输出契约。本线没有这么做，
  也没有复制资产管线。

## 门禁与证据

全部正式命令经公共锁 `D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock`
+ 随仓 `scripts/record-command.py`，单层 `acquire`、仅释放自有 token；
原始 stdout/stderr 在 `raw/cmdlogs/<id>/c1/`，台账 `commands.jsonl`。
窄测、相关 race/build/vet/gofmt 与 docgate/diagdoc 详见 `raw/gates/` 与交付
`handoff.md`。`verify-evidence.py` 的 PASS 只代表**记录完整性**，不等于业务
全绿或语义批准；台账中预期红（overlay 变异 / 平台负例）与首败均如实保留。

## 五态与未验证边界

| 状态 | 结论 |
|---|---|
| 实现 | 完成（切片 1 编译专用 fixture + 注释修正；切片 2 对账与文案修正；生产代码零改动） |
| 自测 | **验收结论：通过**（限无窗口受控集合 / 生成期与构建期）；**复核状态：待独立复核** |
| 独立复核 | 协调者对本线两个切片 SHA 的再复核**验收结论：未验证；复核状态：待独立复核** |
| 主库 | 未合入；HEAD 仍 `255c3925`、status 空 |
| 远端 | 未 push；本轮未执行 `ls-remote`，远端状态**无法核对** |

**验收结论：未验证 / 复核状态：未要求（待授权后转待独立复核）** 单列：
来自原生窗口的真实 `DestroyEvent`、真实窗口显示/原生退出、真实 native race、
进程与窗口句柄收口（`no_residual_process`）、跨平台运行（Linux/macOS 需
`CGO_ENABLED=1`）、IME/DPI/多窗口、人工视觉验收、整应用性能采样。
编译原生 fixture **不等于**执行；`runner_fields_required` 中只能由授权执行者
填写的字段本轮**全部未填**。真实开窗与真实 native race 仍须另行验收。

冻结后停止，不接下一线；由协调者独立复核、组合验收并决定发布。
