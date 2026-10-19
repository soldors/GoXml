# TASK-BINDING-IMPLEMENT-002：绑定运行时读取验证

## 元信息

- 状态：**完成** | 优先级：P0 | 预计：2h
- 依赖：`TASK-BINDING-IMPLEMENT-001`（步骤 1 已完成，提交 `205b4c1`）
- 范围：**只做步骤 2**（运行时读取），不扩展到生产状态机制，不做步骤 3 的缓存与性能优化

## 1. 四个子任务

| # | 内容 | 验收 |
|---|---|---|
| 1 | 字符串绑定端到端 | 真实执行 `Layout`，`vm.Title` 的值进入渲染路径 |
| 2 | 路径与类型 | `{User.Name}`、`{Owner.Name}`、`{Display}` 行为符合契约；不支持的明确拒绝 |
| 3 | 改值后显式重新布局 | 观察到新值；**测试注释声明不证明生产自动刷新** |
| 4 | 失败路径与提交边界 | 区分 Analyzer 拒绝 / Codegen 失败 / 编译失败 / 运行时读取失败 |

## 2. 开工前必须明确的契约判断：运行时读取失败

### 2.1 事实：Go 生成代码中**不存在**运行时读取失败分支

绑定生成为**静态类型的字段访问**（`vm.Title`、`vm.User.Name`）。
Analyzer 已在编译期完成路径解析、类型校验与可访问性检查，
未通过的绑定**根本不会进入 Codegen**。

因此：

- 路径不存在 / 字段未导出 / 类型不匹配 → **Analyzer 拒绝**（BIND-001~005），无生成产物；
- 能生成的代码，其字段访问**必然编译通过**；
- 不存在「运行时读取失败」这一分支。

### 2.2 唯一可能失败的运行时情形

**`vm` 为 nil 指针** → Go 会 panic。

契约当前**未定义**该行为。按「不临时选择默认策略」的要求：

> **标记为待决契约项**，不在步骤 2 擅自决定（不默认返回空串、不忽略、不自定义 panic）。

步骤 2 只记录该事实与可选方案，交由契约决策后再实现。

### 2.3 待决项（记录，不擅自决定）

| 情形 | 可选方案 | 状态 |
|---|---|---|
| `vm == nil` | ① panic（暴露调用方缺陷）② 渲染空 ③ 返回错误 | **待决** |

## 3. 三个验收重点

### 3.1 测试必须真正执行生成代码

证据链：

```text
生成代码 → 编译 → 构造 ViewModel → 执行 Layout → 观察渲染结果
```

`TestBindingGeneratedCompiles` 只证明可编译，**不等于运行时绑定已验证**。
不得仅凭源码字符串匹配声称运行时正确。

### 3.2 保持静态缓存回归保护

必须保留：

- `TestBindingMustNotFreezeDynamic`（绑定页面不被固化）
- `TestStaticPageStillFreezes`（静态页面仍固化）

不得为让绑定测试通过而关闭静态缓存。

## 4. 验收清单（8 项）

| # | 项 |
|---|---|
| 1 | 字符串绑定在真实 Layout 中读取正确值 |
| 2 | 多段路径、指针、零参方法运行行为符合契约 |
| 3 | 改 vm 值后显式重新 Layout 观察到新值 |
| 4 | 测试注明不验证生产自动刷新 |
| 5 | 非法绑定仍由 Analyzer 拦截 |
| 6 | 失败路径与部分提交符合契约 |
| 7 | 静态缓存对照测试均通过 |
| 8 | gofmt/build/vet/test/diagdoc/docgate 全通过 |

## 4.1 实施结果（2026-10-19）

### 验收清单 8/8

| # | 项 | 证据 |
|---|---|---|
| 1 | 字符串绑定读取正确值 | `TestBindingRuntimeReadsValue`：短值/长值像素数不同 |
| 2 | 多段/指针/零参方法 | `TestBindingRuntimePathKinds`（3 子测试全过） |
| 3 | 改值后重新布局观察到新值 | `TestBindingRuntimeValueChange`：改前≠改后，同值两次一致 |
| 4 | 注明不验证生产自动刷新 | 三处测试注释显式声明 |
| 5 | 非法绑定仍被 Analyzer 拦截 | `TestFailureClassA_AnalyzerRejects`（4 例，含 BIND-001/002/003/005） |
| 6 | 失败路径与部分提交 | `TestFailureClassB`：失败时返回空源码 |
| 7 | 静态缓存对照 | `TestBindingMustNotFreezeDynamic` + `TestStaticPageStillFreezes` |
| 8 | 全部门禁 | gofmt/build/vet/test/diagdoc/docgate 全绿 |

### 观测手段（不靠源码字符串匹配）

用 headless 离屏渲染 + 非零像素计数。已实测：
`"AAAA"` → 280 像素；`"WWWWWWWWWWWW"` → 1299 像素。

因此「改值后像素变化」可证明新值进入渲染路径。

### 一个真实缺陷（步骤 2 抓出来的）

**`{Display}` 生成了 `vm.Display`（函数值）而非 `vm.Display()`，编译失败。**

步骤 1 的编译测试只用了 `{Title}`，未覆盖方法段，所以漏掉了。

修复：`PathStep` 新增 `IsMethod`，Analyzer 在 `stepType` 判定为 `*types.Func`
时置位；Codegen 据此生成 `()`。

回归保护：
- `TestBindMethodAsSegment`（Analyzer 层断言 `IsMethod=true`）
- `TestBindFieldNotMarkedAsMethod`（对照：字段不得误标）
- `TestBindingMethodNotCallableAsName`（Codegen 层断言不生成函数值）
- `TestBindingRuntimePathKinds/零参方法`（运行时层真实编译+渲染）

### 同时清理的死代码

`binding.go` 中 `bindingTextExpr` 与 `valueRef` 从未被调用（`nodeExpr` 直接用了
`lookupAttr`）。删除后 codegen 覆盖率从 82.4% → 90.2%。

**没有为了覆盖率保留死代码。**

### 运行时读取失败：待决契约项

已核对：绑定是**静态类型字段访问**，Analyzer 在编译期完成路径/类型/可访问性检查，
未通过的绑定进不了 Codegen。因此生成代码中**不存在运行时读取失败分支**。

唯一可能失败的是 `vm == nil`（Go panic）。契约未定义该行为，
按「不擅自选择默认策略」记录为**待决**，本任务未实现任何策略。
`TestFailureClassD_RuntimeReadFailureUndefined` 锁定该事实。

### `INV-PERF-4` 当前进展

| 维度 | 状态 |
|---|---|
| 值读取正确 | ✅ 已验证（运行时） |
| 值变化后正确更新 | ✅ 已验证（测试显式重新布局） |
| 不得为省分配而错误复用旧文本 | ✅ 已验证（改值后像素变化） |
| 稳定帧零分配 | ❌ **未验证**（步骤 3） |
| shaping 不重复 | ❌ **未验证**（步骤 3，且 shaping 无法直接计数） |

## 5. 退出条件

- 端到端运行测试及实际结果
- 路径、类型与失败行为测试矩阵
- 「测试显式重新布局，不代表生产自动刷新」的证据
- `INV-PERF-4` 当前进展：已验证的正确性场景 + 仍未验证的缓存/shaping 指标
- 提交号与完整门禁结果

**本轮不要求零分配，不要求证明 shaping 已缓存。**
