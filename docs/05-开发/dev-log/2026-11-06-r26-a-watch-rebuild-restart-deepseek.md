# 2026-11-06 R26-A WATCH-REBUILD-RESTART（DeepSeek）开发日志

基线 `1505fd455ff90e5b50b8120eb567dbcb88775184`（开工核验：主库 HEAD 同、porcelain=0、索引空）。分支 `codex/r26-deepseek-watch`，树 `product-r26-deepseek`。所有权：`internal/devx/watch/` + 本设计/开发日志。**未改** main.go/cli.go/命令帮助/根文档/go.mod/go.sum/锁助手/他线目录。
提交：`2a5d070`（A1）、`b2e9f14`（A2）、`668d755`（A3）、`db823e3`（补充真实路径测试）。

## 范围（如实）
- 实现 TASK-DEVX-001 的 **Watch worker 核心**：快照/防抖/代次/取消/进程交接。
- **未做** Hydration/JSON 状态保存恢复（本轮明确排除），因此不关闭整个 TASK-DEVX-001。
- 主库 `goxml watch` 命令**未接线**（由协调者统一接线）；本线交 `integration-proposal.diff` 最小接线建议。
- 生成复用 `internal/cli.Run(["generate", dir, "--check"])`；构建 `go build`。不新造生成器/缓存/锁/台账格式/加载管线；无新依赖。

## A1 监听与防抖（`2a5d070`）
- `config.go`：Config（Debounce 默认 100ms、Poll 50ms、BuildTimeout 120s），候选默认值。
- `snapshot.go`：标准库轮询确定性快照（排序键 + SHA-256），排除 `.git`/`.goxml`/ExcludeDirs；`readOwnedIndex` 读 `.goxml/generate-manifest.json`，`guardHeaderOwned` 校验产物保护头 `// Goxml-Version: 1` / `// Source: <base>`；**owned=Manifest登记∧保护头匹配** ⇒ 不触发自激；同名无所有权用户 Go 仍触发。
- `debounce.go`：纯时钟注入状态机——突发合并、100ms 窗口、取消、构建中再变更 final-wins、失败不发布；`Pump/BuildFinished/Cancel`。
- `watch.go`：Watch 编排（Builder/Swapper 注入端口），NotifyChange/Tick。
- 测试 `watch_a1_test.go` 8 例全 PASS（确定性）。

## A2 真生成/构建与进程切换（`b2e9f14`）
- `realbuilder.go`：RealBuilder = generate(--check) + `go build -o <genbin> <entry>`；区分 generate vs build 失败；ctx 超时；Generate/BuildFn 注入。
- `process.go`：ProcessManager 串行交接——先启新代，成功才停旧代（优雅 stdin-EOF → 有界 Kill → `taskkill /T /F` 树清理），报告真实 PID/代次/退出；新代启动失败保留旧代；ActiveAfter 断言无幸存；只管理自有树。
- `testdata/consoleapp`（无窗口 console helper）证明真实启动/替换/收口。
- 测试 `watch_a2_test.go`：真实进程 handoff（PID 不同/旧退出/1 存活/收口 0）、spawn 失败保旧、builder 分类+argv+次数、超时 —— 全 PASS。

## A3 端到端与判别力（`668d755`）
- `watch_a3_test.go`：真实 console-helper 进度演示 初始成功→突发合并(final-wins)→新代启动(不同PID)→非法GXML/坏入口不重启(旧继续服务)→修复恢复→变更中取消不发布→进程树全收口(ActiveAfter=0)；生成产物写入不自激；并发变更末代获胜。真实 generate/build 次数由 fake 计数（非墙钟）。全 PASS（E2E genCalls=5 buildCalls=4 history=3）。
- **两条可编译行为变异**（独立副本 + Go overlay）：
  - MUT-M1 去代次/取消守卫（`debounce.BuildFinished` 恒发布）→ `TestCancelDoesNotPublish` **FAIL**。
  - MUT-M2 失败仍替换旧进程（`watch.runGeneration` 失败分支也 Swap+发布）→ `TestBuildFailureNoPublish` **FAIL**。
  - 恢复（无 overlay）全 PASS。真红非编译/flags。

## 门禁（Windows/amd64，CGO=0；race 用 MinGW）
- `go build ./...` rc0；`go vet ./internal/devx/watch/` rc0；`gofmt -l` 空；`go test ./internal/devx/watch/` PASS。
- `-race`（CGO=1 CC=MinGW GORACE=halt_on_error=1）全包 rc0、0 DATA RACE。
- 覆盖 `internal/devx/watch/` **83.4%**（补充真实路径测试后，73.2%→83.4%；未覆盖集中在 stop 的超时/树杀竞态分支与需完整 CLI 工程的 realGenerate 细节；不加镜像凑数）。
- `docgate` 复验见交付。

## 候选决策（不擅定、不加公共 flag）
见 `delivery/candidates.md`：轮询间隔默认 50ms、ExcludeDirs 默认（构建输出/缓存）、优雅关闭协议（stdin-EOF→Kill→tree）、除 100ms 外的扩展策略——均为本线**选定默认**，需负责人裁决可调。

## 五态
实现 done(`db823e3`) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
