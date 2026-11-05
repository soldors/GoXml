# TASK-EXPANDER-001：组件 Schema 分类、属性事件区分与 Def/Slot 宏展开

## 元信息
- 状态：**Done**（2026-10-18）| 优先级：P0 | 实际：约 8h
- 依赖：TASK-PARSER-002
- 契约：`docs/03-架构/Analyzer输入输出契约.md` §3（`Schemas` 由本任务导出）

## 1. 目标

在 Parser 输出之上完成三件事：

1. **组件 Schema 分类**：把 `<Goxml>` / `<Defs>` 的声明整理成可查询的组件契约。
2. **属性与事件区分**：按固定的四层顺序判定每个名称的含义。
3. **`<Def>`/`<Slot>` 宏展开**：把调用点内联为渲染子树，不产生运行时组件边界。

## 2. 为什么这一层必须独立于 Parser

实施过程中被两个具体问题逼出来的：

| 发现 | 后果 |
|---|---|
| Parser 若识别属性/事件，必须内置控件白名单 | 每新增控件或第三方组件，语法层就多一处耦合 |
| 适用性检查需要 Schema 知识，但 Parser 消费过的属性 Expander 看不到 | 无法报「`Columns` 不适用于 `<Button>`」 |

因此：Parser 只做语法与属性值形态，所有属性进 `RawAttrs`；Expander 完成分类与适用性检查后才清空。

## 3. 四层分类（顺序固定）

| 顺序 | 类别 | 来源 |
|---|---|---|
| 1 | 全局布局属性 | 编译器内置表 |
| 2 | 组件属性 | `ComponentSchema.Properties` |
| 3 | 组件事件 | `ComponentSchema.Events` |
| 4 | 都不匹配 | `GXML-ATTR-004` |

**规则 4 绝不退化为事件。** 否则 `Clik="Save"`（`Click` 的拼写错误）会被静默当作事件绑定。

## 4. 宏展开语义

| 能力 | 说明 |
|---|---|
| Prop 值替换 | 绑定→绑定、静态→静态、`{User.Name}` + `User="{Account}"` → `{Account.Name}` 路径拼接 |
| 默认值 | 未传且有默认值时使用；无默认值且必填报 `GXML-DEF-008` |
| Slot 替换 | 调用点单子节点替换 `<Slot/>`；未声明插槽却传子节点报 `GXML-DEF-009` |
| 克隆隔离 | 同一 Def 多次调用产生独立副本 |
| 递归检测 | A→B→A 报 `GXML-DEF-010` |
| 颜色 Prop | `<Column Background="{Accent}">` 展开时解析为实际颜色 |

## 5. 已知限制（v1）

`<Def Events>` 只做契约声明与调用点校验，**不产生事件传播**：Def 体内没有语法能「发出」事件。

保留 `Events` 的理由：让契约、诊断与向前兼容现在就稳定，避免后续改动既有属性语义。真正的事件转发属 v2。

`<Slot/>` 只允许被**一个**子节点替换（GXML 无隐式容器节点，见规范 §7.2.2）。

## 6. 交付结果

```text
$ go test ./internal/compiler/expander/
ok  coverage: 91.7%   （87 个测试）
```

导出 `SchemaRegistry` 供 Analyzer 查询（契约 §3.1）：

```go
type SchemaRegistry interface {
    Lookup(tag string) (*ComponentSchema, bool)
}
```

- 合并内置控件（`Text`/`Button`/`Input`/`Image`）与本文档 `<Def>`；
- 布局标签返回 `(nil, false)`；
- 内置控件名无法被覆盖（Parser 以 `GXML-DEF-002` 拒绝同名 `<Def>`）。

## 6.1 后续补正：属性类型声明

初次实现时 `PropertySchema` 只写了 `Name`，`Type` 全为空（`PropUnknown`）。

这是 Analyzer `BIND-004` 的前置缺口：没有目标类型就无法判定兼容。
已在 `TASK-ANALYZER-001` 第 3 步中补齐 Text/Button/Input/Image
的全部属性类型，并在 `PropertySchema.Type` 注释中写明「必须显式声明」。

## 7. 遗留

- 自定义组件事件转发属 v2。
- Go 自定义组件（`//goxml:component`）解析未实现。

## 2026-11-05 R23-R1 证据资格复核补记（不改历史原文）

> 基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`。逐项可机读字段见交接包
> `raw/c1-evidence-qualification.json`；全文见
> [13 项证据资格复核](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)。
> 本补记只登记证据资格，**不追认历史验收、不改写卡内原有状态与判据**，
> 也不把它计入项目完成率。

| 字段 | 复核结果 |
|---|---|
| 终结论 | **有通过记录（限 2026-10-18 版本）**：产出物与测试在基线可定位；但无独立复核、无 raw、覆盖率口径与后续版本不可混用 |
| 归类 | 有通过记录（限声明范围） |
| 实现 SHA | 1ef9247（基线，expander 源码）；5e71510（导出 SchemaRegistry）；64e5855（D3 Input Schema 扩展） |
| 验收 SHA / 记录 | 无独立验收提交；卡 §6 + 交接包（事后补齐） |
| 执行方是否独立 | 否：交接包明写单次会话闭环、事后补齐 |
| 命令 / rc / raw 可定位性 | go test ./internal/compiler/expander/ → ok 91.7%；四层分类、Def/Slot 展开、递归检测等均为卡内文字；E1：无原始 stdout；10-18 dev-log 有 Parser 覆盖率补测记录（84.2→96.2、81.8→91.6）但那是 Parser/宏展开阶段数字，与本卡 91.7% 不同口径 |
| 当时范围 | Schema 分类、属性/事件四层顺序、Def/Slot 宏展开、SchemaRegistry 导出；v1 明确不做事件传播与 Go 自定义组件 |
| 现行适用性 | expander 包在基线；后续 R4（跨包绑定切片隔离）、R6（Background）、R8（Input Schema）、R15（For 变量）都改过它，91.7% 是历史值 |
| 仍缺哪一层资格 | 独立复核、现行版本窄测、原始字节 |
| 下一动作 | 需要现行资格时按现行 expander 测试集另跑 -v 留 raw；不要把 96.2%/91.6%/91.7% 三个历史数字合并 |

