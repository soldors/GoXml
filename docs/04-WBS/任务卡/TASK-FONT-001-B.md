# TASK-FONT-001-B：嵌入字体兜底（边界已冻结）

## 元信息

- 状态：**边界已冻结 + B1/B2/B3前置已取证通过（2026-10-20）；
  仓库内实现未开始**；两个开工阻塞项**均已解除**（§7）
- 父卡：[TASK-FONT-001.md](TASK-FONT-001.md)
- 前置：[TASK-FONT-001-A](TASK-FONT-001-A.md) ✅（`c4ec28b`）
- 规范依据：[字体与文本渲染策略.md](../../03-架构/字体与文本渲染策略.md) §2.2、§3.1、§3.2、§1.2
- 立卡日期：2026-10-20

## 0. 定位：B 不是「放一个字体文件进去」

B 要交付的是**系统字体探测失败后的真实兜底**，并为 A 遗留的失败处理缺口
（A 卡约束 10）提供闭合依据。只把一份字体塞进 `//go:embed` 而不定义
「什么时候用、失败怎么办、体积怎么验」，不算完成 B。

## 1. B1 字体来源（冻结）

> **2026-10-20 更新：B1 已通过（阻塞项 1 解除，走路径①官方发布）。**
> 本机 Windows 副本经实测为**非发布构建**，已**降级**、不得作为子集化输入。
> 详见 §1.4。

### 1.1 选型

| 候选 | 许可证（实测） | 版本 | 结论 |
|---|---|---|---|
| **Noto Sans SC**（首选，官方发布文件） | SIL OFL 1.1（nameID 13/14 实测；官方仓库 `LICENSE` 全文已取） | `2.004`（nameID 3/5） | **采用（子集化输入）** |
| Source Han Sans SC Medium（备选） | SIL OFL 1.1（nameID 13/14 同上） | `2.001` | 备选（未启用） |
| Windows 自带 `msyh` / `simsun` / `simhei` | **Microsoft 许可证禁止再分发** | — | **禁止嵌入**（只能运行时探测，A 的用法） |

**子集化输入（冻结，唯一来源）**：

| 项 | 值 |
|---|---|
| 来源 | `github.com/notofonts/noto-cjk`，tag **`Sans2.004`**（发布于 2022-01-27） |
| commit | `523d033d6cb47f4a80c58a35753646f5c3608a78` |
| 路径 | `Sans/SubsetOTF/SC/NotoSansSC-Regular.otf` |
| 大小 | 8,331,336 字节 |
| SHA-256 | `faa6c9df652116dde789d351359f3d7e5d2285a2b2a1f04a2d7244df706d5ea9` |
| git blob SHA-1 | `fc0fda9394367c16c1af7555a2ae4d5e2e6a6f02`（**本地重算与 API 返回一致**） |
| nameID 1 / 3 / 5 | `Noto Sans SC` / `2.004;GOOG;NotoSansSC-Regular;ADOBE` /
`Version 2.004;hotconv 1.0.118;makeotfexe 2.5.65603` |
| 含 `fvar` | **否**（静态字体）→ §2.3 的"变量字体必须先定实例"风险**自动消失** |
| 轮廓 / cmap | CFF / 30,890 |
| nameID 0 | `© 2014-2021 Adobe (http://www.adobe.com/).`——**不含** Reserved Font Name 声明 |

### 1.2 OFL 衍生约束（硬性）

- 子集产物**必须改名**：官方文件 nameID 0 虽未声明 Reserved Font Name，
  但 nameID 7 明示 `Noto is a trademark of Google Inc.`；为避免衍生品沿用上游品牌名，
  B 统一改名为 **`Goxml Sans SC`**（PostScript 名 `GoxmlSansSC-Regular`）。
  > 改名后的字体名**待维护者确认**；一旦确认即写入 §5 的 `TestEmbeddedFallbackMetadata` 断言。
- OFL 全文与版权声明**必须随附**到仓库（不能只写一个 "OFL 1.1" 字样）：
  官方仓库 `LICENSE` 全文已取，4,301 字符，
  SHA-256 `6a73f9541c2de74158c0e7cf6b0a58ef774f5a780bf191f2d7ec9cc53efe2bf2`。
- 改名后的字体名写进 B 的元数据（§5 的 `TestEmbeddedFallbackMetadata` 断言它）。

### 1.3 B1 的验收证据（缺一不可）

来源页面/发布地址、版本字符串、**许可证全文文件**、原始文件 SHA-256、子集产物 SHA-256。

**已取得（2026-10-20，全部实测）**：

| # | 证据 | 值 |
|---|---|---|
| 1 | 官方发布地址 | `github.com/notofonts/noto-cjk` tag `Sans2.004` @ `523d033d` |
| 2 | 版本字符串 | `Version 2.004;hotconv 1.0.118;makeotfexe 2.5.65603` |
| 3 | 许可证全文 | 官方仓库 `LICENSE`，4,301 字符，SHA-256 `6a73f954…ef2bf2` |
| 4 | 原始文件 SHA-256 | `faa6c9df…d5ea9`（8,331,336 B） |
| 5 | 子集产物 SHA-256 | `21c297a6…7a9550`（1,625,796 B），见 §2.4 |

**交叉核验（两个独立来源）**：

- PyPI `noto-cjk-sans-otc` 0.0.1：wheel 15,521,948 B，
  SHA-256 `44596df7f0c1590cb9c4cfe85b9213b7e91e1086a8a024f1a05df6d6badd33ec`
  （**与 PyPI 记录的 digest 一致**）；
  内含 `NotoSansCJK-Regular.ttc` 19,484,784 B，
  SHA-256 `b76b0433203017ca80401b2ee0dd69350349871c4b19d504c34dbdd80541690a`
  （10 面 Super OTC，cmap 44,810，**同版本 2.004、同构建串**）。
- **许可证全文逐字节一致**：PyPI wheel 内 `dist-info/licenses/LICENSE` 的 SHA-256
  与官方仓库 `LICENSE` **完全相同**（`6a73f954…ef2bf2`）——两个独立来源互相印证。
- 与官方 SC 版的字形关系：wheel 的 SC 面与官方 `NotoSansSC-Regular.otf`
  cmap 范围不同（44,810 vs 30,890，打包范围差异），
  但**前 5,000 个共有码位的 glyph 映射完全一致** → 同一套字形数据。

### 1.4 本机 Windows 副本：降级（重要发现）

`C:\Windows\Fonts\NotoSansSC-VF.ttf`：17,773,244 字节，
SHA-256 `763146584cf0710223441356b4395e279021b0806c196614377a7a0174ae074a`。

实测发现它**不是官方发布构建**，不能作为可追溯来源：

| 项 | 本机 VF | 官方 2.004 |
|---|---|---|
| nameID 5 | `Version 2.04;241114210130;`**`non-release`** | `Version 2.004;hotconv 1.0.118;makeotfexe 2.5.65603` |
| nameID 6 / 17 | `NotoSansSC-Thin` / **`Thin`** | `NotoSansSC-Regular` / — |
| `fvar` | 有，**默认实例 `wght=100`（Thin）** | 无（静态） |
| 轮廓 / 大小 | glyf / 17.7 MB | CFF / 8.3 MB |
| cmap | 30,890（与官方**集合完全相同**，双向差集均为 0） | 30,890 |

**结论**：覆盖等价，但本机那份是 **2024-11-14 的非发布构建**（构建串自述 `non-release`），
无法与任何上游发布对应。**降级为"仅作 A 的运行时探测目标，不作 B 的子集化输入"。**
这正是本机 SHA-256 与官方/第三方文件不一致的原因——**不是版本差异、不是构建处理差异，
而是打包范围与文件格式不同**（Super OTC 10 面 / 非发布 VF / 官方 SC 静态单面）。

## 2. B2 构建期子集化（冻结）

### 2.1 工具与可复现性

| 项 | 冻结值 |
|---|---|
| 工具 | `fontTools` 的 `pyftsubset` |
| 版本 | **固定版本**（本轮实测 PyPI 可下载 `fonttools 4.67.0`） |
| 位置 | 仓库内脚本（构建期），**运行时绝不执行子集化** |
| 产物 | 写进 `pkg/runtime/theme` 的 embed 目录，并记录 SHA-256 |

### 2.2 字符集与覆盖边界（不许含糊）

| 项 | 冻结 |
|---|---|
| 字符集 | GB2312 一级 + 二级常用字（约 6,763 字）+ ASCII + 常用标点 |
| 覆盖声明 | **不是完整 CJK 覆盖**。完整 CJK 远超此集合，子集后的字体不得被描述为
"完整 CJK"；覆盖清单必须是**可验证**的（测试逐个断言清单内的字有字形） |
| 边界标注 | 清单外的字（罕用字、部分扩展区）无字形——这是已知且必须写明的边界，
不是缺陷 |

### 2.3 变量字体处理

⚠️ 原约束（针对本机 VF）：`NotoSansSC-VF.ttf` 含 `fvar`，子集化必须先固定实例
（例如 `wght=400`），否则可能出现"子集成功但 Gio 解析失败"。

✅ **本约束已被 §1.1 的选型消除**：官方 `NotoSansSC-Regular.otf` 是**静态字体，无 `fvar`**，
不需要定实例。约束保留在卡内，作为"若将来改用 VF 输入"时的检查项。

### 2.4 B2 子集化试跑实测（2026-10-20，仓库外取证）

工具：`fontTools 4.67.0` 的 `pyftsubset`（Python 3.13.12）。

字符集生成：ASCII 可打印 95 个 + GB2312 全部可解码码位 7,445 个 → 并集 **7,540**
（其中 **汉字 6,763**，与 §2.2 冻结的"约 6,763 字"一致）。

命令（可复现）：

```
python -m fontTools.subset NotoSansSC-Regular.otf \
  --unicodes-file=charset.txt \
  --output-file=subset_raw.otf \
  --no-hinting \
  --drop-tables+=DSIG,LTSH,VDMX,hdmx,PCLT \
  --name-IDs=0,1,2,3,4,5,6,7,13,14 \
  --notdef-outline --recommended-glyphs
```

| 项 | 实测 |
|---|---|
| 子集耗时 | 5.5 s（构建期一次性，运行时绝不执行） |
| 子集产物（改名前） | 1,626,340 B，SHA-256 `210979abdaee11a8472f1c84574436b7e6de2be28513d1a77026980aa1a7fa1e` |
| **改名后产物** | **1,625,796 B（1.55 MB）**，SHA-256 `21c297a60425f20694997d515ed7b2f4b023fa196353c7c87ee15df5607a9550` |
| 改名后 nameID 1 / 4 | `Goxml Sans SC` |
| 改名后 nameID 3 / 6 | `1.000;GOXML;GoxmlSansSC-Regular` / `GoxmlSansSC-Regular` |
| 子集 cmap | 7,542 |
| **清单内缺失** | **0** ✅ |
| 清单外（如实标注边界） | `U+20000`、`U+2A700`、`U+3400`、`U+9FA6`、`U+E000` **均无字形** ✅ |
| 轮廓 / 表 | CFF；`BASE CFF GDEF GPOS GSUB OS/2 VORG cmap head hhea hmtx maxp name post vhea vmtx` |

→ **1.55 MB 远低于 §5 的 3 MB 门槛**，且**未牺牲覆盖**（GB2312 6,763 汉字零缺失）。

## 3. B3 嵌入与解析（冻结）

| 项 | 冻结 |
|---|---|
| 嵌入方式 | `//go:embed` |
| 解析 API | `gioui.org/font/opentype.ParseCollection`（v0.10.3）→ `[]font.FontFace` |
| 有效字体 | faces > 0，且含「中」字形（`NominalGlyph('中') != 0`） |
| 损坏字体 | 返回**可诊断**的错误（含原因），**不得** panic、**不得**静默返回空集合 |
| 解析时机 | 兜底触发时**同步**解析（见 §6 关于不实现异步的边界） |

**B3 前置实测（2026-10-20，仓库外 Go 探针，尚未写入仓库代码）**：

| 项 | 实测 |
|---|---|
| `opentype.ParseCollection` | **成功**，`faces=1` |
| 解析耗时 | **1.58 ms**（对比 A 的系统探测 ~110 ms —— 嵌入解析比系统探测快两个数量级） |
| 读出的 typeface | `Goxml Sans SC`（改名生效，被 Gio 正确读出） |
| 「中」「文」「汉」「，」「。」 | 均有字形（gid ≠ 0） |
| `U+20000`（清单外） | `gid=0, ok=false`（如实无字形，非崩溃） |
| Shaper 装配 | `text.NewShaper(text.WithCollection(faces))` 成功，layout 8 glyphs |
| 损坏输入（非字体字节） | 返回 `unsupported font format [110 111 116 32]`——**可诊断错误，未 panic** ✅ |
| 截断输入（前 2048 字节） | 返回 `reading font 0 of collection: EOF`——**可诊断错误** ✅ |

→ 结论：**CFF 子集字体可被 Gio v0.10.3 正常解析与装配**，
§3 冻结的"损坏字体必须可诊断"也已在真实 API 上验证。

## 4. B4 兜底选择顺序（冻结）

1. 系统探测（A）命中 → 用系统字体；
2. 系统探测**全部失败**或**当前平台无候选**（`CandidatesFor` 返回 nil）→ 用嵌入子集；
3. 两者都不可用 → 按 §6 的失败语义处理。

验收必须能证明：

- 注入"系统探测失败"（候选指向不存在/全部损坏）→ 选中嵌入集合，标识前缀 `embedded:`；
- 系统命中时**不解析**嵌入（用计数器注入断言嵌入解析未被调用）——避免无谓的解析成本。

## 5. B5 体积预算（冻结）

| 项 | 冻结 |
|---|---|
| 基线 | `examples/demo` 二进制，同一 GOOS/GOARCH、**固定构建标志**（`CGO_ENABLED=0` 等） |
| 比较 | 「未嵌入」与「嵌入后」的**字节差** |
| 硬门槛 | **≤ 3 MB**（规范 §3.1） |
| 断言方式 | 自动化：构建两次并比较大小（脚本 + 测试断言），阈值写死在测试里 |

⚠️ **不得为压 3MB 牺牲覆盖**：若子集后仍超 3MB，正确的做法是**缩小字符集并如实标注边界**
（§2.2），不是删掉覆盖清单的一部分再宣称"覆盖完整"。

**当前实测**：子集产物 **1,625,796 B（1.55 MB）**，距 3 MB 门槛有约 1.45 MB 余量。
二进制增量（`examples/demo` 同标志构建两次的字节差）仍需在 B5 实现时**实测**——
嵌入数据经 `//go:embed` 进入二进制，增量预期≈子集大小，但不做假设。

## 6. B6 A→B 失败语义（冻结，关闭 A 卡约束 10）

A 的未闭合项：探测失败时 `NewTheme()` 沿用 Gio 默认 Shaper，且没有强制诊断。
B 必须关闭它，冻结为：

| 层 | 行为 |
|---|---|
| `Provider.Resolve` | 系统与嵌入都失败 → 返回包装了 `contracts.ErrFontNotFound` 的错误（严格，不伪装） |
| `NewThemeWithReport` | 返回的 `*ProbeReport` **必须**包含系统与嵌入两条链路的结果与原因 |
| `theme.Diagnostics()`（新增） | 暴露最近一次诊断，供 app / CLI 打印上报 |
| `app` 默认主题路径 | 主题构建失败（系统 + 嵌入都不可用）时，`Run` / `NewApp` **返回字体诊断错误**，
不再静默沿用 Gio 默认；**显式注入主题的路径不受影响**（`RunWith`/`NewApp` 传了 th） |

**"不再静默降级"的判定 = 错误必须可观察**（报告有 Err、app 层能拿到错误），
而不是"必须崩溃"。是否进一步升级为"启动即失败"由上表最后一行确定为**是**（默认主题路径），
理由：规范 §1.2「字体加载失败必须产生明确诊断，不得静默降级到空白渲染」。

> 若后续评估认为改变 `Run` 的失败模式风险过大，应**先改本卡并登记理由**再实现，
> 不得在实现里悄悄保持旧行为。该变更会影响 `pkg/runtime/app` 的现有用例
> （需同步更新并给出证据）。

## 7. 开工阻塞项

| # | 阻塞项 | 现状 | 解除条件 |
|---|---|---|---|
| 1 | 字体产物与上游哈希核对 | ✅ **已解除（2026-10-20，走路径①）**：
  从官方仓库 tag `Sans2.004` 直接取得文件，
  并用 **git blob SHA-1 本地重算** 验证完整性（与 API 返回一致）；
  许可证全文另经 PyPI 独立来源**逐字节比对一致**。
  详见 §1.1 / §1.3 | 已完成 |
| 2 | `fonttools` 可用 | ✅ **已解除**：`fontTools 4.67.0`；
  本轮已实测跑通 `pyftsubset`（5.5 s，产物 1.55 MB），见 §2.4 | 已完成 |

### 7.1 阻塞项 1 解除后仍保留的诚实声明

- **"路径①"是官方仓库 + git blob SHA-1 校验**，强于原先设想的 PyPI 路径②；
  但由于 GitHub 未发布 release asset 的 SHA-256，
  **不存在"官方公布的哈希"可供再比对一次**——完整性由 git 对象模型的 SHA-1 自证。
- 上一轮记录的"GitHub 不可达（502）"是**当时的网络状态**；本轮复测 `github.com` 与
  `api.github.com` 均可达（200），据此完成取证。网络状态可能再次变化，
  取证脚本与产物哈希已记录在案，可复核。
- **本机 Windows 副本仍未、也无法与上游对应**（它是 `non-release` 构建，见 §1.4）——
  这一点不会因为官方文件到手而改变，它只影响"本机那份是什么"，不影响 B 的输入。

## 8. 验收矩阵

命令统一：`go test -run TestEmbeddedFallback ./pkg/runtime/theme/...`
（命中真实用例；`SKIP` 计为未执行，不计入通过）。

| 测试名 | 覆盖 | 证据等级 |
|---|---|---|
| `TestEmbeddedFallbackMetadata` | B1：来源/版本/许可证全文/改名后字体名/两套 SHA-256 可断言 | 逻辑（读仓库内元数据文件） |
| `TestEmbeddedFallbackSubsetCoverage` | B2：覆盖清单内的字**逐个**有字形；并断言存在清单外 CJK 码位无字形（如实标注边界，不伪造"完整 CJK"） | 逻辑 |
| `TestEmbeddedFallbackParseAndCorrupted` | B3：有效字体解析成功且含「中」；损坏字节 → 可诊断错误（非 panic、非空集合） | 逻辑 |
| `TestEmbeddedFallbackSelectionOrder` | B4：系统失败→选嵌入（前缀 `embedded:`）；系统命中→嵌入解析**未被调用** | 逻辑 |
| `TestEmbeddedFallbackSizeBudget` | B5：二进制增量 ≤ 3MB（构建两次比较） | **实测**（本机 Windows） |
| `TestEmbeddedFallbackFailureSemantics` | B6：系统与嵌入都失败 → `ErrFontNotFound` + 报告含两条链路原因 + app 默认路径返回错误 | 逻辑 |

占位用例 `TestEmbeddedFallback`（当前 `t.Skip`）在实现时替换为上述真实断言；
**函数名前缀保持不变**，否则验收命令退化为空转（docgate 会报红）。

## 9. 风险与边界（用户提出，必须留在卡里）

1. **不得为 3MB 牺牲覆盖**：子集必须有明确字符集与可验证覆盖清单；
   只保证常用中文时必须如实标注边界，**不得**描述为完整 CJK 覆盖。
2. **B 不等于字体系统完成**：A 的 Windows 实测首次系统探测 ~110ms，
   已超规范 §3.2 的 20ms 预算；B 解决"字体缺失时的兜底"，**不会**消除探测延迟——
   那仍由 **D（懒加载）** 解决。B 还应**实测登记嵌入解析耗时**（可能又是一笔启动成本）。
3. **B 不实现异步加载系统**：`NewTheme()` 当前承担同步主题构建，懒加载属 D。
   B 只提供可靠的「嵌入解析 + 装配」能力，解析在兜底触发时同步进行。
4. **许可证合规**：子集必须改名（OFL 保留字体名 'Source'），许可证全文随附；
   **禁止**嵌入 Windows 自带字体。
5. **并发工作树**：禁用 `git add -A`（dev-log 10-20 §22.7 / §22.13）。

## 10. 交接记录

| 项目 | 内容 |
|---|---|
| 当前提交 SHA | 见 dev-log 2026-10-20 §26 |
| 已完成 | B1–B6 边界与验收矩阵冻结；**B1 来源核验通过（阻塞项 1 解除）**；
  **B2 子集化试跑成功**（1.55 MB / 6,763 汉字零缺失）；
  **B3 解析前置实测通过**（Gio 可解析 CFF 子集，1.58 ms，损坏输入可诊断） |
| 未完成 | **仓库内实现尚未开始**：`//go:embed` 接入、B4 选择顺序、
  B5 二进制增量实测、B6 失败语义（含 `pkg/runtime/app` 用例同步） |
| 已运行命令及结果 | 见 §1.1 / §1.3 / §1.4 / §2.4 / §3。
  取证脚本与报告在 `%TEMP%\goxml-b-font\`（report.txt ~ report5.txt） |
| 待确认（1 项） | 改名后字体名 **`Goxml Sans SC`**（§1.2）——确认后写入
  `TestEmbeddedFallbackMetadata` 断言；未确认前不落地 embed 文件 |
| 已知失败或风险 | 见 §9 |
| 下一位接手人 | B 实现：① 确认改名后的字体名 → ② B3–B5 接入与体积实测 → ③ B6 失败语义。
  顺序上不并行开 C/D |

### 10.1 本轮新增风险

6. **取证产物不在仓库内**：B2 子集产物目前只在 `%TEMP%`，**尚未写入仓库**。
   下一次执行必须把「子集脚本 + 字符集清单 + 产物 + 许可证全文 + 元数据」
   一并入库，并把产物 SHA-256 写进断言，否则产物不可复现。
7. **网络可达性是时序状态**：本轮 GitHub 可达、上一轮不可达。
   若将来需要重新取证，应先复测连通性，不得沿用"GitHub 不可达"的旧结论。
8. **Git blob API 的大小上限**：本次文件 8.3 MB（API 上限 100 MB）。
   若将来改用更大的输入（如 Super OTC 19.5 MB）仍在限内，但超大文件需改走 release asset。
