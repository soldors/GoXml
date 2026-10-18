# 交接包：TASK-EXPANDER-001

> **事后补齐**：本任务已在单次会话内闭环（有 Git 提交与验收报告）。
> 此文件按 `AGENT.md` §4 第 3 条「可以写」的情形补写，用于沉淀关键决策与易被误解的边界，
> 不作为跨会话续接的必需品。

## 1. 任务状态

- 状态：**Done**（2026-10-18）| 优先级：P0 | 实际：约 8h
- 依赖：TASK-PARSER-002
- 目标：组件 Schema 分类、属性与事件区分、`<Def>`/`<Slot>` 编译期宏展开。
- 契约：`docs/03-架构/Analyzer输入输出契约.md` §3（`Schemas` 由本任务导出）

## 2. 必读依据

1. [GXML语言规范.md](../../03-架构/GXML语言规范.md) §5（事件语法与 Schema 分类）、§7（Def/Slot）
2. [Analyzer输入输出契约.md](../../03-架构/Analyzer输入输出契约.md) §3.1（`SchemaRegistry`）
3. [TASK-EXPANDER-001.md](../../04-WBS/任务卡/TASK-EXPANDER-001.md)
4. 上游交接：[TASK-PARSER-001-交接.md](TASK-PARSER-001-交接.md)

本层只做分类与展开，**不做类型校验**（归 Analyzer）、**不生成代码**（归 Codegen）。

## 3. 代码边界

| 文件/模块 | 责任 |
|---|---|
| `internal/compiler/expander/expander.go` | 总入口 `Expand`、四层分类、遍历与诊断 |
| `internal/compiler/expander/schema.go` | `ComponentSchema` / `PropertySchema` / 布局属性表 |
| `internal/compiler/expander/registry.go` | `SchemaRegistry`：内置控件 + `<Def>` 的统一查询入口 |
| `internal/compiler/expander/expand_def.go` | `<Def>`/`<Slot>` 宏展开、Prop 替换、克隆隔离、递归检测 |
| `internal/compiler/expander/color.go` | 颜色 Prop 的解析 |

导出契约：`SchemaRegistry.Lookup(tag) (*ComponentSchema, bool)`；布局标签返回 `(nil, false)`；
内置控件名（`Text`/`Button`/`Input`/`Image`）不可被 `<Def>` 覆盖（Parser 以 `GXML-DEF-002` 拒绝）。

## 4. 关键决定（改动前必须理解）

| 决定 | 理由 |
|---|---|
| 四层顺序固定：全局布局属性 → 组件属性 → 组件事件 → `GXML-ATTR-004` | 顺序即语义，不可调换 |
| **规则 4 绝不退化为事件** | 否则 `Clik="Save"` 拼写错误会被静默当作事件绑定 |
| Parser 只出 `RawAttrs`，分类在 Expander 完成后才清空 | Parser 不能内置控件白名单；适用性检查需要 Schema 知识 |
| `<Slot/>` 只允许被一个子节点替换 | GXML 无隐式容器节点（规范 §7.2.2） |
| `<Def Events>` 只声明不传播 | v1 无语法能「发出」事件；保留字段是为契约与向前兼容 |

## 5. 验收命令

```text
go test ./internal/compiler/expander/
go test ./internal/compiler/expander/ -run TestDef
go test ./...                              # 全仓
```

## 6. 交接记录

| 项目 | 内容 |
|---|---|
| 当前提交 SHA | 主体实现早于版本控制（仓库基线提交 `1ef9247` 已含本包）。可见的后续提交：`5e71510`（导出 `SchemaRegistry` 供 Analyzer 查询，Analyzer 契约变更 1/3） |
| 已完成子任务 | 四层分类；`<Def>`/`<Slot>` 展开（Prop 三种替换：绑定/静态/路径拼接）；默认值与 `GXML-DEF-008`；插槽单子节点与 `GXML-DEF-009`；克隆隔离；递归检测 `GXML-DEF-010`；颜色 Prop |
| 未完成子任务 | 无（本任务范围内） |
| 已运行命令及结果 | `go test ./internal/compiler/expander/` → ok，coverage **91.7%**（87 个测试） |
| 已知失败或风险 | ① 自定义组件事件转发属 v2，调用点处理器签名**不被校验**——Analyzer 侧由 `TestDefEventNotSignatureChecked` 锁定该行为，改动前先看该测试；② Go 自定义组件（`//goxml:component`）解析未实现；③ 初次实现时 `PropertySchema.Type` 全为空，已在 Analyzer 第 3 步补齐——**新增内置控件属性必须显式声明 `Type`**，否则 `BIND-004` 无法判定 |
| 下一位接手人 | 下游 Analyzer 已完成、Codegen 进行中。当前断点见 `TASK-CODEGEN-001-交接.md` |
