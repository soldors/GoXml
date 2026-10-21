# TASK-FONT-001-D-IMPL-2：帧内切换与布局一致性（接口已冻结，实现未开始）

## 元信息

- 状态：**接口与切换时机已冻结，实现未开始**（2026-10-21 冻结）
- 上级：`TASK-FONT-001-D`（§12 实现边界冻结）
- 前置：`TASK-FONT-001-D-IMPL-1`（提供 `Lazy.Take()` / `Lazy.Theme()` / `Lazy.Report()`）
- 交付物：`pkg/runtime/app` 的懒加载接线 + 测量钩子 + 测试
- 验收命令见 §6（工具限制：真实窗口在无图形会话时不出帧，故**布局/呈现类断言
  允许 Skip，但必须打印帧数**；纯逻辑断言不得 Skip）

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

- `NewLazyApp` 内部调用 **一次** `lz.Request()`（首次加载的唯一触发点），
  且**不等待**它完成。
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
  ├─ view.Layout(gtx, th)                        // ③ 首次排版计时在此区间
  ├─ if swapped { firstLayoutAfterSwap = now()-layoutStart }  // ④ 只记第一次
  ├─ e.Frame(gtx.Ops)                            // ⑤ 提交呈现
  └─ if swapped { lz.MarkDisplayed(now()) }      // ⑥ 呈现之后才算"已显示"（R1）
```

四条不许动的理由：

1. **① 必须在 `FrameEvent` 内**：`th.Shaper` 非并发安全，替换与渲染必须在同一 goroutine；
   跨 goroutine 共用 shaper 的后果是 fatal error（D 卡 §4.3.2）。
2. **② 只换 `Shaper`**：同时改 `Palette`/字号等会让"布局差"混入无关变量，
   D6-6 的度量差就不可归因了。
3. **⑤ 之后再记 `DisplayedAt`**：`e.Frame` 之前只是"生成了绘制指令"，
   提交返回后才算呈现（R1：加载完成 ≠ 已显示）。
4. **⑥ 的一次性**：`DisplayedAt` 只记**首次**，后续帧不得覆盖——
   否则"显示时刻"会随每帧漂移，变成一个没有意义的字段。

### 1.3 `Lazy` 需要补的两个方法（IMPL-1 的追加，向后兼容）

```go
// MarkDisplayed 由渲染线程在真主题**首次提交呈现之后**调用；只记第一次。
func (l *Lazy) MarkDisplayed(at time.Time)

// MarkFirstLayout 记录切换后首次排版耗时；只记第一次。
func (l *Lazy) MarkFirstLayout(d time.Duration)
```

两个方法都**幂等且只生效一次**（第二次调用被忽略），这样"只记首次"的语义
由类型本身保证，而不是靠每个调用点自觉。

## 2. 三条规则在本卡的落地

### R1：加载完成 ≠ 已显示

- `Report().LoadedAt`（IMPL-1 产出）与 `DisplayedAt`（本卡产出）**两个字段分开**；
- 断言 `DisplayedAt >= LoadedAt`，且**必须存在一个用例让二者明显不等**
  （注入一个延迟完成或延迟取走的场景），证明字段不是抄同一个值；
- 禁止把 `DisplayedAt - LoadedAt` 写进"字体解析耗时"。

### R2：后台不得查询渲染 shaper

本卡的职责是**保证切换只发生在帧内**。可测锚点：

- 在 `OnReady` 回调里**不换 shaper**（只 `Invalidate`）；
- 用一个"帧内标记"（`atomic` 递增的 frame 序号 + 渲染 goroutine 标记）
  断言 `Take()` 只在该标记为真的区间被调用；
- 若 `Take()` 在帧外被调用，测试直接失败（不是靠崩溃来发现）。

### R3：四个指标独立

本卡负责其中两个：**切换后首次排版耗时**、**布局度量差**。
另外两个（首帧预算、后台解析耗时）分别归 ACCEPT 与 IMPL-1。
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
| `TestFontLazyLoadDisplayedAtAfterLoaded` | `DisplayedAt >= LoadedAt`，且用例中二者明显不等 | R1 |
| `TestFontLazyLoadFirstLayoutRecorded` | 切换后首帧记录了排版耗时且 > 0；后续帧不覆盖 | 一次性语义 |
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
2. `th.Shaper` 的替换点**全仓库只有一处**（`pkg/runtime/app` 的帧内分支），
   用 `grep -rn "\.Shaper *=" --include=*.go` 自查并在报告里贴结果；
3. 既有 `pkg/runtime/app` 测试全绿，`Run` / `RunWith` / `RunWindow*` 行为不变
   （它们不经过 `Lazy`，不得被顺手改掉）；
4. 布局 delta 的实际数值记录在案（换字体/换平台后可复核）。

## 8. 明确不做

- 不改 `theme.NewTheme()`；不改 `NewApp` 的签名；
- 不做失败诊断与严格失败语义（IMPL-3）；
- 不做首帧预算的最终定阈值（ACCEPT，需实测后再定，D 卡 §9-1 禁止先拍数字）；
- 不碰 Linux（独立待办）。
