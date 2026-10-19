# 交接包：TASK-BINDING-IMPLEMENT-001/002

## 1. 当前状态

- `TASK-BINDING-IMPLEMENT-001`：直接 Codegen 绑定读取已接通；Pipeline `ModulePath` 集成未完成；绑定显示缓存代码已交付。
- `TASK-BINDING-IMPLEMENT-002`：运行时读取验收已完成，提交 `e1c7205`。
- `TASK-BINDING-IMPLEMENT-003`：显示缓存实现和定向测试已交付；完整契约场景和 Gio shaping 观测仍有缺项。

验收报告：[`绑定运行时读取验收报告.md`](../../06-测试/绑定运行时读取验收报告.md)、[`绑定显示缓存验收报告.md`](../../06-测试/绑定显示缓存验收报告.md)。

## 2. 必读依据

1. [`绑定系统契约.md`](../../03-架构/绑定系统契约.md)
2. [`绑定系统现状核查.md`](../../03-架构/绑定系统现状核查.md)
3. [`代码生成输出契约.md`](../../03-架构/代码生成输出契约.md) §4、§15、§16
4. [`AOT状态Diff语义.md`](../../03-架构/AOT状态Diff语义.md)
5. [`TASK-BINDING-IMPLEMENT-003.md`](../../04-WBS/任务卡/TASK-BINDING-IMPLEMENT-003.md)

## 3. 已验证行为

- Analyzer 只把通过类型与可访问性检查的绑定交给 Codegen。
- Codegen 从 `VerifiedDoc.Facts()` 读取路径，不重新解析原始绑定文本。
- 字段、指针字段、多段路径和零参方法均可生成并真实执行。
- 零参方法通过 `PathStep.IsMethod` 生成 `vm.Display()`，不能生成函数值 `vm.Display`。
- 含绑定页面不生成静态整页 `op.Record`/`op.CallOp`，避免把动态值冻结。
- 每个绑定 `<Text>` 使用独立显示缓存槽位；命中时回放本槽位 `CallOp`，变化时重录。
- 稳定帧当前定向样本为 `139 ns/op, 0 B/op, 0 allocs/op`；该样本不代表 shaping 计数或真实帧时间。
- 修改 ViewModel 后显式再次调用 Layout 可以观察到新值；这不证明生产自动刷新。

## 4. 当前缺口

| 缺口 | 影响 | 后续任务 |
|---|---|---|
| Pipeline 未传递 `Options.ModulePath` | 完整管线不能稳定生成跨包 ViewModel import | `TASK-BINDING-IMPLEMENT-001` |
| 场景 ③/⑤尚未按契约原始输入覆盖 | 当前测试只覆盖相同 string 和空值恢复 | `TASK-BINDING-IMPLEMENT-003` |
| Gio shaping 无直接计数 seam | CallOp 回放不能作为 shaping 命中证据 | `TASK-BINDING-IMPLEMENT-003` |
| 无状态通知/Invalidate 关联 | ViewModel 修改不会自动触发生产 UI 重绘 | `TASK-STATE-001` |
| 动态父容器内局部静态子树未固化 | 动态页面的缓存收益尚未验证 | `TASK-BINDING-IMPLEMENT-003`、`TASK-CODEGEN-STATIC-CACHE-001` |

## 5. 推荐接手顺序

1. 在 Pipeline 从 `TypeEnv` 取得 module path 并填入 `codegen.Options.ModulePath`。
2. 增加完整管线跨包绑定生成与编译测试。
3. 补齐场景 ③、⑤的适用类型/错误模型，或在契约中明确 string-only 范围的验收替代规则。
4. 继续只报告可观测的槽位回放；不得将其表述为 Gio shaping 已验证。
5. 自动刷新和状态 Diff 仍归 `TASK-STATE-001`。

## 6. 复现命令

```text
go test -count=1 -run '^TestBindingRuntime' -v ./internal/compiler/codegen
go test -count=1 ./internal/compiler/codegen
go test ./...
```
