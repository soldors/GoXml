# TASK-FONT-001：实现 CJK 字体探测、嵌入兜底、fallback 链与 IME 验收

## 元信息
- 状态：Todo（**已拆卡**，见 §3；前置 `TASK-THEME-SEAM-001` **已完成**，可开工）| 优先级：P0 | 预计：8h
- 分支：feature/TASK-FONT-001
- 依赖：`TASK-THEME-SEAM-001`（接口与包边界）｜**不再是 `TASK-STYLE-001`**
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
| TASK-FONT-001-A | 各平台 CJK 系统字体探测（目录、超时、诊断、解析失败继续下一候选）+ `FontProvider` 单元测试 | `go test -run TestFontProbe ./pkg/runtime/theme/...` | `TASK-THEME-SEAM-001-5`（✅ 包已存在）；**边界与验收矩阵已冻结**：[TASK-FONT-001-A.md](TASK-FONT-001-A.md) |
| TASK-FONT-001-B | 嵌入兜底：`//go:embed` + **构建期**子集化（GB2312 约 6,763 字）+ 增量 ≤3MB 断言 | `go test -run TestEmbeddedFallback ./pkg/runtime/theme/...` | A |
| TASK-FONT-001-C | 按字形覆盖的 fallback 链（中英混排、emoji） | `go test -run TestFontFallbackChain ./pkg/runtime/theme/...` | A（+ 可选 B） |
| TASK-FONT-001-D | 懒加载与加载失败诊断（不阻塞首帧、不静默空白） | `go test -run TestFontLazyLoad ./pkg/runtime/theme/...` | A / B |

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
| `pkg/runtime/theme/theme.go` | 主题构建入口 `NewTheme()`（**已创建**，当前只包装 Gio 默认主题；`pkg/runtime/app` 的默认来源） |
| `pkg/runtime/theme/font_pending_test.go` | FONT-001-A..D 的显式 skip 占位用例（**已创建**） |
| `pkg/runtime/theme/font.go` | 字体探测、fallback 链、懒加载（**待创建**） |
| `pkg/runtime/theme/font_embed.go` | 嵌入子集与 `//go:embed`（**待创建**） |
| `pkg/runtime/theme` 的主题构建入口 | 把解析结果装进 `text.Shaper`，产出 `*material.Theme`（路线 B，ADR-0012 决策 1） |
| `pkg/platform/font/` | 平台字体目录路径（按 build tags 隔离） |
| `contracts/runtime_api.go` | `FontProvider` 契约（**已冻结**，经 `make contract-lint`） |

生成代码**不得**包含字体路径、字号常量或 `text.Shaper` 构建逻辑，
且**不得引用任何字体字段**（原 §6.2 的 `th.Fonts.Family` 要求已按 ADR-0012 删除）；
字体选择由 `pkg/runtime/theme` 在运行时主题构建过程中完成，
由 `pkg/runtime/app` 把配好字体的 `*material.Theme` 交给视图。

## 6. 验收要求

1. 中文界面三平台默认环境正确渲染，无空白/豆腐块。
2. 系统无 CJK 字体时嵌入兜底生效并有诊断。
3. 字体损坏/缺失产生明确错误，不静默渲染空白。
4. 中文 IME 可用，候选框跟随光标，组合文本正确。
5. 中英混排与 emoji fallback 正确。
6. 稳定帧文本节点 `allocs/op == 0`。
7. 字体加载不阻塞首帧。
8. 嵌入增量 ≤ 3MB。

## 7. 已知风险

- **Windows IME**：Gio 在 Windows 的 IME 支持存在历史边界问题（候选窗位置、组合事件时序），必须显式验证而非假设。
- **Linux 容器环境**：精简镜像常无 CJK 字体，嵌入兜底是唯一可靠路径。
- **体积权衡**：嵌入字体会让纯英文应用也背上 3MB，因此系统优先策略不可省略。

## 8. 交接记录

| 项目 | 内容 |
|---|---|
| 当前提交 SHA | 见 dev-log 2026-10-20 §20 / §21（审计 `b05bb12`、接缝冻结提交） |
| 已完成子任务 | 无（字体实现未开始）；已完成的是**前置**：ADR-0012 决策、`FontProvider` 契约冻结、拆卡，以及 `pkg/runtime/theme` 主题构建入口（THEME-SEAM-001-5）与验收方案清点（-6） |
| 未完成子任务 | A 探测 / B 嵌入子集 / C fallback 链 / D 懒加载（四项均有显式 skip 占位用例） |
| 已运行命令及结果 | 依赖审计：`grep`/`ls`/`Read` 核对调用关系，见 `docs/05-开发/STYLE-FONT依赖审计.md`；四条命令按 §3 表**分别**执行（`go test -run TestFontProbe ./pkg/runtime/theme/...` 等）→ A 为真实断言（已实现），B/C/D 为 `SKIP`（未执行，非通过）；非空转门禁见 `internal/docgate`。⚠️ 曾把四条写成一条 `-run 'A\\|B\\|C\\|D'`——Markdown 转义竖线交给 go test 后是**字面量**，实测 `no tests to run` 且退出码 0（空转报绿），已改回分条写法 |
| 已知失败或风险 | ① `FontProvider.Resolve` 返回 `string` 标识是否够用，要等 A 的实测；**够不够用都必须书面结论**，不够则走 ADR，不许在实现里暗改（见 A 卡 §4.3）。② 占位用例改名必须与本卡验收命令同步，否则 docgate 报红。③ 背景里的中文渲染实测为本机 Windows 观测，未在 CI 或其它平台复现；A 卡 §5 强制区分「实测 / 待验证」 |
| 下一位接手人 | `TASK-FONT-001-A`（前置与边界均已就绪）；**不得**并行开 B/C/D |
