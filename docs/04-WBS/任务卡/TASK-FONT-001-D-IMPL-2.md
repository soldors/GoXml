# TASK-FONT-001-D-IMPL-2：帧内切换与布局一致性（接口已冻结，实现未开始）

## 元信息

- 状态：**接口与切换时机已冻结（第 2 版，按评审意见修正 3 处），实现未开始**
- 上级：`TASK-FONT-001-D`（§12 实现边界冻结）
- 前置：`TASK-FONT-001-D-IMPL-1`（提供 `Lazy.Take()` / `Theme()` / `Report()` / `SetNotify`）
- 交付物：`pkg/runtime/app` 的懒加载接线 + 测量钩子 + 测试
- 验收命令见 §6（工具限制：真实窗口在无图形会话时不出帧，故**布局/呈现类断言
  允许 Skip，但必须打印帧数**；纯逻辑断言不得 Skip）

### 修订记录（第 2 版）

| # | 评审问题 | 处理 |
|---|---|---|
| 2 | `DisplayedAt` 的定义超过了现有证据 | 改名为 **`FrameSubmittedAt`**，语义上界止于 `e.Frame` 返回；禁止表述为"屏幕呈现"，见 §1.4 |
| 2b | 流程 ④ 没有调用 `lz.MarkFirstLayout` | 补入伪代码，并要求用例断言"值确实写回 `Report()`"，见 §1.2 / §4 |
| 5 | "全仓库唯一 Shaper 赋值点"过严，与既有同步路径冲突 | 改为"懒加载切换路径唯一 + 既有 `theme.go:116` 不计入"，见 §7-2 |

## 0. 本卡只做一件事

把 IMPL-1 产出的真主题，在**正确的时机、正确的线程**换上去，并把
"什么时候换的""换了之后排一次版要多久""文本度量差多少"测出来。

**不做**：状态机语义（IMPL-1）、诊断对外暴露与严格失败（IMPL-3）、
首帧预算的最终验收（ACCEPT）。

## 1. 冻结的接线方式

### 1.1 新增入口（不改既有入口的语义）

```go
// pkg/runtime/app

// NewLazyApp 用异步加载主题构造运行句柄。
// 与 NewApp 的关系：NewApp 拿现成的 *material.Theme；本函数拿 *theme.Lazy。
// **不接管** theme.NewTheme() 的默认构建路径（路线 B 边界不变）。
func NewLazyApp(view View, lz *theme.Lazy, cfg Config) (*App, error)
```

- `NewLazyApp` 内部**先 `lz.SetNotify(...)` 再 `lz.Request()`**（顺序与闭包捕获见
  `TASK-FONT-001-D-IMPL-1` §2.3），且**不等待**加载完成。
- `App.Run()` / `Run()` 的行为与语义**不变**；懒加载路径走同一 `loop`，
  只多一个"帧内取待应用主题"的步骤。

### 1.2 帧内替换（唯一允许的替换点）

冻结为如下顺序，**不得调整**：

```text
FrameEvent 到达
  ├─ if next := lz.Take(); next != nil {        // ① 只在帧内、只在渲染线程
  │       th.Shaper = next.Shaper               // ② 只换 Shaper，不动 Palette/其它字段
  │       swapped = true
  │       layoutStart = now()
  │  }
  ├─ gtx := app.NewContext(&ops, e)
  ├─ view.Layout(gtx, th)                        // ③ 排版计时在此区间
  ├─ if swapped && firstSwapFrame {              // ④ 只记第一次
  │       lz.MarkFirstLayout(now()-layoutStart)  //    必须真的写回 Report（评审点名）
  │  }
  ├─ e.Frame(gtx.Ops)                            // ⑤ 提交
  └─ if swapped { lz.MarkFrameSubmitted(now()) } // ⑥ 提交返回后（代理指标，见 §1.4）
```

四条不许动的理由：

1. **① 必须在 `FrameEvent` 内**：`th.Shaper` 非并发安全，替换与渲染必须在同一 goroutine；
   跨 goroutine 共用 shaper 的后果是 fatal error（D 卡 §4.3.2）。
2. **② 只换 `Shaper`**：同时改 `Palette`/字号等会让"布局差"混入无关变量，
   D6-6 的度量差就不可归因了。
3. **⑤ 之后再记时间**：`e.Frame` 之前只是"生成了绘制指令"；提交返回后才算
   这一帧已经交给 Gio 的呈现路径（R1：加载完成 ≠ 已提交）。
4. **⑥ 的一次性**：该时间只记**首次**，后续帧不得覆盖——否则会随每帧漂移，
   变成一个没有意义的字段。

### 1.4 字段语义更正：`DisplayedAt` → `FrameSubmittedAt`（评审阻塞点 2）

第 1 版把 `e.Frame` 返回后的时间命名为 `DisplayedAt`（"实际显示时刻"），
**这超出了现有证据**：

- `e.Frame` 返回只能证明"绘制操作已提交给 Gio 的帧处理路径"；
- 它**不能**证明像素已经出现在屏幕上；
- 而 `TASK-FONT-001-D-PROBE` §8.4 已明确把"**present 耗时与调度延迟的分离**"
  列为未覆盖项，本仓库没有任何手段测到真实呈现时刻。

因此字段**改名为 `FrameSubmittedAt`**，并在类型注释里写死：

> 这是"帧提交完成"的**代理时间**，不是屏幕呈现时间。
> 语义上界止于「`app.FrameEvent.Frame` 已返回」。
> 任何报告/文档**不得**把它表述为"用户已看到新字体"。

理由不只是措辞：`FrameSubmittedAt` 会被 IMPL-3 用于 R1 的"加载完成 ≠ 已提交"
断言。如果字段名声称的是呈现时刻而实际测的是提交时刻，那条断言就成了假证据。

### 1.5 `Lazy` 需要补的两个方法（IMPL-1 的追加，向后兼容）

```go
// MarkFrameSubmitted 由渲染线程在真主题**首次提交返回之后**调用；只记第一次。
// 记录的是提交时刻的代理值，不是屏幕呈现时刻（见 §1.4）。
func (l *Lazy) MarkFrameSubmitted(at time.Time)

// MarkFirstLayout 记录切换后首次排版耗时；只记第一次。
func (l *Lazy) MarkFirstLayout(d time.Duration)
```

两个方法都**幂等且只生效一次**（第二次调用被忽略），这样"只记首次"的语义
由类型本身保证，而不是靠每个调用点自觉。

## 2. 三条规则在本卡的落地

### R1：加载完成 ≠ 已提交（不得说成"已显示"）

- `Report().LoadedAt`（IMPL-1 产出）与 `FrameSubmittedAt`（本卡产出）**两个字段**；
- 断言 `FrameSubmittedAt >= LoadedAt`，且**必须存在一个用例让二者明显不等**
  （注入延迟完成或延迟取走的场景），证明字段不是抄同一个值；
- 禁止把 `FrameSubmittedAt - LoadedAt` 写进任何"耗时"指标——这段是
  `Invalidate` 调度 + 排队，2b 实测在帧在途时可达 75.7–105.2 ms；
- **措辞纪律**：本卡及后续文档一律写"提交"，不写"显示/呈现"。

### R2：后台不得查询渲染 shaper

本卡的职责是**保证切换只发生在帧内**。可测锚点：

- 通知回调（`SetNotify` 绑定的那个）里**不换 shaper**，只 `Invalidate`；
- 用一个"帧内标记"（`atomic` 递增的 frame 序号 + 渲染 goroutine 标记）
  断言 `Take()` 只在该标记为真的区间被调用；
- 若 `Take()` 在帧外被调用，测试直接失败（不是靠崩溃来发现）。

### R3：四个指标独立

本卡负责其中两个：**切换后首次排版耗时**（`FirstLayoutAfterSwap`）、
**布局度量差**。另外两个（M1 首帧预算、M2/M3 后台段）分别归 ACCEPT 与 IMPL-1。
本卡**不得**输出"综合耗时"之类的合数。

## 3. 布局跳动的判据（D6-6，评审点名项）

冻结方法：**同一段文本、同一字号、同一约束下，比较临时字体与真字体的
`layout` 尺寸差**，用**相对差**而非绝对像素：

```text
delta = |size_real.Width - size_temp.Width| / max(size_temp.Width, 1)
```

- 接受阈值：**单行 CJK 文本 `delta <= 2%`**；超过则本卡必须给出
  "修复或说明"，不得只记录数字了事。
- 阈值是**判断不是事实**，理由必须写进代码注释（D 卡 §9-4 要求）。
- 测试要打印 `temp`/`real` 两个宽度与算出的 `delta`，便于换字体后复核。
- **不得**用"像素比对通过"替代度量差：像素相等可能只是渲染位置未变。

## 4. 测试要求（全部以 `TestFontLazyLoad` 开头）

| 用例 | 断言 | 备注 |
|---|---|---|
| `TestFontLazyLoadSwapOnlyInFrame` | `Take()` 只在帧内被调用；帧外调用即失败 | 用帧标记，不靠崩溃 |
| `TestFontLazyLoadSwapOncePerGeneration` | 同一次 Ready 只触发一次 `th.Shaper = ...`；后续帧不再换 | 用计数器 |
| `TestFontLazyLoadFrameSubmittedAfterLoaded` | `FrameSubmittedAt >= LoadedAt`，且用例中二者明显不等 | R1（**不写"显示"**） |
| `TestFontLazyLoadFirstLayoutRecorded` | 切换后首帧记录了排版耗时且 > 0；后续帧不覆盖；**值确实写回 `Report()`** | 评审点名：必须调 `MarkFirstLayout` |
| `TestFontLazyLoadLayoutDelta` | 打印 temp/real 宽度与 delta，断言 `delta <= 2%` 或给出说明 | D6-6 |
| `TestFontLazyLoadNoRepeatShaping` | 切换后连续 N 帧不再产生新的 shaping 冷启动（耗时上界） | D6-3 |
| `TestFontLazyLoadNoFrameCounting` | 唤醒类断言必须用序号/状态，不得数帧 | D 卡 §4.3.5 纪律 |

**环境说明**：涉及真实窗口的用例，无图形会话时 `t.Skip` 并**打印帧数**；
纯逻辑用例（替换次数、时间戳顺序）用注入的假 `Lazy` 驱动，不得 Skip。

## 5. 与既有测试的关系（防重复）

`pkg/runtime/app` 已有 `TestRealWindowStateRefresh` / `TestAppRunDrivesAndInvalidateRefreshes`
覆盖"后台改状态 + Invalidate → 后续帧读到新值"。本卡**不重复**验证唤醒链路，
只验证**主题切换**这条新链路。若被问到"唤醒是否可靠"，答案引用
`TASK-FONT-001-D-PROBE`，不要在本卡重新论证。

## 6. 验收命令

```text
go test -v -run TestFontLazyLoad ./pkg/runtime/app/ ./pkg/runtime/theme/
go test -run TestRealWindow ./pkg/runtime/app/
go vet ./pkg/runtime/app/
```

同样遵守"贴 PASS 行不贴退出码"的规矩：真实窗口用例若 Skip，必须能看到
`--- SKIP` 与帧数打印，且**不得**把 Skip 计入通过。

## 7. 完成判据

1. §4 七个用例 PASS（真实窗口用例允许 Skip，但必须在报告中单列"未自动验证"）；
2. **`th.Shaper` 的赋值点范围**（第 2 版更正，原来的"全仓库只有一处"是错的）：
   既有同步路径 `pkg/runtime/theme/theme.go:116` 的 `th.Shaper = text.NewShaper(...)`
   **本来就存在且必须保留**（路线 B 的同步构建），不计入本卡的切换点。
   本卡只要求：**懒加载切换路径的赋值点唯一，且位于 `pkg/runtime/app` 的帧内分支**。
   自查命令与期望：
   ```text
   grep -rn "\.Shaper *=" --include=*.go pkg/ | grep -v _test.go
   # 期望恰好 2 处：
   #   pkg/runtime/theme/theme.go   —— 既有同步路径（不计入）
   #   pkg/runtime/app/*.go         —— 本卡的帧内切换点（唯一 1 处）
   ```
   出现第 3 处即判失败；把既有那处算成违规同样是判错。
3. 既有 `pkg/runtime/app` 测试全绿，`Run` / `RunWith` / `RunWindow*` 行为不变
   （它们不经过 `Lazy`，不得被顺手改掉）；
4. 布局 delta 的实际数值记录在案（换字体/换平台后可复核）。

## 8. 明确不做

- 不改 `theme.NewTheme()`；不改 `NewApp` 的签名；
- 不做失败诊断与严格失败语义（IMPL-3）；
- 不做首帧预算的最终定阈值（ACCEPT，需实测后再定，D 卡 §9-1 禁止先拍数字）；
- 不碰 Linux（独立待办）。
