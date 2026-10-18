# TASK-STYLE-001：实现强类型 Theme 与组件宏展开

## 元信息
- 状态：Todo | 优先级：P1 | 预计：5h
- 分支：feature/TASK-STYLE-001
- 依赖：TASK-CODEGEN-001

## 1. 目标
提供强类型主题访问，并在编译期展开 `<Def>`/`<Slot>` 组件模板，生成代码不包含运行时模板或样式解析。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-STYLE-001-1 | 定义默认 Theme、颜色字段及主题切换接口实现 | go test -v ./pkg/runtime/theme/... |
| TASK-STYLE-001-2 | 注册 `<Def>` 并校验组件定义冲突 | go test -v -run TestDef ./internal/compiler/macro/... |
| TASK-STYLE-001-3 | 展开 `<Slot>` 内容并覆盖参数、缺省与错误场景 | go test -v ./internal/compiler/macro/... |
