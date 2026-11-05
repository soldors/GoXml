# 2026-11-05 R22-C GALLERY-CONTINUOUS-ACCEPTANCE（WorkBuddy）dev-log

基线 `8dbfe9be5be144be928e7e39ec629d2102dff9da`（B 线接纳版；Router 生产代码
`5306c8c3..8dbfe9be` **零改动**，只多了 `pkg/runtime/router/r22_scheduler_test.go`）。
树 `C:/Users/admin/.codex/worktrees/r22-workbuddy-gallery/Goxml`；分支
`codex/r22-workbuddy-gallery-acceptance`。主库与用户 WIP 只读，未推送、未合并。

四个业务提交（按切片，逐文件 `git add --`，`GOXML_AUTO_PUSH=0`，临时空 hooksPath）；
本文件自身所在的 dev-log 提交不在表内（它只记别人）：

| 切片 | 提交 | 内容 |
|---|---|---|
| C1 / C2 入口 | `56772ff3b54a83270f6fca5f4fe0bb682ba80cdc` | `scripts/verify-gallery-accept.py`（主验收判据 + 缓存/资产/变异矩阵） |
| C3 反例 | `f5ddcb799d2961ab3f662f88e1d7e0a48130cf62` | `scripts/test-verify-gallery-accept.py`（14 条不跑 Go 的秒级反例） |
| C3 文档 | `052f7c7ca61d9c00b46cbd6ea0119f2022f673bd` | 图库教程：一条命令、C8 上下文对照、关窗两层证据、删交接包工具依赖 |
| C3 数字修正 | `be1e57f67277993ac0f8c6500c78ab6eafb4e6bb` | README 不再写死判据条数（74→76 是过程事实，写死就会过期） |

C1 与 C2 落在**同一个文件**里，所以合成一个提交：把同一个脚本拆成两个「半成品」
提交会产生不可运行的中间态，而本线的证据只认「能跑出结论的那一版」。

## C1 从交接包变成随仓入口

迁进来的是**判据**，不是业务实现：

- 主验收仍然委派 `scripts/verify-projects.py routed-gallery`（复制 → 一次目录
  `generate view --out-dir view --check` → 只构建窗口入口 → 离屏测试）；本脚本不复制
  复制器、不重建 CLI、不自己拼 `go test` 参数。
- 期望用例清单**不复制**：直接从 `verify-projects.PROJECTS['routed-gallery']` 取
  `expectedTests` / `completionMarker`，所以「数量以新基线实际用例集为准」是机械成立的。
- 进程收口复用 `verify_examples_proc.run_capture`，锁复用 `verify-examples-lock.py`；
  Job Object / token / 台账一行都没重写。
- `make-assets.py` **没有迁**：要一张「另一张合法 PNG」时直接用详情页那张版本化字节
  （1444 B，32x16）。零生成器、零新二进制，且仍是版本化材料。

持锁归属（一段一层，三段串行，无嵌套）：

| 段 | 持锁者 | 锁忙时 |
|---|---|---|
| 预检 | 本进程取后立刻按自有 token 放 | rc=8，**连运行目录都不建** |
| 主验收 | `verify-projects.py` 子进程 | 子进程 rc=8 → 外层 8，零 Go 进程 |
| 矩阵 | 本进程取、本进程按 token 放 | rc=8，矩阵零 Go 进程 |

每次运行独占 `<run-dir>`（必须不存在），报告只写 `<run-dir>/report.json`
（`--json` 走 `O_EXCL`）；四样落点：`project/`、`main.json`、`raw/`、`report.json`。

## C2 主验收实测

运行目录 `D:/Codes/Goxml-r22c-work/r22 图库 终验4 （空格）`（空格 + 全角括号），
`GALLERY-ACCEPT-OK ga-20261105-164315-594e67`，**76 条判据全部成立，16 条命令，40.345 s**。

主验收段（委派 verify-projects，锁由子进程持）：

- `generate` 阶段**只有一次**，`argv[1:] == ['generate','view','--out-dir','view','--check']`，
  cwd=工程根；`checkTargets == 2`（一批两页是真的），`--check` 的「提交前检查通过」标记
  被观察到（不只是参数转发）。
- 台账**独立重数**（不信任上层摘要）：`=== RUN` 10 个、`--- PASS:` 10 个、
  `ROUTE-DONE` 10 个，三个集合都等于必要用例集，`skip=0 fail=0 noTestFiles=0`。
- 窗口入口只构建：`build-entry` rc=0，且**从未**有任何阶段把 `gallery.exe` 当 `argv[0]`。

矩阵（16 条命令全部 `contained=True`、`rcSource=observed`）：

| 判据 | 实测 |
|---|---|
| 真命中（第二次 / 第三次） | `命中 2，生成 0`、零「缓存未命中」、两页都报命中；两页产物 + Manifest 的**字节与 mtime 全未动** |
| 合法换图 | 只有首页未命中；首页字节不变、mtime 变；详情页产物字节与 mtime **完全没动** |
| 换回原字节 | 回到原键 `命中 2，生成 0`，且这一次**没有重写任何文件** |
| 坏图 | rc=2；`home.gxml:13:7: CODEGEN-ASSET-002: asset "assets/badge.png": image: unknown format`；`批次全部放弃，磁盘无任何变更`；三件套保旧 |
| 删图 | rc=2；`CODEGEN-ASSET-002`（底层 `GetFileAttributesEx`）；三件套仍保旧 |
| 恢复 | rc=0 且**仍是 `命中 2，生成 0`**——两次失败批次没有删掉成功缓存记录 |

### 资产变化 vs 上下文变化（本轮新增的分离判据）

| 变化的是 | 未命中的范围 | 原因词 |
|---|---|---|
| 资产内容（C4） | 只有引用它的那一页 | `package-sources-changed` |
| 构建上下文（C8，去掉 `GOFLAGS`） | 同一批**所有**页 | `package-sources-changed` |

原因词一样，只有范围能区分，所以矩阵把「几页未命中」单独记成判据；C8 还额外断言
`go.mod`/`go.sum` 没被改写、回到固定环境又变回 `命中 2，生成 0`。

### 一个自我纠错（留证据，不擦）

第一轮 `ga-20261105-163813-f85a64` 在 `s7.back-frozen` 上红了：我把「回到固定环境
不该重写」的比较基线取在**上下文实验之前**，比的是 S7 自己造成的重写。修法是把基线
移到 `s7-context-restored` 之前，并把上下文实验的重写单独记成 `s7.rewrote`
（断言两页 + Manifest 的 mtime 都变）。红过的报告留在 `raw/accept-attempt1/`。

另一个数字变化：判据条数 74 → 75 → 76，来自两次新增（`s7.rewrote`、`s6.cache-survived`），
不是删判据凑绿。

### 关窗接线：两层证据，分开记

| 层 | 证明什么 | 谁在跑 | 判据 |
|---|---|---|---|
| 源码接线 | `wire.go` 里 `app.WithOnDestroy(r.View(), r.Shutdown)` 存在；对已包装视图再包一次被拒 | 图库 `TestRouteDestroyWiringPresent` + 变异 M1 + 未变异对照 | `m1-no-destroy-wrap.*` |
| 受控事件覆盖 | 两个事件循环（`app-loop`、`integration-loop`）收到 `DestroyEvent` 后真的调用回调 | `pkg/runtime/app` 的 `TestDestroyEventShutsDownWrappedRouter`（不经图库） | `destroy.event.pass/loops` |

两层都**不覆盖**「原生窗口关窗事件到达 Gio」这条路径。

## C3 变异、反例与窄回归

两条变异都迁进入口，且每条都带健康对照：

| 变异 | `go build ./...` | 目标用例 | 未变异对照 | 实际失败行 |
|---|---|---|---|---|
| M1 拆掉关窗包装 | rc=0 | rc=1，`--- FAIL: TestRouteDestroyWiringPresent` | rc=0 | `route_test.go:356: 生产组合未带关闭包装：对已包装视图再包一次本应被拒绝` |
| M2 两页各建模型 | rc=0 | rc=1，`--- FAIL: TestRouteSharedModelSurvivesRoundTrip` | rc=0 | `route_test.go:297: 在 x=[20 24 30 40 60 100] / y 0..420 的扫描下未命中「打开详情」（home 页…）` |

「能编译」有意义：红不是编译错误冒充的，是同一条接线上本该成立的事实。

低成本反例（`test-verify-gallery-accept.py`，14 条，**不跑 Go**，`GALLERY-ACCEPT-TEST-OK
total=14 skipped=0`）：重复目录 rc=3 且目录内容不变、旧报告 rc=2 且不建运行目录、
缺参数/未知参数 rc=2、锁忙 rc=8 零新增；台账六种坏输入（少用例点名、SKIP、FAIL、
多用例、完成标记缺项、no test files）各自判红；同字节不同 mtime 与删文件都被抓到；
变异锚点 0 次/2 次都拒绝且不写文件；副本目录已存在拒绝；两次 `new_run_id()` 不同。

可重复：同一命令换新 `--run-dir` 连跑两轮（`ga-…-c7836c`、`ga-…-686529`）各自
材料化一份新工程并全绿；旧目录一律 rc=3，所以**读不到上一轮的报告**。

窄回归与自检（基线实际用例集为准）：

- `verify-projects.py crosspackage-preferences` → rc=0，**11** pass / 0 skip / 0 fail（7.233 s）
- `verify-projects.py crosspackage-taskboard` → rc=0，**6** pass / 0 skip / 0 fail（9.091 s）
- `test-verify-projects.py` → `{"status": "PASS", "failed": 0, "skipped": 0}`，24 条

门禁（按实际影响）：`gofmt -l` 无输出；`go build ./...` OK；`go test ./internal/docgate`
ok；`go test ./internal/compiler/diagdoc` ok。本轮**不含 Go 改动**，未复跑全量 `go test ./...`。

## 五态

| 态 | 状态 |
|---|---|
| 实现 | 完成（4 个业务提交 + 本文件所在的 dev-log 提交，工作树 clean） |
| 自测 | 完成（76/76 判据 + 14 条反例 + 11/6/24 条回归自检） |
| 外审 | 未开始（交协调者） |
| 主仓 | 未合入（隔离分支 `codex/r22-workbuddy-gallery-acceptance`） |
| 远端 | 未推送（`GOXML_AUTO_PUSH=0`） |

## 未验证边界

- **原生窗口**：`cmd/gallery` 只构建、从未运行；`DestroyEvent` 从系统事件队列到 Gio
  的那一段、真实关窗、IME、真实 DPI、字体渲染都不在自动验收里。
- **跨平台**：只在 Windows / `CGO_ENABLED=0` 实测；Linux/macOS 未跑。
- **耗时数字**：`elapsedSec` 全部来自本机（Go build cache 是热的），不得当作跨机预算。
- **原因词**：资产变化仍报 `package-sources-changed`，没有 `asset-changed`；本轮只是把
  「范围」做成判据，没有改进 CLI 诊断可读性（登记给 CLI 线）。
- **锁忙路径**：真实三方争抢只覆盖了「我持锁 → 入口 rc=8」这一种；预检到委派之间的
  抢锁竞态没有构造。
