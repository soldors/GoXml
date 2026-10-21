# TASK-FONT-001-C：按字形覆盖的 fallback 链（边界已冻结）

## 元信息
- 状态：**已实现并通过验收**（2026-10-21；边界同日冻结）
- 验收命令：`go test -run TestFontFallbackChain ./pkg/runtime/theme/...`
  → 退出码 0，**10 个用例 PASS、0 SKIP、0 `no tests to run`**
- 前置：A（`c4ec28b`）+ B（`9b81748`）**均已满足**
- 依赖文档：`docs/03-架构/字体与文本渲染策略.md`、父卡 §3.2 的两个事实

## 0. 定位：C 不是「自己再写一遍 fallback」

冻结前先做了一轮 Gio v0.10.3 源码与本机实测取证（探针：
`pkg/runtime/theme/testdata/fallbackprobe`，该目录不参与 `./...`）。
**结论是：Gio 已经内建了逐字形 fallback，C 不重建它。**

`gioui.org/text` → `fontscan.FontMap.ResolveFace(r)` 的文档注释写明 5 级：

1. 精确匹配 `Query.Families` 的字体（按 aspect 剪枝）；
2. 系统 fallback（相似族名 + 支持当前 script 的字体）；
3. `AddFont`/`AddFace` 手动加入的字体，**按加入顺序**；
4. 所有匹配当前 script 的字体（忽略 aspect）；
5. **兜底：返回任意一个 face**。

第 5 级是 C 的问题根源——**没有任何字体含该字形时，Gio 不返回 nil，而是返回一个
任意 face，渲染出 notdef（豆腐块），且全过程静默**。

因此 C 的四项职责是：**让这 5 级的选择结果可观测、把「缺字形」变成可报告的事实、
在候选准入阶段挡住「能解析但没汉字」的字体、在确有缺口时按字形补链**。

## 1. 取证实测（2026-10-21，本机 Windows / amd64 / Go 1.26.x / Gio v0.10.3）

环境：`CGO_ENABLED=0`，系统命中 `C:\Windows\Fonts\msyh.ttc`（2 faces），
嵌入子集 `GoxmlSansSC-Regular.otf`（1,625,796 B，GB2312 6,763 汉字 + ASCII）。

### 1.1 GlyphID 可反解，且 `gid == 0` 是可靠的 notdef 判据

`text/shaper.go` 的 `newGlyphID` 把 `(ppem, faceIdx, gid)` 打包进 `Glyph.ID`：
`facebits=16, sizebits=16, gidbits=32`。按相同布局反解即可从**公开 API**
（`LayoutString` + `NextGlyph`）拿到命中了哪个 face 与字形 id。

在「仅嵌入子集 + `text.NoSystemFonts()`」的受控场景下，对 10 个样本 rune 对比
反解出的 `gid` 与 `font.Face.NominalGlyph(r)`：

| rune | 覆盖内 | 反解 gid | NominalGlyph | 一致 |
|---|---|---|---|---|
| `A` U+0041 | 是 | 34 | 34 | ✅ |
| `é` U+00E9 | 是 | 105 | 105 | ✅ |
| `世` U+4E16 | 是 | 691 | 691 | ✅ |
| `汉` U+6C49 | 是 | 3472 | 3472 | ✅ |
| `〇` U+3007 | **否** | **0** | MISS | ✅ |
| `𠀋` U+2000B | **否** | **0** | MISS | ✅ |
| `鿿` U+9FFF | **否** | **0** | MISS | ✅ |
| `😀` U+1F600 | **否** | **0** | MISS | ✅ |
| `→` U+2192 | 是 | 269 | 269 | ✅ |
| `★` U+2605 | 是 | 434 | 434 | ✅ |

**10/10 一致**：`gid == 0 ⟺ NominalGlyph 返回 false`。这是 C 全部判定的地基。

> ⚠️ 该位布局是 Gio **未导出的实现细节**。C 必须自带交叉校验（见 §2.2），
> 使 Gio 升级改变布局时测试立刻变红，而不是悄悄给出错误结论。

### 1.2 collection 顺序 = 字形级优先级（决定性实验）

同一个 rune 在两个字体里的 GID 不同，**gid 就是字体指纹**：
`世` 在嵌入子集 = **691**，在 msyh = **1037**。

| collection 顺序 | `世`（两者都有） | `〇`（嵌入 **无**、系统 有） |
|---|---|---|
| `[嵌入, 系统]` | **691**（嵌入） | 28579（系统） |
| `[系统, 嵌入]` | **1037**（系统） | 28579（系统） |

两条结论：
- 顺序决定**谁优先**（`世` 的指纹随顺序切换）；
- 前一个**缺字形时才会跨到下一个**（`〇` 在两种顺序下都取到系统）——
  不是"整段文本用同一个字体"，而是**逐 rune 选**。

### 1.3 本机上嵌入相对系统**零增量**（决定默认行为的关键事实）

遍历 `charset.txt` 全部 **7,540** 个码位，逐位比较嵌入与 msyh 的 `NominalGlyph`：

```text
两者都有      7540
仅嵌入有         0   ← 追加嵌入的真实收益
仅系统有         0
两者都无         0
```

**在这台机器上，嵌入子集是 msyh 覆盖的真子集。** 因此「系统命中时无条件把嵌入
追加进 collection」只会付出解析与内存代价，**换不到任何一个字形**。

这一条直接决定了 §4 的默认值：**补充链按需触发，不无条件开启**。

### 1.4 emoji：有字形，但依赖 shaper 配置，且**不在任何保证内**

| 场景 | `😀` U+1F600 反解 gid | 说明 |
|---|---|---|
| 仅嵌入子集（`NoSystemFonts`） | **0** | 嵌入子集不含 emoji |
| 默认 `NewTheme()`（命中 msyh） | 6787 | 由系统 emoji 字体补上 |
| 裸 `text.NewShaper()`（无 collection） | **0** | **同样没有 emoji** |

要点：
- emoji 命中与否**取决于 shaper 的 collection 与 query**，不是 Gio 的固定保证；
- 嵌入子集**永不**提供 emoji（子集化时就不含），这是已冻结的边界，不是缺陷；
- 父卡 §3.2 第 2 条「emoji 目前没有任何来源」成立：**C 只能报告它缺失，不能凭空补**。

### 1.5 系统探测耗时复核

本轮实测首探 **96.0 ms**（上一轮 110 ms）。仍远超规范 §3.2 的 20 ms 预算——
**这是 D 的动因，C 不解决它**，且 C 不得让这条路径更慢。

## 2. C1：字形解析与 notdef 可观测（必做）

### 2.1 交付物（`pkg/runtime/theme/fallback.go`）

```go
// GlyphResolution 是单个 rune 在给定 shaper 下的解析结果。
type GlyphResolution struct {
    Rune     rune
    GID      font.GID   // 0 表示 notdef（无任何字体提供该字形）
    HasGlyph bool
    FaceIdx  int        // 命中的 face 索引；-1 表示 shaper 未产出任何 glyph
    Advance  fixed.Int26_6
}

// Resolver 基于一个 shaper 做逐 rune 字形解析。
type Resolver struct{ ... }

func NewResolver(s *text.Shaper, p text.Parameters) *Resolver
func (r *Resolver) ResolveRune(rn rune) GlyphResolution
func (r *Resolver) MissingGlyphs(s string) []rune   // 返回 notdef 的 rune（去重、保序）
```

约束：
- **一次只 shape 一个 rune**。`Glyph` 只带 cluster 的 rune 计数、不带 rune 本身，
  多字符 shaping 无法可靠地把 glyph 映射回输入 rune。
- `Resolver` 只读，不改 shaper；不引入隐藏全局状态。

### 2.2 解码器的可证伪性（硬性）

依赖未导出的位布局必须能被证伪，否则等于把结论建立在假设上。C 自带两条：

1. **交叉校验**：受控场景（`NoSystemFonts` + 嵌入子集）下，对一批**已知覆盖内**的
   码位，`ResolveRune` 的 `GID` 必须 **等于** `NominalGlyph` 的返回值。
   布局一变，这条立刻红。
2. **反向自检（永久保留）**：对一批**已知覆盖外**的码位（emoji、U+2000B 等），
   `HasGlyph` 必须为 false、`GID` 必须为 0。判定器若恒返回"有字形"，这条会红。

### 2.3 实现结果（2026-10-21）

`pkg/runtime/theme/fallback.go`：解码器 `splitGlyphID`、`GlyphResolution`、
`Resolver`（`ResolveRune` / `MissingGlyphs`）。

- 交叉校验实测：**抽样 146 个码位，146 个覆盖内 gid 与 `NominalGlyph` 逐项一致**。
- 一次只 shape 一个 rune：`text.Glyph` 只带 cluster 的 rune **计数**、不带 rune 本身，
  多字符 shaping 无法把 glyph 映射回输入。
- `nil` shaper 显式退化为「无字形」，不 panic、也不伪装成有字形。

> ⚠️ **变异实测暴露的盲点（值得记住）**
>
> 首轮把解码器的位移改错（`faceIdx` 位移 +1）后，**测试仍然全绿**——
> 因为当时 `FaceIdx` 字段**没有任何断言**，而 gid 的解码独立于该位移。
> 也就是说，`FaceIdx` 曾经是一个"错了也没人知道"的字段。
> 修法不是放宽断言，而是补上 `FaceIdx` 断言（§6-4 的
> "FaceIdx 必须指向 collection 的实际位置"），重跑同一变异后如期变红。
> **教训：新增一个诊断字段时，如果它没有断言覆盖，它就是装饰品。**

## 3. C2：覆盖判定作为候选准入（必做，修一个真实漏洞）

### 3.1 漏洞

B 的探测标准是「**能解析**」（`opentype.ParseCollection` 成功）＋「集合非空」。
它**不检查**命中字体是否真有汉字。因此一台只有拉丁字体（或中文字体被替换成
残缺版本）的机器会**系统命中成功、报告无错、汉字全部渲染成 notdef**——
正是规范 §1.2 禁止的「静默降级到空白渲染」。

这不是假想：§1.2 已证明 Gio 在第 5 级会静默返回任意 face，整个过程无错误。

### 3.2 交付物

```go
// RequiredRunes 是准入判定用的最小码位集（抽样，不是全量）。
func RequiredRunes() []rune

// Coverage 统计 faces 对 required 的覆盖，返回命中数与缺失清单。
func Coverage(faces []font.FontFace, required []rune) (hit int, missing []rune)
```

接入位置：`probe()` 命中一个候选后，先做覆盖判定；命中数低于阈值则把该候选
标为新的状态 `StatusCoverageInsufficient` 并**继续下一个候选**，
全部不足则落到嵌入兜底（B 的既有路径）。

约束：
- 抽样集必须**小而确定**（建议 16–32 个码位，覆盖 GB2312 一级字 + 常用标点），
  判定要快——它跑在探测热路径上，C 不得让 §1.5 的 96 ms 明显变长（**实测并登记**）。
- 阈值不写成 100%：单字形缺失不该整族判死。阈值与理由写进卡与代码注释。

### 3.3 实现结果（2026-10-21）

- 准入集：`requiredRunes` = **GB2312 一级字 16 个 + 常用全角标点 4 个 = 20 个码位**；
  阈值 `minCoverageRatio = 0.9`（20 个里至少命中 18，理由写在代码注释里）。
- 新状态 `StatusCoverageInsufficient`：解析成功但覆盖不足的候选记为此状态并
  **继续下一个候选**；全部不足则落到嵌入兜底。
- 实测：goregular（纯拉丁）作为唯一候选时 → 判定 0/20 → 记为
  `coverage_insufficient` → 兜底到 `embedded:`，`id` 不再以 `system:` 开头。
  这正是漏洞本体被挡住的证据。

#### 性能优化（实测，勿回退）

首版 `Coverage` 把 `faces[i].Face.Face()` 写在码位循环里，实测 **589 µs/op**；
而 `NominalGlyph` 本身是纳秒级——瓶颈全在**接口解包**（约 29 µs/次）。
把解包提到循环外（`underlyingFaces`）后降到 **22 µs/op**（约 27 倍）。
判定跑在每个候选的命中路径上，这个数量级差异会直接叠加到本已 ~96 ms 的探测上。

#### 对 A / B 既有测试的修正（不是放宽断言）

覆盖判定改变了「什么是命中」的定义，三处既有用例的前提随之失效：
它们用 **goregular 当"好的系统字体"**，而 goregular 一个汉字都没有。

| 位置 | 原写法 | 改后 | 性质 |
|---|---|---|---|
| `font_test.go` `TestFontProbeContinuesOnParseFailure` | `good.ttf` = goregular | `good.otf` = 嵌入子集（含汉字） | 前提修正 |
| `font_test.go` `TestFontProbeResolveCaching` | 同上 | 同上 | 前提修正 |
| `embedded_fallback_test.go`「系统命中时不得解析嵌入字体」 | 同上 | 同上 | 前提修正 |

**没有放宽任何一条断言**：三处断言（命中、`system:` 前缀、嵌入加载 0 次）原样保留，
只是让"good 候选"名副其实。goregular 的场景改由 C 的
`TestFontFallbackChainCoverageInsufficient` 承接，成为漏洞的证据。

## 4. C3：字形级补充链（必做，但**按需**触发）

§1.2 已证明「collection 顺序 = 字形级优先级」真实可用；§1.3 已证明**本机追加零收益**。
因此：

- **当且仅当** C2 判定系统命中字体存在覆盖缺口时，才把嵌入子集**追加**到 collection
  末尾（`[系统..., 嵌入...]`），让 Gio 的逐字形 fallback 去补缺口；
- **覆盖完整时不加载嵌入** —— B4「系统命中则不解析嵌入字体」的约束**保持不变**，
  其计数器断言继续有效；
- 触发与否必须进 `ProbeReport`（新增字段），让"这一轮到底补没补"可查。

这样名实相符：链**按字形覆盖**建立，而不是无条件塞一个字体进去。
本机的预期行为是**不触发**（§1.3 增量 0），因此 C 不得靠本机观察来证明补充链生效——
必须用**受控 shaper**（见 §6）证明顺序语义与补位行为。

### 4.1 实现结果（2026-10-21）

`Provider.supplement(faces, rep)`：

- 触发条件 `rep.Coverage != nil && len(rep.Coverage.Missing) > 0`；
- 追加在集合**末尾**（系统在前面才有优先）；
- 成功置 `Coverage.Supplemented = true`，并把嵌入链路诊断写进 `rep.Embedded`；
- 嵌入加载失败**原样返回**——系统已可用，嵌入只是增益，不得把整体打成失败；
- `ProbeReport.Coverage` 新字段，`String()` 里以 `coverage 20/20` 形式暴露。

实测（本机）：系统命中 msyh、覆盖 20/20 → **不加载嵌入**（计数器 0 次），
B4「系统命中则不解析嵌入字体」的约束**保持有效**。

## 5. C4：emoji 与降级行为（必做，且必须如实）

- 实测并记录 emoji 在本机三种场景下的真实结果（§1.4 表）。
- 提供 `MissingGlyphs` 让调用方能查出"这段文字里有 N 个字符没有字形"。
- **明确不做**：不为 emoji 引入第二个嵌入字体（体积与许可证都要重新取证，
  属新任务，不在 C 范围）。C 的降级是**报告**，不是**补字形**。

## 6. 验收矩阵

命令：`go test -run TestFontFallbackChain ./pkg/runtime/theme/...`
（前缀不得改名，否则 `internal/docgate` 的
`TestFont001AcceptanceCommandsAreNotVacuous` 报红。）

| # | 验收点 | 方法 | 结果 |
|---|---|---|---|
| 1 | GlyphID 反解正确 | 受控 shaper（`NoSystemFonts` + 嵌入子集）下 gid 与 `NominalGlyph` 逐项相等 | ✅ 146/146 一致 |
| 2 | notdef 可判定 | 覆盖外码位 `HasGlyph=false`、`GID=0`；**反向自检常驻** | ✅ 含两条反向自检 |
| 3 | 中英混排 | `"Hello 世界"` 全部有字形，且中英段分别命中预期指纹 | ✅ 缺字形清单为空 |
| 4 | **顺序语义**（§1.2） | `[嵌入,系统]` 与 `[系统,嵌入]` 下同一 rune 的 gid 指纹**不同**，与预期顺序一致 | ✅ 拉丁/嵌入指纹 36 vs 34，随顺序切换 |
| 5 | **缺字形补位**（§1.2） | 取一个"前一个无、后一个有"的码位，断言 gid 等于**后者**的指纹 | ✅ 汉字补到嵌入，附"仅拉丁时必为 notdef"对照 |
| 6 | 覆盖判定可证伪 | 对嵌入 faces 喂 emoji 集合必须报缺失；喂 GB2312 抽样必须全命中 | ✅ 含阈值边界与空集合用例 |
| 7 | 补充链按需 | 覆盖完整 → 不加载嵌入（计数器断言沿用 B4）；覆盖有缺口 → 追加且进诊断 | ✅ 计数器 0 次；分支用例 3 个 |
| 8 | emoji 实测 | 记录三种场景结果；断言"嵌入子集无 emoji"（这是冻结边界，不是缺陷） | ✅ 嵌入无；系统 gid=6787（仅记录） |
| 9 | 不拖慢探测 | 记录接入 C2 前后的探测耗时，进 dev-log；不得显著恶化 §1.5 的 96 ms | ✅ 判定单次 **11.86 µs**（远低于 1 ms 预算） |

**每项先证明能失败**：实现前先把断言写成反向（或注入错误实现），确认测试红，
再实现到绿。变异必须**能编译**——编译失败不算变异证明。

#### 变异验证记录（5 次，全部如期变红）

| # | 注入的错误实现 | 变红的用例 | 退出码 |
|---|---|---|---|
| A | `HasGlyph` 恒为 `true` | Notdef、SupplementsMissingGlyph/反向自检 | 1 |
| B | `splitGlyphID` 的 faceIdx 位移 +1 | OrderSemantics/FaceIdx 位置 | 1 |
| C | `CoverageSufficient` 恒 `true` | Coverage/纯拉丁、Coverage/阈值边界 | 1 |
| D | 补充链无条件触发（去掉缺口短路） | SupplementOnDemand/覆盖完整时不加载 | 1 |
| E | 覆盖判定不再挡住候选 | CoverageInsufficient | 1 |

> 变异 B 在**补上 FaceIdx 断言之前**未能让任何测试变红（详见 §2.3 的警示框）。
> 这条记录保留在此，是为了说明"跑过变异"不等于"覆盖充分"。

## 7. 风险与边界

1. **位布局耦合**：`GlyphID` 打包方式是 Gio 私有实现。靠 §2.2 两条自检兜底；
   Gio 升级后若测试变红，**不允许**通过放宽断言让它变绿，要重新读源码确认布局。
2. **受控场景 vs 真实场景**：`NoSystemFonts` 下行为确定，适合断言具体 gid；
   系统场景下 Gio 的选择依赖机器上装了什么字体，**只能断言行为、不能断言具体数值**
   （跨平台会红）。两类测试必须分开，注释写清。
3. **C 不解决延迟**：96 ms 归 D。C 只允许新增常量级开销（覆盖判定），且要实测登记。
4. **emoji 无来源**：C 只报告。要真正显示 emoji 需另开任务（新字体取证 + 体积预算）。
5. **阈值是判断不是事实**：C2 的覆盖率阈值带主观性，必须把取值与理由写进代码注释，
   不接受"看起来合理"。
6. **本机无法证明补充链的收益**（§1.3 增量 0）。收益论证要等 Linux 精简容器
   或"只有拉丁字体"的环境，属跨平台验证，不在 C 内完成。

## 8. 交接记录

| 项目 | 内容 |
|---|---|
| 当前基线 | 实现基于 `b0eaff5`（远端 `master` 同）；A `c4ec28b`、B `9b81748` |
| 本卡状态 | **已实现并通过验收**（10 个用例全 PASS，5 次变异全部如期变红） |
| 取证产物 | `pkg/runtime/theme/testdata/fallbackprobe`（6 个场景：三场景字形表、顺序实验、覆盖增量统计） |
| 已完成 | 设计调研与实测（§1）；C 卡冻结；§2 字形解析、§3 覆盖判定、§4 按需补充链、§5 emoji 降级 |
| 未完成 | **跨平台验证**：补充链的真实收益无法在本机证明（§1.3 增量 0），
  需 Linux 精简容器或"只有拉丁字体"的环境。**emoji 仍无来源**（只报告不补）。
  **96 ms 探测延迟归 D** |
| 落地文件 | `pkg/runtime/theme/fallback.go`（新增，C1+C2）、
  `pkg/runtime/theme/font.go`（改：新状态、`supplement`、`ProbeReport.Coverage`）、
  `pkg/runtime/theme/fallback_test.go`（新增，10 个用例）、
  `pkg/runtime/theme/font_test.go` / `embedded_fallback_test.go`（改：good 候选换含汉字字体）、
  `pkg/runtime/theme/font_pending_test.go`（改：删除 C 占位 skip）、
  `pkg/runtime/theme/testdata/fallbackprobe/main.go`（新增，取证探针）、
  `docs/04-WBS/WBS总表.md`（新增 C 行） |
| 已知失败或风险 | 见 §7（6 条）。**emoji 仍无来源**、**本机增量 0**、**96 ms 归 D**；
  另新增：位布局耦合靠自检兜底（§2.2），不得靠放宽断言过关 |
| 下一步 | 评估 **D 的懒加载设计**（系统探测 ~96 ms 超 20 ms 预算）。
  C 已把判定开销压到 11.86 µs，延迟的大头仍在系统字体探测本身；
  D 动手前应先复核 C 落地后的真实探测耗时 |
