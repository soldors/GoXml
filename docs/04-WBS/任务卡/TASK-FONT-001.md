# TASK-FONT-001：实现 CJK 字体探测、嵌入兜底、fallback 链与 IME 验收

## 元信息
- 状态：Todo | 优先级：P0 | 预计：8h
- 分支：feature/TASK-FONT-001
- 依赖：TASK-STYLE-001
- 规范依据：`docs/03-架构/字体与文本渲染策略.md`（v1 冻结）

## 1. 背景与上下文

**这是 WBS 此前完全缺失的工作项。** Gio 自带的 `font/gofont` 只覆盖 Latin / Greek / Cyrillic，不含任何 CJK 字形。实测：

```text
U+0041 'A'  -> glyphIndex=36   （存在）
U+4E2D '中' -> glyphIndex=0    （缺失）
U+FF0C '，' -> glyphIndex=0    （缺失）
```

`glyphIndex == 0` 表示字形不存在。**任何含中文的界面在不额外配置字体时会静默渲染为空白或豆腐块，且不报错。** 对中文开发者使用的框架而言，这是交付阻断级问题。

## 2. 目标

1. 中文界面在 Windows / macOS / Ubuntu 默认环境下正确渲染。
2. 系统无 CJK 字体时启用嵌入兜底并给出诊断。
3. 中英混排与 emoji fallback 正确。
4. 中文 IME 输入可用，候选框跟随光标，组合文本显示正确。
5. 字体加载不阻塞首帧，不破坏冷启动 ≤ 50ms。
6. 文本 shaping 结果可缓存，稳定帧文本节点 `allocs/op == 0`。

## 3. Subtask 清单与验收命令

| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-FONT-001-1 | 实现各平台 CJK 系统字体探测（带超时与诊断） | `go test -v -run TestFontProbe ./pkg/runtime/theme/...` |
| TASK-FONT-001-2 | 实现嵌入兜底字体与 `//go:embed` 子集构建（≤3MB） | `go test -v -run TestEmbeddedFallback ./pkg/runtime/theme/...` |
| TASK-FONT-001-3 | 实现按字形覆盖的 fallback 链与中英混排 | `go test -v -run TestFontFallbackChain ./pkg/runtime/theme/...` |
| TASK-FONT-001-4 | 实现懒加载与加载失败诊断（不得静默空白） | `go test -v -run TestFontLazyLoad ./pkg/runtime/theme/...` |
| TASK-FONT-001-5 | 中文 IME 输入、候选框定位与组合文本验收 | `go test -v -run TestIMEComposition ./pkg/widgets/...` |

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
| `pkg/runtime/theme/font.go` | 字体探测、fallback 链、懒加载 |
| `pkg/runtime/theme/font_embed.go` | 嵌入子集与 `//go:embed` |
| `pkg/platform/font/` | 平台字体目录路径（按 build tags 隔离） |
| `contracts/runtime_api.go` | `FontProvider` 契约（经 `make contract-lint`） |

生成代码**不得**包含字体路径、字号常量或 `text.Shaper` 构建逻辑，只能通过主题引用。

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
| 当前提交 SHA | |
| 已完成子任务 | |
| 未完成子任务 | |
| 已运行命令及结果 | |
| 已知失败或风险 | |
| 下一位接手人 | |
