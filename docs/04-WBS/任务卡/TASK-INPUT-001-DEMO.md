# TASK-INPUT-001-DEMO：Input 交互样板

## 元信息

- 状态：**进行中**（2026-10-20）
- 父卡：[TASK-INPUT-001.md](TASK-INPUT-001.md)
- 前置：D4-A/B/C ✅（`02b8593`/`3487289`/`46accb9`）
- 性质：**手动验收入口**，不是功能开发的替代品

## 1. 目标

给 D5 的人工验收提供**可观察的交互入口**，同时验证 Input 在真实窗口中可用。

## 2. 可观察场景（冻结）

| # | 场景 | 界面上的可观察结果 |
|---|---|---|
| 1 | 初始 `Value` | 输入框显示初始文本 |
| 2 | 用户连续输入 | `{回写值}` 文本随 `ValueChanged` 实时更新 |
| 3 | 程序修改 `Value` | 点按钮后输入框同步为新值 |
| 4 | `Submit` 回车 | 界面显示提交次数与最后提交文本 |
| 5 | `MaxLen=12` | 输入超长时肉眼可见被截断（无越界字符） |
| 6 | `Filter="0123456789"` | 输入字母时被过滤，只剩数字 |
| 7 | `ReadOnly="true"` | 该输入框不可编辑（焦点落上但输入无效） |

## 3. 边界（严格）

| 允许 | 禁止 |
|---|---|
| 在 `examples/demo` 现有 Page 内**追加**一块 Input 区 | 重构 demoView / 改运行时 / 改其它控件行为 |
| 新增 demoVM 字段与方法（供 Input 绑定） | 把 demo 变成功能开发线 |
| 复用现有 `{Title}`/`{Status}` 展示风格 | 顺手实现新控件或改契约 |

**现有结构保持不变**：`AddItem`/`ClearAll`/`Select`/`Delete`/`<For>`/`<If>` 一行不动。

## 4. 实现方式

demo 是**手写同构 View**（不编译生成代码），因此 Input 部分按生成形态手写：

```go
// 字段
input0   widget.Editor
applied0 string

// Layout 内（控件位置消费，不进顶部轮询）
ed := &v.input0
ed.MaxLen = 12
ed.Filter = "0123456789"
if cur := v.vm.EchoText; cur != v.applied0 {
    ed.SetText(cur)
    v.applied0 = cur
}
for {
    ev, ok := ed.Update(gtx)
    if !ok { break }
    switch e := ev.(type) {
    case widget.ChangeEvent: v.vm.OnEchoChanged(ed.Text()); v.applied0 = v.vm.EchoText
    case widget.SubmitEvent: v.vm.OnEchoSubmit(e.Text);   v.applied0 = v.vm.EchoText
    }
}
material.Editor(th, ed, "输入数字…").Layout(gtx)
```

**必须与生成形态一致**（含两条 A 更新路径），否则样板验证的不是真实行为。

## 5. 验收

| # | 检查 |
|---|---|
| 1 | `go build ./examples/demo/` 通过 |
| 2 | 冒烟：进程存活 ≥8 秒 |
| 3 | 生成代码（demo 打印）中不含 Input（因为 demo 页面用的是手写同构 View），但页面 .gxml 含 `<Input>` 且管线通过 |
| 4 | 现有按钮/列表/空状态行为不变 |
| 5 | 门禁全绿 |

## 6. 已知限制（如实声明）

- demo 的 Input 是**手写同构**，不执行生成的 Input 代码——生成代码的正确性由 codegen 测试保证
- `MaxLen`/`Filter` 的**自动化**验证仍属 D5；样板只提供人工观察入口
- IME 行为在样板中可人工尝试，但**不构成 AC-002-24 通过**
