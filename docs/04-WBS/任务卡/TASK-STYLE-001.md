# TASK-STYLE-001：实现强类型 Theme 与组件宏展开

## 元信息
- 状态：**部分完成**（`<Def>`/`<Slot>` 已实现于 `internal/compiler/expander`；
  Theme 与主题色渲染未完成）| 优先级：P1 | 预计：5h
- 分支：feature/TASK-STYLE-001
- 依赖：TASK-CODEGEN-001
- 2026-10-20 拆分：字体功能**不再依赖本卡**（审计 `docs/05-开发/STYLE-FONT依赖审计.md`）；
  `FontConfig` / 主题构建入口归入 `TASK-THEME-SEAM-001`

## 1. 目标
提供强类型主题访问，并在编译期展开 `<Def>`/`<Slot>` 组件模板，生成代码不包含运行时模板或样式解析。

## 2. Subtask 清单与验收命令

| 子任务ID | 内容 | 验收命令 | 状态 |
|---|---|---|---|
| TASK-STYLE-001-1 | 定义默认 Theme、颜色字段及主题切换接口实现；**并让 Codegen 渲染主题色**（IR 已有 `NRGBAColor{IsThemeRef, ThemeField}`，Codegen 完全未用） | `go test ./pkg/runtime/theme/...`（**该包尚不存在，命令当前空转**）+ `go test ./internal/compiler/codegen/` | ⬜ 未完成 |
| TASK-STYLE-001-2 | 注册 `<Def>` 并校验组件定义冲突 | `go test -run 'TestExpand' ./internal/compiler/expander/`（**已修正**：原命令指向不存在的 `internal/compiler/macro/...`） | ✅ 已实现于 `internal/compiler/expander/expand_def.go` |
| TASK-STYLE-001-3 | 展开 `<Slot>` 内容并覆盖参数、缺省与错误场景 | `go test -run 'TestExpandSlot' ./internal/compiler/expander/`（**已修正**，同上） | ✅ 已实现：`TestExpandSlotReplacement` / `TestExpandSlotEmptyBody` / `TestExpandChildrenWithoutSlotRejected` / `TestExpandSlotMultipleChildrenRejected` / `TestExpandSlotInsideIf|IfElse|For` |

### 2.1 拆分说明

原卡把「主题接口」与「`<Def>`/`<Slot>` 宏展开」捆在一起，并把 `TASK-FONT-001`
的依赖指向本卡。审计后确认：

- `<Def>`/`<Slot>` 与字体**无代码耦合**，且已实现（不在卡片写的 `internal/compiler/macro/` 目录里）；
- 字体真正需要的是 Theme / FontConfig / 主题构建入口，已拆到 `TASK-THEME-SEAM-001`。

因此本卡剩余工作量主要是 **STYLE-001-1 的后半段**：把已有的 `IsThemeRef` 落到生成代码
与运行时，而不是重写宏展开。
