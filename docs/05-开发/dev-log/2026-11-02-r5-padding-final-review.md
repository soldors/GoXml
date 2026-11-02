# R5 Padding 最终独立复核与证据补齐

本轮基线为 ZCode 的纯 Padding 子集 `b2a81db`（父链包含容器 Padding 的 `ceb8487`），原作者树只读。原作者 Background 的区域语义仍是候选，本轮没有拷入其绘制代码或组合测试。

逐行复核确认：非零静态 Padding 通过 `layout.Inset` 在运行时按 `Metric` 换算 dp；零值不额外生成 Inset。Text 的绑定显示缓存槽位、For 内直接布局分支保持原规则，Button/Input 仅包裹布局表达式，不改变事件轮询顺序。静态视图的 render key 已含 Metric，原有 Metric 变化探针实测通过。

## 补齐的判据

- 新增 `padding_click_input_test.go`：真实生成、真实 Gio `input.Router`；Button 外部/叶子 Padding 不命中、内容命中、两视图状态独立、静默帧不重复点击；Input 的外部/叶子 Padding 不获焦、内容获焦、Edit 回写和独立下一帧 Return 提交，以及静默帧无重复提交。
- 补 Grid Padding 离屏像素几何，容器 Padding 的运行时证据不只覆盖 Row/Column。
- `padding_stable_test.go` 的固定静态夹具以 `AllocsPerRun` 严格断言稳定帧零分配，注释改为与实际断言一致，不再称“同一数量级”。

## 变异证据与边界

协调者交付目录中的 overlay 仅改代码生成器副本：在静态生成视图的每次 `Layout` 中将 `make([]byte, 1)` 写入包级 sink。生成源码能编译，Plain/Padded 两态均因 `stable frame allocated: 1` 报红；没有编译失败冒充判据失败。原生产源码未被改写，overlay 移除后普通路径回绿。

`go list -json` 实测把 `padding_click_input_test.go` 列在 `TestGoFiles` 中；该文件名没有平台筛选含义。旧轮已删除文件的完整字节与失败命令原件不在交付中，因此不能还原当时“找不到测试”的原因，也不能将其归因于 detached 树的构建缓存。本轮只报告重新建立的同名测试真实执行通过。

原交付把片2 deferred、片3缺行为红证同时称为“三片全部完成”，证据强度不成立；本记录补实际缺口，不回写旧冻结记录。完整三线组合、覆盖率与主仓接纳由协调者在组合树统一完成。本轮不运行原生窗口，不做性能达标或跨平台结论。
