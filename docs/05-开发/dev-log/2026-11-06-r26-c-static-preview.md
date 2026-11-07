# 2026-11-06 · R26-C 静态预览（STATIC-PREVIEW）

- 基线：`1505fd455ff90e5b50b8120eb567dbcb88775184`
- 分支：`codex/r26-workbuddy-preview`（工作树 `C:/Users/admin/.codex/worktrees/product-r26-workbuddy/Goxml`）
- 所有权：`internal/devx/preview/`（本线从零创建；未动 `internal/devx/watch/`）
- 交付：`D:/Codes/Goxml-agent-rounds/2026-11-06/product-r26/workbuddy/delivery/`

## 1. 目标与范围

实现独立静态 GXML 预览核心（TASK-DEVX-002）：把一个无 ViewModel 的
纯静态 `.gxml` 编译成生成 View、接专用隔离宿主、真实 `go build` 出
Windows 预览宿主——**只构建不运行原生窗口**。不是用户业务应用启动器，
不是 Watch。

## 2. 三个切片

| 切片 | 提交 | 内容 |
|---|---|---|
| C1 核心与输入边界 | `2d2eb3e` | `Run` 六段生命周期（resolve→frontend→scope→generate→host→built）；复用 `pipeline.Prepare/RunPrepared` + 资产门禁（TargetDir=SourceDir 与默认生成同口径）；VM pre-parse 早拒绝；scope 门禁 5 类拒绝码；16 测试 |
| C2 隔离宿主 | `41799b9` | MkdirTemp + 所有权标记；go.mod 复制源码根 + replace；go.sum 复制仓库根；构造函数名从产物 AST 提取；资产有效字节复制（与门禁同读取口径）；离线零 CGO 构建；失败保留证据；`HostRunner` 执行控制 + console helper |
| C3 同源验收 | 见交付 handoff | canary 依赖图证明；PNG/JPEG 字节一致；变异 M1/M2 翻红恢复；integration-proposal.diff；手工窗口步骤 |

## 3. 关键实测发现

1. **交互控件即动态页**（C1 期间发现）：`codegen.isInteractiveTag` 把
   `Button`/`Input` 无条件判为动态，无 VM 页面含它们必然在 codegen 报
   "page is dynamic"。预览以 `PREVIEW-005` 显式拒绝（任务书批准范围本就
   不含交互控件）；`interactiveTags` 镜像该集合，不同步时 fail-closed
   到 codegen 错误。
2. **$Theme 背景是静默不绘制**：`decoration.go` 对 `IsThemeRef` 背景直接
   跳过绘制。预览拒绝（`PREVIEW-004`）而不是接受一个缺背景的预览。
3. **无 VM 绑定/事件由标准前端先拒**（"no ViewModel is declared"）：
   预览不重复判，PREVIEW-002/003 是纵深防御（白盒测试覆盖）。
4. **静态页整页缓存**：无 VM 页面产物带 `v.cached/cacheKey`（整页静态
   固化），无 `v.slots`/`v.vm`——预览产物断言据此钉住。

## 4. 隔离与所有权

- 用户模块全程只读（测试快照判据：文件集合 + sha256 前后全等）。
- 宿主依赖图 `go list -deps` 194 行不含用户模块路径（canary init/main
  放置实测；测试不运行宿主来猜安全）。
- 清理只删 token 匹配的自有目录；标记缺失/token 不匹配（旧目录）/卷根
  一律拒绝。
- 资产：门禁校验字节 = 宿主嵌入字节（同一 `readAssetBytes` 路径）；
  overlay 下嵌 overlay 有效字节，磁盘用户资产不动。

## 5. 变异取证（可编译变异，恢复判据＝字节 SHA-256）

| 变异 | 改动 | 翻红判据 | 恢复 |
|---|---|---|---|
| M1 所有权 | `ownsHostDir` 丢弃 token 比对 | `token 不匹配（旧目录）不应被清理` | host.go SHA `2608acef…` 复原，测试回绿 |
| M2 字节一致 | 资产复制丢 overlay（传 nil） | `got sha=81fbbbea… want sha=3771ead7…`（磁盘字节 vs overlay 有效字节） | 同上 |

## 6. 验证记录（本机 Windows，零 CGO）

- `go test ./internal/devx/preview/ -count=1 -timeout 25m`：**23 项全绿**
  （16.4s 量级；含真实宿主构建 11868160 字节、进程树收口）。
- 回归：`./contracts/...` ok；`./internal/compiler/...` 全绿
  （codegen 693.7s）。
- `gofmt -l internal/devx/` 空；`go vet ./internal/devx/...` 通过。
- 重负载运行持公共锁（`D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock`，
  token `c00bf624…`，本 dev-log 写作时已释放，见 commands.jsonl）。
- integration-proposal.diff 在工作树临时应用验证
  （build/vet/gofmt 通过、`git apply --check` 通过）后恢复原状。

## 7. 未证边界

- 原生窗口显示/关窗：未运行（手工步骤在交付
  `manual-window-acceptance.md`，仅步骤不执行）。
- CLI 接线未落地（提案交协调者；本线未改 `cmd/goxml/main.go`、
  `internal/cli/cli.go`、帮助文本）。
- 跨平台未验证：全部实测 Windows；Linux/macOS 不外推。
