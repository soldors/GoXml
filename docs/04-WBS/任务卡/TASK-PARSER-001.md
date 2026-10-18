# TASK-PARSER-001：实现带行列号的 GXML 词法/语法分析器与控制流解析

## 元信息
- 状态：**Done**（2026-10-18）| 优先级：P0 | 实际：约 6h
- 依赖：TASK-CONTRACT-001

## 1. 目标
解析 `.gxml` 文件为带精确源码行列号的 `*contracts.DocumentIR` 树，支持 `<If>` 和 `<For>` 控制流。

## 2. 架构决策：复用 `encoding/xml`

先行验证了三条路线：

| 方案 | 属性定位 | XML 错误定位 | 实现量 | 结论 |
|---|---|---|---|---|
| 纯 `encoding/xml` | ❌ `xml.Attr` 无位置 | 仅行号 | 最小 | 无法按属性报错 |
| 自写词法器 | ✅ | ✅ | ~800 行 | 需重写转义/注释/CDATA 规则 |
| **`encoding/xml` + 偏移重扫** | ✅ 已验证 | ✅ | ~400 行 | **采用** |

**关键事实（实测确认）：**

1. `Decoder.InputPos()` 返回的列号是**准确**的（早期误判源于测试用例缩进恰好等于行号）。
2. `Decoder.InputOffset()` 返回 token 结束处的字节偏移。
3. `xml.Attr` **不带**位置信息，但可通过「起始标签原始字节区间 + 在原文中查找属性名」反推精确偏移。
4. XML 属性名**不允许 `@`**，但允许 `:`。这是 `@Click` 语法必须废弃的直接原因。

实现要点：`sourceMap` 维护行首偏移表，列号按 **rune** 计数（中文占 3 字节但算 1 列），保证编辑器定位正确。

## 3. 职责边界：Parser 不做属性/事件分类

Parser 只负责语法，不判断某名称是属性还是事件——那需要组件 Schema 知识，属于 Expander。

若 Parser 依赖控件白名单，语法层会逐渐掺入类型知识，新增控件或第三方组件时产生耦合。

**但布局属性的值形态在 Parser 解析**（只依赖属性自身语法），同时**保留在 `RawAttrs` 中**，使 Expander 能执行语境相关的适用性检查。

## 4. 实现的文件

| 文件 | 职责 |
|---|---|
| `source.go` | 字节偏移 → 行列映射（rune 列号）、标签名常量、内置控件名 |
| `document.go` | 文档结构：根节点、`<Defs>`、`<Page>`；token 循环与 XML 错误转换 |
| `attrs.go` | 属性定位与值分类（静态/绑定）、`EdgeInsets`/`Color` 解析 |
| `layout.go` | 布局属性：`Align`/`Justify`/`Gap`/`Width`/`Height`/`Columns` |
| `element.go` | 元素、`<Def>`、`<Slot>`、`<Page>` 解析；保留名与作用域检查 |
| `control_flow.go` | `<If>`/`<Then>`/`<Else>`/`<For>` |
| `def_events.go` | `<Def Events="Open(id:string)">` 声明解析 |
| `diag.go` | 诊断码与构造 |

## 5. 验收结果

```text
$ gofmt -l .                  → 无输出
$ go build ./...              → exit 0
$ go vet ./...                → exit 0
$ go test ./...               → PASS
    contracts    coverage: 92.9%   （38 个测试）
    parser       coverage: 86.8%   （93 个测试）
    expander     coverage: 96.2%   （40 个测试）
```

## 6. 覆盖的语义

- 文档结构：根节点必须是 `<Goxml Version="1">`，`[<Defs>]` + `<Page>`，顺序固定。
- 属性值：静态字面量 / 完整绑定 `{Path}` / 取反 `{!Path}`；禁止混合。
- 布局属性：`EdgeInsets` 1/2/4 参数、`Color` 的 `#RRGGBB`/`#RRGGBBAA`/`$Theme.Field`、`Align`、`Justify`、`Gap`、`Width`/`Height`、`Columns`。
- 控制流：`<If>` 必须含 `<Then>`、可选 `<Else>`；`<For>` 的 `Each`/`In`/`Index`/`Key`/`Virtualize`/`ItemExtent`。
- 有状态循环缺 `Key` 时编译失败（防 BUG-001 状态串位）。
- `<Def>`：`Name`/`Props`/`Events`，单一渲染根节点，最多一个 `<Slot/>`，保留名检查（含内置控件名）。
- 文本节点、处理指令、指令节点一律拒绝并给出精确位置。

## 7. 覆盖率补充

首轮 `expander` 覆盖率为 84.2%，略低于 85% 门槛。补测后达 **96.2%**，主要补上：

- `classifyTree` 的 `If`/`For`/`Def` 分支（40% → 86.7%）：Then/Else/For 体内/Def 体内的元素也必须分类，未知名称在嵌套结构中同样要报错。
- `registerDef` 的事件与布局属性冲突、重复事件声明。
- `parseEvent` 的括号缺失、非法方法名、空实参。
- `isNumberLiteral` 的负数与多小数点。
- `Expand` 的 nil 文档与缺 `Page` 的健壮性。

第二轮（宏展开加入后）覆盖率降至 81.8%，补测至 **91.6%**，主要补上：

- `<Slot/>` 出现在 `<If>`/`<Else>`/`<For>` 内的替换（`replaceSlotList`、`cloneList`）。
- `cloneNode` 对 `If`/`For`/`Def`/`Columns` 的深拷贝分支。
- 事件参数中的 Prop 替换（`substituteEvents`）。
- 颜色的六种拒绝形式与 `#RRGGBBAA`、非法静态值。
- 调用点布局属性的不适用检查与事件语法校验。

## 8. 完成情况：宏展开

`<Def>`/`<Slot>` 的展开已在 `expander` 内实现（原计划归 TASK-STYLE-001，因它依赖 Schema 分类，与分类放在同一层更自然）。

实现要点：

| 能力 | 说明 |
|---|---|
| Prop 值替换 | 绑定 → 绑定、静态 → 静态、`{User.Name}` + `User="{Account}"` → `{Account.Name}` 路径拼接 |
| 默认值 | 未传且有默认值时使用默认值；无默认值且必填时报 `GXML-DEF-008` |
| Slot 替换 | 调用点单子节点替换 `<Slot/>`；未声明插槽却传子节点报 `GXML-DEF-009` |
| 克隆隔离 | 同一 Def 多次调用产生独立副本（`cloneNode` 深拷贝） |
| 递归检测 | A→B→A 报 `GXML-DEF-010`，用 `inExpansion` 覆盖「克隆→替换→再展开」全程 |
| 颜色 Prop | `<Column Background="{Accent}">` 展开时解析为实际颜色；未解析的绑定/非法静态值报 `GXML-ATTR-005` |

### v1 限制（已写入规范 §7.2.3）

`<Def Events>` 只做契约声明与调用点校验，**不产生事件传播**：Def 体内没有语法能"发出"事件。调用点传入的处理器会被校验但不出现在展开结果中。真正的事件转发需要新语言机制（事件冒泡或回调插槽），属 v2。

保留 `Events` 的理由：让契约、诊断与向前兼容现在就稳定，避免后续改动既有属性语义。

## 9. 遗留

- 未实现 Go 自定义组件（`//goxml:component`）解析，属 Analyzer。
- 自定义组件事件转发属 v2。

