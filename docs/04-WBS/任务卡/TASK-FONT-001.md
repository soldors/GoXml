# TASK-FONT-001：实现 CJK 字体探测、嵌入兜底、fallback 链与 IME 验收

## 元信息
- 状态：**进行中（A、B、C 已完成；D 未开始）** | 优先级：P0 | 预计：8h
- 子卡进度（2026-10-21 实测，见 §3）：A ✅ / B ✅ / C ✅ / D ⬜ —— 本卡**尚未完成**（D 与跨平台验证仍缺）
- 分支：feature/TASK-FONT-001
- 依赖：`TASK-THEME-SEAM-001`（接口与包边界）✅ **已完成**｜**不再是 `TASK-STYLE-001`**
  - 审计结论（`docs/05-开发/STYLE-FONT依赖审计.md`）：`<Def>/<Slot>` 宏展开与字体无代码耦合，
    且已实现于 `internal/compiler/expander/expand_def.go`；真依赖只有「主题/FontConfig/主题构建入口」，
    已归入 `TASK-THEME-SEAM-001`。
- 规范依据：`docs/03-架构/字体与文本渲染策略.md`（v1 冻结，§6.2 已按 ADR-0012 修订）

## 1. 背景与上下文

**这是 WBS 此前完全缺失的工作项。** 事实需要分两层，混在一起会得出错误结论：

**（1）显式使用 `font/gofont` 集合时，不含任何 CJK 字形。** 实测：

```text
U+0041 'A'  -> glyphIndex=36   （存在）
U+4E2D '中' -> glyphIndex=0    （缺失）
U+FF0C '，' -> glyphIndex=0    （缺失）
```

`glyphIndex == 0` 表示字形不存在。

**（2）但 `material.NewTheme()` 用的是零值 `&text.Shaper{}`，默认会加载系统字体。**
`text.NoSystemFonts()` 的注释是「用于**关闭**系统字体加载」，即零值是**开启**。
因此在装有中文字体的机器上（例如本机 Windows 11），中文界面**能显示**，
走的是系统字体隐式回退，而不是框架保证。

2026-10-20 实测（由维护会话在本仓库环境执行，**尚未在 CI 或其它平台复现**）：

| 观测 | 数据 | 说明 |
|---|---|---|
| `"ABCD"` 离屏渲染非透明像素 | 316 | 基线 |
| `"中文测试"` 离屏渲染非透明像素 | 548 | 有字形（非全空白） |
| `"中"` 与必缺失字符（U+1F600）位图差异 | 200 / 12800 字节 | 位图不同 → 「中」有独立字形，不是 notdef 方块 |

环境：Windows 11 / amd64、Go 1.26.1、Gio v0.10.3、`CGO_ENABLED=0`、headless 离屏截图。

**结论修正**：不能再说「任何含中文的界面在不额外配置字体时都会静默渲染为空白或豆腐块」——
在**有系统 CJK 字体**的环境里不会。真正的风险是**环境依赖不可控**：
Docker 基础镜像、精简 Linux、未安装中文字体的目标机，以及跨平台分发时，
同一份程序会静默变成空白/豆腐块且不报错。对中文开发者使用的框架而言，这仍是交付阻断级问题，
但它是一个**确定性问题**，不是「本机也看不见」的问题。

## 2. 目标

1. 中文界面在 Windows / macOS / Ubuntu 默认环境下正确渲染。
2. 系统无 CJK 字体时启用嵌入兜底并给出诊断。
3. 中英混排与 emoji fallback 正确。
4. 中文 IME 输入可用，候选框跟随光标，组合文本显示正确。
5. 字体加载不阻塞首帧，不破坏冷启动 ≤ 50ms。
6. 文本 shaping 结果可缓存，稳定帧文本节点 `allocs/op == 0`。

## 3. Subtask 清单与验收命令

按 2026-10-20 裁决拆成四张独立卡，顺序推进；IME **已移出本卡**（见 §3.1）。

| 子任务ID | 内容 | 验收命令 | 前置 |
|---|---|---|---|
| TASK-FONT-001-A | 各平台 CJK 系统字体探测（目录、超时、诊断、解析失败继续下一候选）+ `FontProvider` 单元测试 | `go test -run TestFontProbe ./pkg/runtime/theme/...` | `TASK-THEME-SEAM-001-5`（✅ 包已存在）；边界与验收矩阵见 [TASK-FONT-001-A.md](TASK-FONT-001-A.md) **→ ✅ 已实现**（`c4ec28b`，8 个用例） |
| TASK-FONT-001-B | 嵌入兜底：`//go:embed` + **构建期**子集化（GB2312 6,763 字，实测 1.55 MB）+ 增量 ≤3MB 断言 | `go test -run TestEmbeddedFallback ./pkg/runtime/theme/...` | A（✅）；边界与验收矩阵见 [TASK-FONT-001-B.md](TASK-FONT-001-B.md) **→ ✅ 已实现**（`9b81748`，6 个用例；两个开工阻塞项均已解除，见卡 §7） |
| TASK-FONT-001-C | 按字形覆盖的 fallback 链（中英混排、emoji、系统与嵌入的实际选择） | `go test -run TestFontFallbackChain ./pkg/runtime/theme/...` | A + B（**均已满足**）；边界与验收矩阵见 [TASK-FONT-001-C.md](TASK-FONT-001-C.md) **→ ✅ 已实现**（10 个用例全 PASS；覆盖判定单次 11.86 µs；5 次变异全部如期变红） |
| TASK-FONT-001-D | 懒加载与加载失败诊断（不阻塞首帧、不静默空白） | `go test -run TestFontLazyLoad ./pkg/runtime/theme/...` | A / B（均满足）；**设计已冻结，实现未开始**，见 [TASK-FONT-001-D.md](TASK-FONT-001-D.md)。动因：系统探测 ~97–109 ms 超 20 ms 预算，**D 解决延迟，B 不解决**。占位用例仍是 `t.Skip` |

> 顺序裁决（2026-10-20）：**先做 A，不并行开 B/C/D**。理由见
> [TASK-FONT-001-A.md](TASK-FONT-001-A.md) §1.3——B 需要 A 定出的「系统字体缺失」边界，
> C 依赖可靠的字体解析与加载基础，D 过早会把字体发现/加载/shaping 混成难以验收的任务，
> 且 A 是 `Resolve(family string) (string, error)` 这个冻结接口的第一次实测。

> ⚠️ 曾经的空转状态（2026-10-20 前）：`pkg/runtime/theme` 不存在时，四条命令都匹配
> 0 个包却返回 ok。按纪律不能把「命令返回 ok」当成通过。
>
> ✅ 2026-10-20 `TASK-THEME-SEAM-001-5/-6` 之后：包已建立，且 A..D 各有一个**同名
> 显式 `t.Skip` 占位用例**（`pkg/runtime/theme/font_pending_test.go`）。因此四条命令
> 会真实命中测试、输出 `--- SKIP: ...`，「未执行」在日志里一眼可见，**不会**被误读为通过。
> `internal/docgate` 的 `TestFont001AcceptanceCommandsAreNotVacuous` 负责阻止它们重新退化为
> 空转（包路径不存在、或 `-run` 匹配不到任何测试，都会变红）。
>
> 接线纪律：实现某一项时把对应 `t.Skip` 换成真实断言，**函数名保持不变**；
> 若必须改名，同步改本表，否则门禁会红。

### 3.0 进度实测（2026-10-21，在远端 `9b81748` 上重跑）

| 子任务 | 验收命令 | 退出码 | 命中 | 备注 |
|---|---|---|---|---|
| A | `go test -run TestFontProbe ./pkg/runtime/theme/...` | 0 | **8 个 PASS** | 占位用例已替换为真实断言（`font_test.go`） |
| B | `go test -run TestEmbeddedFallback ./pkg/runtime/theme/...` | 0 | **6 个 PASS** | 占位用例已替换为真实断言（`embedded_fallback_test.go`） |
| C | `go test -run TestFontFallbackChain ./pkg/runtime/theme/` | 0 | **10 个 PASS** | 2026-10-21 实现完成，占位用例已删除（`fallback_test.go`） |
| D | `go test -run TestFontLazyLoad ./pkg/runtime/theme/` | 0 | **1 个 SKIP** | 仍是占位，**未实现、未执行** |

⚠️ SKIP 的退出码同样是 0——**不能**据此认为 D 已通过。SKIP 计为未执行。

**C 完成后新增的实测事实**（详见 [TASK-FONT-001-C.md](TASK-FONT-001-C.md) §1）：

1. Gio v0.10.3 **已内建** 5 级逐字形 fallback，最后一级会静默返回「任意 face」
   → 缺字形变成 notdef 且不报错。C **不重建**它，只做观测与准入。
2. `Glyph.ID` 可反解出 faceIdx 与 gid；`gid == 0 ⟺` 无字形，
   受控场景下与 `NominalGlyph` **146/146 一致**。
3. collection 顺序 = 字形级优先级；前一个缺字形才跨到下一个（指纹实测坐实）。
4. **本机嵌入相对系统零增量**：charset.txt 7,540 个码位全部被 msyh 覆盖。
   → 补充链因此设为**按需**触发，覆盖完整时不加载嵌入（B4 约束保持有效）。

**已完成 / 未完成（边界写清楚，避免"字体工作已完成"的误读）**：

- ✅ A：系统字体探测（候选表、超时、诊断、缓存、`Resolve` 语义、Shaper 装配）。
  完成于 `c4ec28b`。**实测范围仅 Windows**；macOS / Linux 真机探测仍**待验证**。
- ✅ B：嵌入兜底（官方 Noto Sans SC 2.004 → 构建期子集化 → `//go:embed`）、
  系统优先的选择顺序、二进制增量实测、失败语义收紧。完成于 `9b81748`。
  子集是 **GB2312 6,763 汉字 + ASCII**，**不是完整 CJK**：清单外码位无字形，
  属已知边界而非缺陷。
- ✅ C：按字形覆盖的 fallback 链（2026-10-21 完成）：GlyphID 反解与 notdef
  可观测、覆盖判定作为候选准入（挡住「能解析但没汉字」）、按需字形级补充链、
  emoji 实测降级。**未解决**：emoji 仍无来源（只报告不补）；
  补充链的真实收益无法在本机证明（本机零增量），待跨平台验证。
- ⬜ D：懒加载与不阻塞首帧；A 的 Windows 实测首次系统探测约 **110 ms**，
  已超规范 §3.2 的 20 ms 预算，这是 D 的核心动因。

### 3.3 C 之后仍需跨平台的验证（架构评审 2026-10-21 点名）

1. **`Glyph.ID` 位布局是 Gio 实现细节。** 146 个码位当前交叉校验一致，
   **不能**证明未来 Gio 版本会保持该布局。
   → **升级 Gio 时必须把解码器交叉校验列为强制回归门禁**，并在升级前主动读
   `text/shaper.go` 的 `newGlyphID` 确认布局未变。门禁失败不许靠放宽断言变绿。
2. **本机零增量不是补充链无用的证据。** C 的「仅嵌入有 0」只说明
   **本机 Windows 系统字体已覆盖嵌入子集**；Linux 精简容器、缺 CJK 字体的机器
   仍可能需要嵌入提供增量覆盖。→ 补充链的**收益**论证未完成，只完成了机制。
   **必须在本机之外的环境补一次「嵌入确实提供增量」的实测**（见 §8 验证顺序）。

1. **B 解决的是"字体缺失时的兜底"，不是"任意字符都有字形"。**
   B 的选择粒度是**字体族**：系统命中就用系统、全失败才用嵌入。
   嵌入子集只有 GB2312 覆盖，因此即使在"嵌入兜底生效"的机器上，
   清单外字符（罕用汉字、扩展区、emoji）仍然可能无字形。
2. **emoji 目前没有任何来源。** 系统字体与嵌入子集都不保证含 emoji 字形；
   C 必须实测"emoji 有没有字形"并给出**明确降级行为**，而不是假设 Gio 会兜底。

### 3.1 IME 已移出本卡

| 原 | 现 |
|---|---|
| `TASK-FONT-001-5` 中文 IME 输入、候选框定位与组合文本验收（`./pkg/widgets/...`） | 归入 `TASK-WIDGET-001` / `INPUT-001` |

理由：规范 §8 本就把「`<Input>` 必须通过 IME 验收」写在 `TASK-WIDGET-001` 上；
IME 的验收路径 `pkg/widgets` 与 `<Input>` 控件本身都**不存在**，留在字体卡里等于让
字体工作等一个更重且无关的前置。字体侧只保留"为 IME 提供字体与降级说明"的责任。

## 4. 实现要求

### 4.1 字体来源

按 `字体与文本渲染策略.md` §2 实现「系统字体优先 + 嵌入兜底」双轨：

- Windows：`msyh.ttc` → `simsun.ttc` → `simhei.ttf`
- macOS：`PingFang.ttc` → `STHeiti Light.ttc` → `Hiragino Sans GB.ttc`
- Linux：`NotoSansCJK-Regular.ttc` → `wqy-microhei.ttc` → `uming.ttc`

规则：

- 探测必须有超时，不得因文件系统缓慢阻塞启动。
- 不得通过执行 `fc-match`/`zenity` 等外部命令作为实现路径（只可作诊断）。
- 文件存在但解析失败时继续尝试下一候选并记录原因。

### 4.2 嵌入兜底

- 必须做**子集化**（GB2312 常用字约 6,763 字），不得整包嵌入。
- 二进制增量 ≤ 3MB。
- 子集化在**构建期**完成，不在运行时做。

### 4.3 懒加载

- 禁止在 `init` 或 `main` 同步加载字体。
- 首次需要 shaping 时加载，或异步加载后 `Invalidate`。

### 4.4 IME

- 必须实测中文输入（拼音输入法），不得假设可用。
- 若某平台存在已知缺陷，必须在 `平台与平台依赖矩阵.md` 能力矩阵中标注并给出用户可见降级说明。

## 5. 代码边界

| 文件/模块 | 责任 |
|---|---|
| `pkg/runtime/theme/theme.go` | 主题构建入口 `NewTheme()` / `NewThemeWithReport()` / `Diagnostics()`（**已创建**；`pkg/runtime/app` 的默认来源）。宽容入口 vs 严格入口的边界写在包注释里 |
| `pkg/runtime/theme/font.go` | 字体探测（A）、嵌入兜底编排与两条链路诊断（B）（**已创建**） |
| `pkg/runtime/theme/embedded/` | 嵌入子集字体、`charset.txt`、OFL 全文、`METADATA.json`、`//go:embed`（**已创建**，B）。原名设想的 `font_embed.go` 未采用——改为独立子包，边界更清晰 |
| `pkg/runtime/theme/font_pending_test.go` | C / D 的显式 skip 占位用例（A、B 的占位已删除，换成真实断言） |
| `pkg/runtime/theme/testdata/sizeprobe/` | B5 的二进制增量测量靶子（`testdata` 不参与 `./...`）。⚠️ `examples/demo` 不链接 theme 包，不能用来测增量 |
| `scripts/font/subset.py` | 构建期子集化（B）。运行时**绝不**做子集化 |
| `pkg/platform/font/` | 平台字体目录路径（同文件内按 GOOS 分表，未用 build tag——便于非宿主机候选表可测） |
| `contracts/runtime_api.go` | `FontProvider` 契约（**已冻结并已接线**：`theme.Provider` 实现它） |

生成代码**不得**包含字体路径、字号常量或 `text.Shaper` 构建逻辑，
且**不得引用任何字体字段**（原 §6.2 的 `th.Fonts.Family` 要求已按 ADR-0012 删除）；
字体选择由 `pkg/runtime/theme` 在运行时主题构建过程中完成，
由 `pkg/runtime/app` 把配好字体的 `*material.Theme` 交给视图。

## 6. 验收要求

整体验收在 **C、D 完成前不成立**——下面逐条标注了当前状态，不把子集达标记成整体达标。

| # | 要求 | 状态 |
|---|---|---|
| 1 | 中文界面三平台默认环境正确渲染，无空白/豆腐块 | ⬜ **待验证**：仅 Windows 实测；macOS / Linux 真机未验证 |
| 2 | 系统无 CJK 字体时嵌入兜底生效并有诊断 | ✅ B（`9b81748`）：系统全失败→嵌入，诊断含两条链路 |
| 3 | 字体损坏/缺失产生明确错误，不静默渲染空白 | ✅ B：两条链路都失败 → `ErrFontNotFound` + `app.Run` 返回错误 |
| 4 | 中文 IME 可用，候选框跟随光标，组合文本正确 | ⬜ **已移出本卡**，归 `TASK-WIDGET-001` / `INPUT-001` |
| 5 | 中英混排与 emoji fallback 正确 | ⚠️ **部分**：C 已实测中英混排无缺字形（✅）；
  **emoji 仍无来源**——嵌入子集永不含 emoji，系统场景有字形但不保证，
  C 只能报告缺失不能补（待新任务） |
| 6 | 稳定帧文本节点 `allocs/op == 0` | ⬜ 未验收 |
| 7 | 字体加载不阻塞首帧 | ⬜ **归 D**；设计已冻结（`TASK-FONT-001-D.md`），实现未开始。
  D1 基线实测（Windows）：探测 97–109 ms、shaper 冷启 38.5 ms、首次 shaping 41–53 ms。
  **20 ms 探测预算与首帧预算已明确分开计量** |
| 8 | 嵌入增量 ≤ 3MB | ✅ B：实测 2,182,144 B ≤ 3,145,728 B |

## 7. 已知风险

- **Windows IME**：Gio 在 Windows 的 IME 支持存在历史边界问题（候选窗位置、组合事件时序），必须显式验证而非假设。
- **Linux 容器环境**：精简镜像常无 CJK 字体，嵌入兜底是唯一可靠路径。
- **体积权衡**：嵌入字体会让纯英文应用也背上 3MB，因此系统优先策略不可省略。

## 8. 交接记录

| 项目 | 内容 |
|---|---|
| **评审基线（2026-10-21 确立）** | **远端 `master` = `2dbe874`**。
  后续评审以此为准，**不将文档状态当作测试执行证据** |
| 当前提交 SHA | `2dbe874`；A `c4ec28b`、B `9b81748`、C `5aea209`、D 卡冻结 `49d18c5`（见 §8.1） |
| 已完成子任务 | **A 系统探测**（`c4ec28b`，8 个用例，Windows 实测命中 `msyh.ttc`）；
  **B 嵌入兜底**（`9b81748`，6 个用例，二进制增量 2,182,144 B ≤ 3 MiB）；
  **C 按字形覆盖的 fallback 链**（`5aea209`，10 个用例全 PASS、5 次变异全红） |
| 未完成子任务 | **D 懒加载**（设计已冻结，实现未开始；占位用例仍为显式 `t.Skip`）。
  **跨平台验证**：① macOS / Linux 真机探测未验证；
  ② **Linux 无 CJK 字体环境验证嵌入补充链确实提供增量覆盖**（C 卡 §7-6，评审点名）；
  ③ emoji 无来源 |
| 三条已确认结论（评审 2026-10-21，基线 `2dbe874`） | ① **C 已完成子任务验收**，但存在跨平台待验证项：
  `Glyph.ID` 位布局依赖必须保留为 Gio 升级门禁；Windows 上补充链零增量
  **不能**推导出其他环境也没有收益。
  ② **D 的性能基线已建立，但真实首帧尚未测量**：系统字体探测 97–109 ms、
  首次 shaping 41–54 ms。这些数据说明需要懒加载，**不能直接充当真实首帧的验收结果**。
  ③ **下一步只做 Gio 生命周期独立验证**：帧内替换 `th.Shaper`、
  `Window.Invalidate()`、连续帧绘制，再决定是否进入 D 的实现 |
| 本轮（基线后）未做的事 | 未重复 D1 测量、未实现 D、未启动其他字体功能。
  仅冻结 D 卡 §3 的两处待选项 |
| 验证顺序（评审 2026-10-21） | 1) ✅ **已完成**（2026-10-21）独立小任务验证 Gio shaper 生命周期：
  帧间替换 `th.Shaper` 安全且生效、可回退；跨 goroutine 共用 shaper **必现崩溃**
  （6/6，fatal/panic，崩点分散于 `bidi` 与 `fontscan`；**边界**：只覆盖本机 +
  v0.10.3 + 该调用方式，不据此断言 Gio 实现性质）。详见 D 卡 §4.3。
  2) ✅ **已完成**（2026-10-21）真实窗口 `Window.Invalidate()` 与 `FrameEvent`
  时序：由 `TASK-FONT-001-D-PROBE` 闭环，5 phase × 4 次全 PASS；
  唤醒可靠、合并允许、最后一次更新必被渲染；帧在途时净等待 75.7–105.2 ms。
  **未覆盖**：窗口最小化状态、macOS/X11。详见 D 卡 §4.3.5。
  3) **实现懒加载已拆为四张卡**（D 卡 §12）：`D-IMPL-1` 状态机 →
  `D-IMPL-2` 帧内切换 → `D-IMPL-3` 诊断与失败语义 → `D-ACCEPT` 整体验收；
  接口与验收矩阵已冻结，**四张卡均未开工**，不得跳卡并行；
  4) Windows 完整回归；5) **Linux 无 CJK 环境验证嵌入增量**（独立待办，
  不并入 D 的任何子卡）；6) 更新父卡与 WBS |
| 已运行命令及结果 | 2026-10-21 在 `9b81748` 上重跑四条验收命令，见 §3.0：
  A=8 PASS、B=6 PASS、C=1 SKIP、D=1 SKIP（**SKIP 计未执行**）；
  `go build ./...` 0、`go vet ./...` 0、`gofmt -l` 空、
  `go test ./contracts/ ./pkg/... ./internal/docgate/` 全 ok |
| 已知失败或风险 | ① `FontProvider.Resolve` 返回 `string` 够用——A 卡 §4.3 已给书面结论
  （v1 够用；限制是不含 Style/Weight，需扩展时走 ADR，不许在实现里暗改）。
  ② 占位用例改名必须与本表验收命令同步，否则 docgate 报红。
  ③ 背景里的中文渲染实测为本机 Windows 观测，**未在 CI 或其它平台复现**。
  ④ B 的子集是 GB2312 覆盖，**不是完整 CJK**——清单外码位无字形是已知边界。
  ⑤ **emoji 无来源**：系统与嵌入都不保证 emoji 字形，C 必须实测并给降级行为。
  ⑥ 系统探测 ~110 ms 超 20 ms 预算，归 D。
  ⑦ `examples/demo` 注入 `material.NewTheme()`，不链接 theme 包——旗舰样例
  看不到嵌入兜底与字体诊断，属 DEMO 线边界，本卡只记录不改 |
| 下一位接手人 | `TASK-FONT-001-D-IMPL-1`：异步加载器与状态机（**接口与并发语义
  已冻结**，见 `TASK-FONT-001-D-IMPL-1.md`）。
  D 的 2a/2b 前置门禁已全部通过（D 卡 §4.3 / §4.3.5），第 4 步已拆为
  IMPL-1 → IMPL-2 → IMPL-3 → ACCEPT 四张卡，**按序推进，不得跳卡**。
  三条验收规则在每张卡都要逐条核对，不要留到 ACCEPT：
  R1 加载完成 ≠ 已提交（`LoadedAt` 与 `FrameSubmittedAt` 两个字段两个写入点；
  后者只是"帧提交返回"的代理指标，**不得**表述为屏幕呈现）；
  R2 异步路径不得查询渲染 shaper（`th.Shaper` 只在帧内换）；
  R3 四个性能指标独立验收（M2 探测段不设通过阈值、M3 为后台总量），
  不得互相替代、不得报"总耗时"。
  四张卡已于 2026-10-21 出**第 2 版**（评审 6 条全采纳：`SetNotify` 后绑定、
  `FrameSubmittedAt` 改名、删不可测的 `ParseElapsed`、补 `LoadReport` 完整类型、
  收窄 Shaper 赋值点口径、冻结临时主题不污染 B4 诊断）。
  另有一处语义冲突已裁决（D 卡 §12.4）：异步化不得让 `app.Run` 的同步严格语义
  静默消失，两者并存，异步侧用 `StrictFontFailure` 开关。
  **跨平台验证应并行推进**：Linux 无 CJK 字体环境下的嵌入增量验证是 C 的
  未竟项，且**不并入 D 的任何子卡**——D 全绿也不代表它已验证 |

### 8.1 提交登记

| 提交 | 内容 |
|---|---|
| `5aea209` | `feat(font)`：TASK-FONT-001-C 实现（GlyphID 反解与 notdef 可观测、覆盖判定准入、按需补充链、emoji 降级），10 个验收用例全 PASS，5 次变异全部如期变红。记录见 dev-log 10-21 §29 |
| `49d18c5` | `docs(font)`：冻结 TASK-FONT-001-D 设计（D1 基线、状态机、三个设计问题）+ 写入评审点名的两条风险。记录见 dev-log 10-21 §30 |
| `2dbe874` | `docs(dev-log)`：登记 `49d18c5`。**确立为后续评审基线** |
