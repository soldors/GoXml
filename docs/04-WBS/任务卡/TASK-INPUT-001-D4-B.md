# TASK-INPUT-001-D4-B：`lastApplied` 同步算法

## 元信息

- 状态：**Done（2026-10-20）** | 证据：`internal/compiler/codegen/input.go` +
  `input_sync_test.go`（6 项测试全过，含 2 项运行时验证）
- 父卡：[TASK-INPUT-001.md](TASK-INPUT-001.md)（D4 第二切片）
- 前置：D4-A ✅ `02b8593`、契约 §5（冻结）
- 契约依据：[Input输出契约.md](../../03-架构/Input输出契约.md) §5（含新增 §5.3.1）

## 1. 范围

实现契约 §5 的两条 `lastApplied` 更新路径与逐帧顺序。

## 2. 实现前核实（Gio 行为，非伪代码）

### 2.1 关键发现：`Editor.Layout` 内部自己调用 `Update`

【源码】`widget/editor.go` 的 `Layout` 开头：

```go
for { _, ok := e.Update(gtx); if !ok { break } }
```

### 2.2 实验证据（两轮）

| 实验 | 操作 | 结果 | 结论 |
|---|---|---|---|
| A | 只调 `Layout`（不手动 Update），队列放 `key.EditEvent{Text:"abc"}` | 文本变 `"abc"` | **Layout 内部会消费事件** |
| B | 先手动 `Update`（拿到 `widget.ChangeEvent`），再调 `Layout` | 文本仍 `"abc"`，未变 `"abcabc"` | **事件被消费后 Layout 不重复处理** |

### 2.3 对契约 §5.3 的影响

③（Update）与 ④（Layout）在 Gio 中**不可完全分离**，但顺序语义不变：

```text
③ for { ev, ok := ed.Update(gtx); if !ok { break } ... 消费 ... }
④ material.Editor(...).Layout(gtx)   // 队列已空，内部 Update 空转
```

**关键性质**：手动 `Update` 必须先执行——否则事件会被 Layout 内部静默吞掉，
`ValueChanged` 永不触发。已写入契约 **§5.3.1**（含实验证据表）。

## 3. 生成形态（实测）

```go
ed := &v.input0
// 路径 1（外部应用）：判据是 V != A，不是 V != ed.Text()
if cur := v.vm.Title; cur != v.applied0 {
    ed.SetText(cur)
    v.applied0 = cur        // 仅在 SetText 之后
}
for {
    ev, ok := ed.Update(gtx)
    if !ok { break }
    switch ev.(type) {       // 无 Submit 时用 ev（避免 declared and not used）
    case widget.ChangeEvent:
        v.vm.OnNameChanged(ed.Text())
        v.applied0 = v.vm.Title   // 路径 2：重求值 V，不用实参
    }
}
return material.Editor(th, ed, "").Layout(gtx)
```

## 4. 实现中发现并修复的两个真实缺陷

| # | 缺陷 | 后果 | 修复 |
|---|---|---|---|
| 1 | 用 `if v := <expr>; v != v.applied0` | 临时变量遮蔽 View receiver `v`，`v.applied0` 编译失败 | 改用 `cur` |
| 2 | 只有 ChangeEvent 时仍写 `switch e := ev.(type)` | `e declared and not used` → **生成代码编译失败** | 仅 Submit 分支存在时才引入 `e` |

两个都是「生成代码能否编译」级别的问题，靠 `go build` 暴露，字符串断言查不出。

## 5. 验收（6 项）

| 测试 | 证明 |
|---|---|
| TestInputSyncPath1Generated | 路径 1 判据 `cur != applied`；**A 的赋值在 SetText 之后**（顺序断言用索引比较） |
| TestInputSyncPath2Generated | 路径 2 重求值 `v.vm.Title`；**不得**用 `ed.Text()` 当 A |
| TestInputSyncOrderBeforeUpdate | 索引比较证明 `SetText < Update < Layout` |
| TestInputNoValueNoSync | 无 `Value` 绑定 → 不生成 SetText（非受控） |
| TestInputLiteralValueNoSync | 字面量 `Value` 不参与逐帧比较（否则与用户编辑无限冲突） |
| **TestInputSyncRuntimeBehaviors** | 真实生成代码：首帧同步 → 外部改值时同步 → 连续两帧不重置 |
| **TestInputSyncWriteBackRuntime** | 合成 `key.EditEvent` 触发 ChangeEvent → 回写 vm → **下一帧不被重置**（路径 2 生效） |

## 6. 明确的责任边界

- 位置/光标行为**不是本契约承诺**：测试只断言「文本包含用户输入」，
  不断言插入位置（`SetText` 后光标归零会导致插入在开头）
- IME 组合输入未验证（无合成 API，属 D5 人工验收）
- 同帧外部改值 + 用户输入的覆盖行为未做运行时验证（契约 §5.4 已声明后果，
  但自动化构造该场景需要同时精确控制事件队列与 vm 变更时机，留 D4-C）

## 7. 未做（D4-C）

- AC-002-10..19/23 的完整自动化映射
- 同帧冲突的运行时验证
- 与 `<For>`/`<If>` 组合的回归
