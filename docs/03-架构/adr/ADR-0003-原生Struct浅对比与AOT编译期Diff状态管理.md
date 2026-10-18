# ADR-0003：采用原生 Struct 浅对比与 AOT 编译期 Diff 状态管理
- 状态：已接受，v1 语义已冻结
- 冻结日期：2026-10-18
- 规范正文：docs/03-架构/AOT状态Diff语义.md
- 核心决策：业务层使用纯 Go Struct，编译器根据绑定路径生成 ShadowState、硬编码字段对比和节点级 Dirty Mask，结合 Gio MacroOp 局部失效，杜绝运行时反射。
- 循环规则：有状态 `<For>` 必须使用稳定 `Key`；Map 不参与 v1 自动 Diff；交互控件的输入处理不得整体缓存为绘制 Macro。
