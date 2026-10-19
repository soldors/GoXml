# TASK-BINDING-IMPLEMENT-001：接通绑定生成与绑定缓存

## 元信息

- 状态：**步骤 1 直接 Codegen 与步骤 2 已完成；Pipeline 集成未完成；步骤 3 显示缓存实现已交付，完整验收见子任务卡** | 优先级：P0 | 预计：3h
- 依赖：`TASK-BINDING-CONTRACT-001`（契约已冻结）、`TASK-CODEGEN-STATIC-CACHE-001`（静态缓存基线）
- 范围：**只做步骤 1–3**，不扩展到完整状态更新机制

## 1. 三步计划

| 步骤 | 内容 | 验收 |
|---|---|---|
| **1** | 接通 `Facts → Codegen`，生成绑定取值表达式 | 直接 `Generate` 的字段和零参方法读取已接通并有定向验证；默认 Pipeline 尚未传 `ModulePath`，集成验收未关闭 |
| **2** | 最小运行时读取验证 | ✅ `TASK-BINDING-IMPLEMENT-002` 已完成；多段路径、指针访问、零参方法、显式重新布局和失败分类均有测试 |
| **3** | 绑定显示缓存与性能验证（三层分离） | 当前实现按绑定节点回放已录制结果；稳定帧分配与局部行为已测，契约全维度验收见 `TASK-BINDING-IMPLEMENT-003` |

## 2. 五条红线（实现时必须守住）

1. **不破坏 `VerifiedDoc` 边界**：Codegen 只消费已验证文档及其 `Facts`，不得自行解析绑定路径或绕过验证。
2. **不混淆静态缓存与绑定缓存**：静态子树继续遵守契约 §16；含绑定节点按已冻结的动态边界处理，**不得把旧文本冻结进静态子树**。
3. **不提前实现状态系统**：可用测试驱动值变化，但必须明确标注这是测试驱动，**不代表生产具备通知或自动刷新**。
4. **正确性优先于零分配**：即使某方案 `allocs/op == 0`，只要值变化后显示陈旧，就不予验收。
5. **不把性能猜测写成结论**：shaping 无法直接计数时，先验证测试 seam 是否可靠；无有效观测证据就标注未验证，不得以零分配代替 shaping 缓存证据。

## 3. 步骤 1 的实现约束

- 静态属性走现有路径；绑定属性走独立路径，**禁止绑定值被误当作普通字符串常量**。
- `Generate` 仍只返回字节，不承担文件写入或提交。

### 3.1 已冻结的契约局部决策

契约 §05.3.1 已冻结：显示字符串与所有 shaping 输入均未变化时，允许复用；这是允许而非强制。实现和验收边界见 `TASK-BINDING-IMPLEMENT-003`。显示缓存回放不等同于 Gio 内部 shaping 已验证。

### 3.2 `ModulePath` 的 Pipeline 接线尚缺

`Options.ModulePath` 已加入 Codegen 和输出契约，但 `pipeline.go` 当前只填 `PackageName`
与 `SourcePath`。需从 `TypeEnv.ModulePath()` 取得模块路径并传递，再补完整管线编译测试；
完成前，跨包绑定只在测试显式调用 `Generate` 时可用。

### 3.3 方法路径已修复并验证

Analyzer 通过 `PathStep.IsMethod` 传递方法身份，Codegen 生成 `vm.Display()`；
`TestBindingRuntimePathKinds/零参方法` 已真实编译并运行。Pipeline 的 `ModulePath` 接线仍未完成。

## 3.3 步骤 1 已完成（2026-10-19）

### 实现

| 项 | 做法 |
|---|---|
| Facts 接入 | `generator.facts = v.Facts()`；绑定**只**从 Facts 取，查不到就报错 |
| 取值表达式 | `bindingExpr()`：按 `RootKind` 决定起点（vm / For 变量），再拼 `Steps` |
| `<Text>` 分支 | 有绑定 → `v.bindText(..., vm.Title)`；无 → 字符串常量 |
| vm 参数 | `assemble` 按 `usesVM` 给 `Layout` 加 `vm *<Type>` |
| **红线 2** | 含绑定页面不生成整页 `cacheOps/cacheCall/cacheDims`；绑定文本使用独立私有槽位，显示值变化时失效 |

### 生成结果（实测）

```go
func (v *GoxmlDashboardView) Layout(gtx layout.Context, th *material.Theme, vm *fixture.ViewModel) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return v.bindText(gtx, th, 0, vm.Title)
		}))
}
```

### 验收（全部通过）

| 测试 | 覆盖 |
|---|---|
| `TestBindingGeneratesReadExpr` | 生成 `vm.Title`，不把 `{Title}` 当字面量 |
| `TestBindingMultiSegment` | `{User.Name}` |
| `TestBindingPointerDeref` | `{Owner.Name}` |
| `TestBindingZeroArgMethod` | `{Display}` |
| `TestBindingMixedStaticAndBinding` | 静态与绑定混排 |
| `TestBindingStillRejectedByAnalyzer` | 非法绑定仍被 Analyzer 拦下（3 例） |
| **`TestBindingGeneratedCompiles`** | 生成代码真实编译通过 |
| `TestBindingMustNotFreezeDynamic` | 禁止整页缓存字段，保留绑定槽位缓存 |
| `TestStaticPageStillFreezes` | 对照：静态页面仍固化，未退化 |

### 两个实测发现

**① 跨模块编译验证不可行。**
ViewModel 在 `internal/compiler/analyzer/testdata/fixture`，而 Go 的 internal
规则禁止外部模块 import `github.com/goxml/goxml/internal/...`。
→ 改为在**仓库内同模块**建临时包编译，验证通过。

**② 绑定页面也需要缓存依赖。**
绑定文本槽位使用独立 `op.Ops` 与 `CallOp`，因此绑定页面也会生成
`system`/`op`/`unit`/`math` 相关的 `renderKey` 代码。

### 契约补充（需记录）

`Options` 新增 `ModulePath`：仅当 ViewModel 位于**其他包**时需要，用于拼
import 路径。同包 ViewModel 与纯静态页面不需要。这是对契约 §4.4 的补充。

## 4. 验证边界（步骤 1–3 能证明与不能证明）

能证明：

- 直接调用 `Generate` 可生成并编译字段/零参方法读取；定向运行时测试证明显式再次布局可以读取新值。
- 绑定显示缓存的实现、测试范围和未闭环项目见 [`绑定显示缓存验收报告.md`](../../06-测试/绑定显示缓存验收报告.md)。

**不能证明** ViewModel 修改后生产 UI 会自动失效、触发重绘。该能力归 `TASK-STATE-001`。

## 5. 交付物

- 绑定代码生成实现及测试（步骤 1 已交付；当前覆盖 `<Text Value>`）
- 绑定读取与显示缓存实现及测试；Pipeline 集成和契约全维度性能验收仍开放
- 运行时读取验收报告：[`绑定运行时读取验收报告.md`](../../06-测试/绑定运行时读取验收报告.md)（步骤 2 已完成）
- shaping 测试 seam 设计记录（若无需生产接口，记录原因）
- `INV-PERF-4` 验收记录：通过项 / 失败项 / 未验证项分别列出
- 开发日志：测试命令、实际结果、基准数据、并发改动来源

## 6. 回滚点

若步骤 1 导致静态缓存回归，可回退到提交 `40f2c9b`（契约冻结点），此时静态缓存契约 §16 仍完好。
