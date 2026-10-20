# STYLE-001 / FONT-001 依赖审计

> 目的：判断 `TASK-FONT-001` 声明的「依赖 `TASK-STYLE-001`」里，哪些是真依赖、哪些只是规划时的假设。
> 依据：**代码调用关系与接口边界**，不是任务卡文字。
> 日期：2026-10-20 ｜ 审计人：维护会话 ｜ 本轮**未改**任何代码 / 契约 / 规范。

## 0. 结论（先说答案）

1. 声明依赖里只有 **STYLE-001-1（Theme / FontConfig / 主题构建入口）** 是真依赖；
   `<Def>/<Slot>` 两块（STYLE-001-2/-3）与字体**没有任何代码耦合**。
2. 真依赖的**强度取决于一个尚未裁决的设计点**：生成代码里的 `th` 到底是谁——
   Gio 的 `material.Theme` 还是 Goxml 自己的 `Theme`。见 §3 的 A/B 两条路线。
3. 无论走哪条，两块工作都会共用 `pkg/runtime/theme` 这个**尚未存在**的包。
   建议先冻结「`FontProvider` 契约 + 包边界 + 主题构建入口」这个最小接缝（独立立卡），
   之后再拆 FONT 的四个子任务；不要一上来就做跨平台字体实现 + 体积预算 + IME 的大提交。

## 1. 证据一：主题与字体在当前代码里的实际落点

| # | 事实 | 证据 |
|---|---|---|
| 1 | 运行时主题由 `pkg/runtime/app` **硬编码**创建 | `pkg/runtime/app/app.go:82` → `return RunWith(view, material.NewTheme(), cfg)` |
| 2 | 生成代码签名是 `Layout(gtx, th *material.Theme)`（契约 §4.3.1） | `codegen.go:857`、`app.go:24` |
| 3 | 生成代码**没有任何** `Theme.` 引用，`Background` 也完全没实现 | `grep 'Theme\.' / 'Background'` 在 codegen 生产码中**零命中** |
| 4 | `pkg/runtime/theme` **不存在**（FONT-001-1..-4 与 STYLE-001-1 的验收路径） | `ls pkg/` → 只有 `runtime/app` |
| 5 | `pkg/widgets` **不存在**（FONT-001-5 IME 的验收路径） | 同上 |
| 6 | `contracts/` 里**没有任何**字体字段或接口 | `grep -rn Font contracts/*.go` → 零命中 |
| 7 | `ThemeProvider` 已声明但**零引用**（孤儿接口） | 仅命中 `contracts/runtime_api.go:19`；全仓无实现、无调用、生成代码不引用 |
| 8 | IR 已有主题色表达能力但**未落到生成物** | `contracts.NRGBAColor{IsThemeRef, ThemeField}`（ir_schema.go:118）由 `expander/color.go` 与 `expand_def.go` 产出；codegen 完全不用 |

第 7、8 条合起来说明：**主题这条链路目前是「IR 有、运行时没有、生成代码不用」**。
这是 STYLE-001-1 的真实剩余工作量，也是字体唯一可能挂靠的地方。

## 2. 证据二：`<Def>/<Slot>` 与字体无关

- Def/Slot **已经实现**，在 `internal/compiler/expander/expand_def.go`：
  含 `GXML-DEF-009`（未声明 Slot 却传子节点）、Props 替换、Slot 内容替换，并有
  `expand_def_test.go` 覆盖。
- 卡片写的验收路径是 `./internal/compiler/macro/...`——**该目录不存在**。
  即 STYLE-001-2/-3 的门禁命令指向一个不存在的包（与 A0 §19 是同一类问题）。
- `<Def>` 能带的是 `Props`（含 `color`，可写 `$Theme.Primary`），
  与字体无关；IR 中没有任何字体字段。

结论：**`<Def>/<Slot>` 不构成字体功能的硬依赖**。

## 3. 唯一真依赖：生成代码里的 `th` 是谁

字体规范 `docs/03-架构/字体与文本渲染策略.md`：

- §6.1：`pkg/runtime/theme.Theme{Fonts FontConfig{Family, CJKFallback}}`
- §6.2：生成代码「**不得**包含字体路径、字号常量或 collection 构建逻辑」，
  「只通过主题引用字体：`th.Fonts.Family`」
- §6.3：`contracts` 增加 `FontProvider{Resolve(family string) (string, error)}`，走 `make contract-lint`

而契约 §4.3.1 已冻结为 `th *material.Theme`（Gio 的类型）。

这两处**互相冲突**：按 §6.2 字面实现，`th` 必须是 Goxml 的 `Theme`，
就必须先做 STYLE-001-1，并且要改契约 §4.3.1、改 codegen、改所有依赖该签名的测试。

### 路线 A（规范字面）

`th` 改为 Goxml `Theme`，生成代码写 `th.Fonts.Family`。

- 成本：契约 §4.3.1 修订 + codegen 改动 + 全量生成代码相关测试（签名 / 静态缓存 / 编译 / 绑定 / 事件）跟随修改。
- 收益：与 §6.2 字面一致。

### 路线 B（运行时解析，推荐）

保持 `th *material.Theme`；字体在**运行时**探测 / fallback / 懒加载后装进 `Shaper`，
再把已解析好的主题交给视图；**生成代码一行不改**。

- 成本：§6.2 需要改写（它现在要求生成代码写 `th.Fonts.Family`），属规范修订。
- 收益：
  1. FONT-001 与 STYLE-001 基本解耦，可以立刻开工；
  2. §6.2 的**禁令**（不得出现字体路径 / 字号常量 / shaping 构建）在 B 下同样成立，
     而且更强——生成代码连 `Fonts.Family` 都不出现；
  3. 不动已冻结、且被上百个测试覆盖的契约 §4.3.1。

**推荐 B**，但需要一次正式的设计裁决（§9「版本演进边界」要求走 ADR 或至少记录在案）。
建议把它做成一张 0.5h 的小卡：只产出决策记录与契约 / 规范的同步修改，不写代码。

## 4. 建议的拆卡

### 4.1 前置（新增）

| 卡 | 内容 | 验收 |
|---|---|---|
| `TASK-THEME-SEAM-001` | ① 冻结 `FontProvider` 契约（§6.3 签名）；② 裁决 A/B 路线；③ 确定 `pkg/runtime/theme` 包边界；④ 把主题构建入口从 `pkg/runtime/app` 抽成 `NewTheme()` | `make contract-lint` + `go test ./pkg/runtime/...` |

### 4.2 FONT-001 拆成四张（按你的建议分开建卡）

| 卡 | 内容 | 备注 |
|---|---|---|
| `FONT-001-A` | 各平台 CJK 系统字体探测（目录、超时、诊断、解析失败继续下一候选） | 纯探测，无资源、无体积影响 |
| `FONT-001-B` | 嵌入兜底：**构建期**子集化（GB2312 约 6,763 字）+ `//go:embed` + 增量 ≤3MB 断言 | 独立，可单独权衡体积 |
| `FONT-001-C` | 按字形覆盖的 fallback 链（中英混排、emoji） | 依赖 A（+ 可选 B） |
| `FONT-001-D` | 懒加载与失败诊断（不阻塞首帧、不静默空白） | 依赖 A/B |

### 4.3 IME 建议**移出** FONT-001

- 规范 §8 已把「`<Input>` 必须通过 IME 验收」写在 `TASK-WIDGET-001` 上；
- IME 的验收路径 `./pkg/widgets/...` 与 `<Input>` 控件本身都**不存在**；
- 把它留在 FONT-001 里，等于让字体工作等一个无关（且更重）的前置。
- 建议：`TASK-FONT-001-5` 改为「字体侧为 IME 提供的契约与降级说明」，
  真正的 IME 实测归入 `INPUT-001` / `WIDGET-001`。

### 4.4 STYLE-001 拆成两张

| 卡 | 现状 | 建议 |
|---|---|---|
| `STYLE-001-1`（Theme + 颜色） | Theme 运行时无、codegen 不用 `Background` | 保留，且是 FONT 的真前置（若走 A 路线） |
| `STYLE-001-2/-3`（Def/Slot） | **已在 `expander` 实现并有测试** | 改为「修正验收命令指向 `internal/compiler/expander` + 补齐 codegen 的主题色渲染」，不要重写 |

## 5. 顺带查出的门禁空转（与 A0 §19 同一类）

| 卡 / 子任务 | 验收命令 | 目标状态 |
|---|---|---|
| `FONT-001-1`..`-4` | `go test ./pkg/runtime/theme/...` | 目录不存在 |
| `FONT-001-5` | `go test ./pkg/widgets/...` | 目录不存在 |
| `STYLE-001-1` | `go test ./pkg/runtime/theme/...` | 目录不存在 |
| `STYLE-001-2`/`-3` | `go test ./internal/compiler/macro/...` | 目录不存在（实现在 `expander`） |

按现行纪律（只有证据覆盖卡内要求才标记通过），这些门禁现在**显示通过也没有意义**。

## 6. 本审计的边界（未做的事）

- 未改任何代码、契约、规范、任务卡（任务卡目录本会话不可写，见 dev-log §12.5）。
- 未裁决 A/B 路线——这是设计决策，需要你拍板。
- 未评估嵌入字体的许可与体积细节、未验证 IME 可行性（按上述建议，它不属于这张卡）。
