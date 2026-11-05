# TASK-INPUT-001-D4-B：`lastApplied` 同步算法

> 2026-11-05 状态阅读提示：下文是2026-10-20的历史实现/自验记录。
> 同步顺序与期望以2026-11-02修订的 [Input 输出契约](../../03-架构/Input输出契约.md) 为准；
> 负责人已批准用户编辑优先、程序化同步静默与归一化追随，不能把本卡旧断言当作当前语义。
> 自动化实现存在不等于D5-F原生窗口/真实焦点/IME人工验收完成，整体验收范围见 [父卡](TASK-INPUT-001.md)。


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

## 2026-11-05 R23-R1 证据资格复核补记（不改历史原文）

> 基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`。逐项可机读字段见交接包
> `raw/c1-evidence-qualification.json`；全文见
> [13 项证据资格复核](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)。
> 本补记只登记证据资格，**不追认历史验收、不改写卡内原有状态与判据**，
> 也不把它计入项目完成率。

| 字段 | 复核结果 |
|---|---|
| 终结论 | **历史记录，不作为现行验收证据**：必须有 2026-11-02 规则下的独立复核才算现行资格；R8 的组合验证提供的是新规则下的证据，不是本卡旧断言的追认 |
| 归类 | 未闭环（旧断言已被批准规则取代） |
| 实现 SHA | 3487289c2ebb528de588087d0d61cfd3654157a5 |
| 验收 SHA / 记录 | 卡 §5 自验；dev-log §21.5 明确并发线提交、内容不由本线背书 |
| 执行方是否独立 | **未证（缺具名执行者/独立会话记录）**；上一版措辞「否（同上）」已撤销。依据：无具名执行者/会话记录；「同上」是引用而非独立证据。本轮标准台账 `-v` 重放另列，E3 可复跑不自动等于独立历史验收。 |
| 命令 / rc / raw 可定位性 | 卡 §5 列 6 个命名测试（含 TestInputSyncRuntimeBehaviors / TestInputSyncWriteBackRuntime）；无逐条 rc；E1：无历史 raw；本轮 `r1c-c1-input-v` 现行重放留 raw |
| 当时范围 | 旧契约 §5 的两条 lastApplied 更新路径与「外部值先应用、SetText < Update < Layout」顺序 |
| 现行适用性 | **被 2026-11-02 负责人批准的用户编辑优先 / 程序化同步静默 / 归一化追随规则取代**；契约 §5.2 已重写，R8 报告记「原候选全包 rc1 / 10 条旧断言红」后才 rc0。6 个测试名多数仍在，但断言内容已改（input_conflict_test.go -53、input_divergence_test.go -142） |
| 仍缺哪一层资格 | 现行语义下的独立复核与逐断言差异清单 |
| 下一动作 | 按 R8 的 `e200ace` + 契约 §5.2 重新界定；保留本卡原文为历史 |

## 2026-11-06 R23-R1 返修 R1 补正（覆盖上一版补记中被撤销的结论）

> 本次返修关闭协调者 F-C1 / F-C2 / F-C3。完整逐项字段见 [13 项证据资格复核（R1 返修补正版）](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md) 与交接包 `repair-r1/raw/closure-matrix.json`。

| 字段 | R1 返修后结论 |
|---|---|
| 单一状态（每项只计一次） | B-未闭环（旧断言已被批准规则取代） |
| 证据层 | E1（历史）+ 本轮 E3 精确名重放 |
| 历史原命令是否有效 | 卡内 6 个名字及运行时用例在 codegen 包真实存在；但卡没有可执行命令，且旧顺序断言已被 R8 取代 |
| 本轮另选命令 | `go test -count=1 -timeout 10m -v -run ^(TestInputSyncPath1Generated\|…\|TestInputSyncWriteBackRuntime)$ ./internal/compiler/codegen`（r1r-replay-d4-sync） |
| 真实用例集合 | `r1r-replay-d4-sync`：7 个顶层 PASS / 0 个子测试 PASS / 0 SKIP / 0 FAIL |
| 能证明的范围 | 本轮 7 个精确名顶层用例全部 PASS；只证明这些用例当前可跑通 |
| 未覆盖的范围 | 旧「SetText < Update < Layout」顺序语义的现行有效性、独立历史验收、真实窗口 |
| 历史执行方独立性 | 未证（缺具名执行者/独立会话记录） |
| 本轮 E3 重放 | r1r-replay-d4-sync 是本轮标准台账 E3 重放，不等于独立历史验收 |

