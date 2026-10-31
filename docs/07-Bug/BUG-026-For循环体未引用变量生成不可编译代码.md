# BUG-026：`<For>` 循环体未引用 Each/Index 时生成不可编译代码

## 状态
- 状态：**已修复（编译器2f003066，主仓9e81534接纳）；Image仍为未批准候选**
- 发现日期：2026-10-31
- 影响版本：`<For>` 生成路径（Image 切片测试首次覆盖）
- 关联：`TASK-IMAGE-001`、`TASK-CODEGEN-001`
- 边界：`compiler 事件/For` 当前由并行工作在制；本线只记录复现与建议，
  未改动 `emitForChildren` / `forVarUsed`，避免与其冲突

## 1. 现象

以下页面在生成阶段成功，但 `go build` 失败：

```xml
<For Each="item" In="{Items}">
  <Image Src="a.png"/>
</For>
```

```text
declared and not used: item
```

`<For Each="item" In="{Items}"><Text Value="static"/></For>` 同样复现。

## 2. 根因

`emitForChildren` 无条件生成：

```go
for _, item := range items {
    item := item
    ...
}
```

即使循环体完全不引用 `item`，Go 也会因 `item` 未使用而拒绝编译。
旧的 `Index` 判定也只检查了属性/事件中的引用，没有覆盖：

- `<If Condition="{item.Done}">` 条件；
- `Key="{item.ID}"` 在行状态池中的渲染期使用。

## 3. 建议修复

- 按实际引用决定循环头：`for i, item` / `for i` / `for _, item` / `for range`；
- `forVarUsed` 增加 `<If Condition>` 与 Then/Else 分支递归；
- Key 只有在循环体确实含交互控件（会生成行状态池）时才计入循环变量引用；
- 显式 `item := item` 遮蔽只在 Each 被引用时生成。

## 4. 建议回归测试

- 循环体只有静态子节点
- Each 只在 If Condition 中引用
- Each 只在行状态 Key 中引用且含 Button
- Each/Index 都未引用
- Image 作为静态子节点（本卡的触发用例）

## 5. 边界

- 未改变 `<For>` 的语义、状态池或 Key 规则；
- 未覆盖嵌套 `<For>`（Codegen 仍明确拒绝）；
- 未新增性能基准；循环头变化只影响生成代码文本，不影响运行时分配。

## 协调者接纳补记

基础Text场景由ZCode独立实际构建确认通过；2f003066通过结构化作用域渲染及空白赋值保证变量可仅用于Key/事件或完全未使用。主仓组合后For/If事件与Image现有测试选择器实测rc0；该组合验证不代表Image语言/ADR已批准。原建议作为当时记录保留。
