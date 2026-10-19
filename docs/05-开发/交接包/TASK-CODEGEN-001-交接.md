# 交接包：TASK-CODEGEN-001（进行中）

> **事后补齐**：本任务已在单次会话内闭环（有 Git 提交与验收报告）。
> 此文件按 `AGENT.md` §4 第 3 条「可以写」的情形补写，用于沉淀关键决策与易被误解的边界，
> 不作为跨会话续接的必需品。

## 1. 任务状态

- 状态：**进行中**（最小纵向切片已于 2026-10-19 完成）| 优先级：P0
- 依赖：TASK-ANALYZER-001（已完成并整体验收）
- 输出契约：`docs/03-架构/代码生成输出契约.md`（§2.1–§2.3 入口与门禁，§4 结构，§13 验收 20 条）
- ⚠️ 前置偏差：`TASK-SEC-001`（威胁建模）要求「Codegen 接入前完成」，但 Codegen 已落地。
  该流程偏差已记录在 dev-log 2026-10-19 §5，**不要修改历史措辞抹平**。

## 2. 必读依据

1. [代码生成输出契约.md](../../03-架构/代码生成输出契约.md)（唯一输出依据）
2. [编译编排契约.md](../../03-架构/编译编排契约.md)（谁调用 Codegen、谁写盘）
3. [性能预算与优化策略.md](../../03-架构/性能预算与优化策略.md)（0 分配、静态子树固化）
4. [ADR-0008](../../03-架构/adr/ADR-0008-布局属性模型.md)（Align/Justify/Gap/尺寸/Grid 的映射依据）
5. 上游交接：[TASK-ANALYZER-001-交接.md](TASK-ANALYZER-001-交接.md)

## 3. 当前实现范围（断点）

```go
func Generate(v *analyzer.VerifiedDoc, opts Options) ([]byte, error)
```

- 支持 `<Page>`、静态 `<Text>`、静态 `<Column>/<Row>` 多子节点和静态子树缓存
- 已接通 `<Text Value="{字段路径}">` 的字段/零参方法绑定读取；运行时读取验收见 [`绑定运行时读取验收报告.md`](../../06-测试/绑定运行时读取验收报告.md)
- 保护头、`Goxml<Path>View` / `NewGoxml<Path>View` / `Layout(gtx, th)` 按契约生成
- import 由 `importSet` 按需收集、去重、冲突按路径字典序分配别名
- 一律经 `go/format.Source`；失败返回 error 且**不返回部分代码**
- **本包不写盘**：写入与原子替换归 `internal/compiler/pipeline`

**未实现或未验收**：完整布局属性、Grid/Spacer、事件、`<For>`、多控件、
Pipeline 的 `ModulePath` 传递、绑定缓存、状态 Diff、动态/静态混合缓存、虚拟化和完整 CLI 输出管理。

## 4. 两层门禁（不可互替）

| 层 | 职责 | 状态 |
|---|---|---|
| 类型入口 | 只接受 `*analyzer.VerifiedDoc`，未验证 IR 在编译期传不进来 | ✅ 已验证 |
| 编排层 | 失败时**不调用** Codegen、不写任何文件 | ✅ 已由 `TASK-ORCH-001`（`d5c083b`）验证 |

不能只凭 `VerifiedDoc` 类型就宣称编排层门禁已验证——两者是不同的事实。

## 5. 验收命令

```text
go test ./internal/compiler/codegen/
go test ./internal/compiler/codegen/ -run TestGeneratedCompiles   # 临时模块 go build（GOPROXY=off）
go test ./internal/compiler/pipeline/                             # 门禁归属
```

## 6. 交接记录

| 项目 | 内容 |
|---|---|
| 状态基准 | 静态缓存与绑定读取分别见 `TASK-CODEGEN-STATIC-CACHE-001`、`TASK-BINDING-IMPLEMENT-001/002`；当前工作区未把完整 Codegen 任务关闭 |
| 已完成子任务 | 入口拒绝未验证 IR、生成结果可被 `go build` 编译、编排层门禁、静态 Page/Text/Column/Row 缓存、字段/零参方法绑定读取 |
| 未完成子任务 | 完整布局属性、控件与 Shadow Struct、Golden 集、Pipeline `ModulePath`、绑定缓存、事件、For、Grid、动态边界和完整输出管理 |
| 已运行命令及结果 | `go test ./...` → ok；`go test -count=1 -run '^TestBindingRuntime' -v ./internal/compiler/codegen` → ok；绑定步骤 3 尚未开始 |
| 已知失败或风险 | ① 无 CLI：只能以库或 `examples/demo` 方式驱动（运行入口已由 `pkg/runtime/app` + `TASK-DEMO-001`（`eb8a7b2`）打通，本卡落地时点尚无）；② `gioui.org v0.10.3` 已进 `go.mod`，`TestGeneratedCompiles` 依赖本地模块缓存（`GOPROXY=off`），换机需先 `go mod download`；③ 契约 §4.3 的 `Layout` 曾与字体策略文档冲突（主题参数缺失），已新增 §4.3.1/§4.3.2——改动 `Layout` 签名前先读这两节；④ 仅 Windows 验证 |
| 下一位接手人 | 先完成 `TASK-BINDING-IMPLEMENT-001` 的 Pipeline `ModulePath` 接线，再按 `TASK-BINDING-IMPLEMENT-003` 设计 seam 和缓存；随后扩展布局属性、事件与 `<For>` |
