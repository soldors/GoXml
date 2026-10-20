# TASK-THEME-SEAM-001：冻结主题/字体接缝（路线 B）

## 元信息

- 状态：**已完成**（6/6 子任务）｜优先级：P0 ｜ 预计：3h
- 完成日期：2026-10-20
- 依赖：`TASK-CONTRACT-001`（已完成）、ADR-0012（已接受）
- 依据：[ADR-0012](../../03-架构/adr/ADR-0012-主题字体边界与Spacer主轴默认值.md)、
  [STYLE-FONT依赖审计.md](../../05-开发/STYLE-FONT依赖审计.md)
- 立卡日期：2026-10-20

## 1. 目标

在**不实现跨平台字体**的前提下，先把字体/主题的层间边界冻成可执行形式：

- 生成代码只接受已配好字体的 `*material.Theme`，不引用任何字体字段；
- 字体探测、fallback 链、懒加载与 `Shaper` 配置全部由 `pkg/runtime/theme` 承担；
- 跨层通道只有 `contracts.FontProvider` 一条；
- 两侧文档不得再各说各话（由门禁保证）。

范围严格限定在**接口、包边界、契约修订与验收方案**。
平台字体实现属 `TASK-FONT-001-A..D`，不在本卡。

## 2. 为什么先立这张卡

审计（`docs/05-开发/STYLE-FONT依赖审计.md`）证明：`TASK-FONT-001` 声明的依赖
`TASK-STYLE-001` 中，只有「Theme / FontConfig / 主题构建入口」是真依赖，
`<Def>/<Slot>` 宏展开与字体**无代码耦合**（且已实现于 `internal/compiler/expander`）。
因此不能让字体工作等整个 STYLE；但也不能直接开做——因为 `pkg/runtime/theme`
这个包**不存在**，`FONT-001-1..-4` 的验收命令目前全部空转。

## 3. Subtask 清单与验收命令

| 子任务ID | 内容 | 验收命令 | 状态 |
|---|---|---|---|
| TASK-THEME-SEAM-001-1 | 冻结 `FontProvider.Resolve(family string) (string, error)`：职责、错误语义（`ErrFontNotFound`）、调用时机（不得每帧、不得阻塞首帧、结果可缓存） | `make contract-lint` | ✅ 完成（ADR-0012，2026-10-20） |
| TASK-THEME-SEAM-001-2 | 判定现有 `ThemeProvider` 孤儿接口：保留 / 替换 / 删除 | 契约注释写明处置与替换规则；`make contract-lint` | ✅ 完成：保留（理由与替换规则写在 `contracts/runtime_api.go`） |
| TASK-THEME-SEAM-001-3 | 文档一致性门禁：禁止字体规范再要求生成代码引用字体字段；输出契约必须仍是 `th *material.Theme`；Codegen 实际签名必须一致 | `go test -run 'TestThemeRouteBDocumentationConsistent\|TestGeneratedLayoutSignatureMatchesRouteB' ./internal/docgate/` | ✅ 完成（`internal/docgate/theme_seam_test.go`） |
| TASK-THEME-SEAM-001-4 | 冻结包边界：`pkg/runtime/theme`（字体解析、fallback、主题构建）与 `pkg/runtime/app`（启动、窗口生命周期） | 契约与规范章节核对 + `go build ./...` | ✅ 完成（ADR-0012 §契约与包边界；代码未动） |
| TASK-THEME-SEAM-001-5 | 抽取主题构建入口：`pkg/runtime/theme.NewTheme()`，`pkg/runtime/app` 改为调用它，不再硬编码 `material.NewTheme()`，并**保留** `RunWith`/`NewApp`/`RunWindow*` 的显式注入入口 | `go test ./pkg/runtime/...`、`go test -run 'TestThemePackageBoundary' ./internal/docgate/` | ✅ 完成（2026-10-20） |
| TASK-THEME-SEAM-001-6 | 验收方案：`FONT-001-A..D` 各自的验收命令必须指向**存在的**包且 `-run` 能命中测试；未实现项用显式 skip 登记，不得留下空转命令 | `go test -run TestFont001AcceptanceCommandsAreNotVacuous ./internal/docgate/` | ✅ 完成（2026-10-20） |

### 3.1 -5 的实现边界（不可回退）

`Run` 与 `RunWith` 的语义**不同**，不能把 `material.NewTheme()` 一换了之：

| 入口 | 主题来源 | 说明 |
|---|---|---|
| `Run(view, cfg)` | `theme.NewTheme()`（经 `newTheme` 注入点） | 便捷入口；默认主题不暴露给调用方 |
| `RunWith` / `NewApp` / `RunWindow*` | 调用方显式注入 | 参数、校验、生命周期语义**未改**；注入主题按指针原样传递 |

三条锁：

1. `pkg/runtime/app/app.go` 不得出现 `material.NewTheme()` 调用（AST 门禁，注释里的引用不算）；
2. `pkg/runtime/theme` 不得反向导入 `pkg/runtime/app` 或 `gioui.org/app`；
3. 两条路径各有行为测试：`Run` 走默认入口（把入口返回值换成 nil 会让它报错），
   `RunWith` **不**走默认入口（把入口换成返回 nil 也不影响它）。

## 4. 验收要求

1. **契约一致性检查必须通过**：不允许出现「一边规定 `th *material.Theme`、
   另一边又要求 `th.Fonts.Family`」的文档组合（由 -3 的两条测试锁住）。
2. `make contract-lint` 通过（`go vet ./contracts/...` + `go test -v ./contracts/...`）。
3. 不得并存两个没有调用方的同类抽象：`ThemeProvider`（颜色）与 `FontProvider`
   （字体）必须各写清楚**当前状态**与**接线条件**。
4. `pkg/runtime/theme` 出现前，**不得**声称 FONT-001 的验收命令可用。
5. 入口存在 ≠ 字体已支持：`NewTheme()` 当前等价于 `material.NewTheme()`，
   FONT-001-A..D 仍全部未实现，由显式 skip 的占位用例标记。

## 5. 代码边界

| 文件/模块 | 责任 |
|---|---|
| `contracts/runtime_api.go` | `FontProvider` / `ErrFontNotFound` / `ThemeProvider` 状态说明 |
| `pkg/runtime/theme/theme.go` | 主题构建入口 `NewTheme()`（**已创建**；当前只包装 Gio 默认主题） |
| `pkg/runtime/theme/font_pending_test.go` | FONT-001-A..D 的显式 skip 占位用例（**已创建**） |
| `pkg/runtime/app/app.go` | 启动与窗口生命周期；默认主题取自 theme 包，注入入口保留 |
| `pkg/runtime/app/theme_entry_test.go` | 两条主题路径的行为边界测试（**已创建**） |
| `internal/docgate/theme_seam_test.go` | 路线 B 文档一致性 + 包边界 + 验收命令非空转门禁 |
| 生成代码 | 只接受 `*material.Theme`，不含字体路径、字号常量、`Shaper` 构建 |

## 6. 交接记录

| 项目 | 内容 |
|---|---|
| 当前提交 SHA | 见 dev-log 2026-10-20 §22 |
| 已完成子任务 | -1、-2、-3、-4、-5、-6（全部） |
| 未完成子任务 | 无（字体实现属 FONT-001-A..D，不在本卡） |
| 已运行命令及结果 | `go test ./pkg/runtime/...` → 通过；`go build ./...` → 通过；`go vet ./...` → 通过；`make contract-lint` → 通过；`go test ./internal/docgate/` → 通过（含新增两条门禁）；四条 FONT-001 命令 → 4 × `SKIP`（未执行） |
| 已知失败或风险 | `Resolve` 返回 `string` 标识是否需要更多信息，要等 FONT-001-A 实测；如需改签名须走 ADR。`pkg/runtime/theme` 尚未做任何字体工作，别把「入口存在」读成「字体已支持」 |
| 下一位接手人 | FONT-001-A |
