# 交接包：TASK-CODEGEN-001（进行中）

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

- **只支持** `<Page>` + 单个静态 `<Text>`
- 保护头、`Goxml<Path>View` / `NewGoxml<Path>View` / `Layout(gtx, th)` 按契约生成
- import 由 `importSet` 按需收集、去重、冲突按路径字典序分配别名
- 一律经 `go/format.Source`；失败返回 error 且**不返回部分代码**
- **本包不写盘**：写入与原子替换归 `internal/compiler/pipeline`

**未实现**：布局（Column/Row/Grid/Spacer）、属性绑定、事件、`<For>`、多控件、
静态子树 `CallOp` 固化、虚拟化、零分配断言。对应子任务 1–3 与 4–25（见任务卡）。

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
| 当前提交 SHA | `3cd258b`（输出契约冻结 + 最小纵向切片）；编排门禁 `d5c083b`；入口门禁契约 `b4b13a9` |
| 已完成子任务 | 子任务 4（入口拒绝未验证 IR）、5（生成结果可被 `go build` 编译）、6（编排层门禁，由 ORCH-001 完成）；最小切片：`<Page>` + 静态 `<Text>`、保护头、import 收集、格式化 |
| 未完成子任务 | 子任务 1–3（布局映射、控件与 Shadow Struct、Golden 集）与 4–25 的布局/Grid/尺寸专项全部未做 |
| 已运行命令及结果 | `go test ./internal/compiler/codegen/` → ok，coverage **92.6%**（26 个测试，含 `TestGeneratedCompiles`）；切片落地时点全仓 496 个测试 |
| 已知失败或风险 | ① **生成代码尚不能运行**：无 `goui.App` 主循环、无 CLI，从 `.gxml` 到窗口的链路未打通；② `gioui.org v0.10.3` 已进 `go.mod`，`TestGeneratedCompiles` 依赖本地模块缓存（`GOPROXY=off`），换机需先 `go mod download`；③ 契约 §4.3 的 `Layout` 曾与字体策略文档冲突（主题参数缺失），已新增 §4.3.1/§4.3.2——改动 `Layout` 签名前先读这两节；④ 仅 Windows 验证 |
| 下一位接手人 | 继续扩展 Codegen（布局 → 绑定 → 事件 → `<For>`，见任务卡 §4 的扩展顺序理由）；或先补 `TASK-SEC-001` 追平流程偏差 |
