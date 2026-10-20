# TASK-FONT-001-A：系统字体探测与 `FontProvider.Resolve`（边界已冻结）

## 元信息

- 状态：**实现已落地（2026-10-20）**，门禁复跑中；B/C/D 仍未开始
- 父卡：[TASK-FONT-001.md](TASK-FONT-001.md)
- 前置：`TASK-THEME-SEAM-001` ✅（已完成 6/6：`pkg/runtime/theme` 存在、
  `FontProvider` 契约冻结、A..D 占位用例在位、`app.Run` 走 `theme.NewTheme()`）
- 规范依据：[字体与文本渲染策略.md](../../03-架构/字体与文本渲染策略.md) §1.2 / §2.3、
  [ADR-0012](../../03-架构/adr/ADR-0012-主题字体边界与Spacer主轴默认值.md) 决策 1（路线 B）、
  `contracts/runtime_api.go`
- 立卡日期：2026-10-20

## 1. 范围

### 1.1 做

| 项 | 交付 |
|---|---|
| 候选路径常量 | 三平台 CJK 候选字体路径（规范 §2.3），放 `pkg/platform/font`，按 build tag 隔离 |
| 探测流程 | 存在性 → 解析（`opentype.ParseCollection`）→ 首个可解析者胜；解析失败继续下一候选并记录原因 |
| `Resolve` | 实现 `contracts.FontProvider`：语义、缓存、`ErrFontNotFound` |
| 诊断 | `ProbeReport`：每个候选的 `path / status / err`，可被测试断言、可被打印排查 |
| 超时 | 可注入的 opener + deadline；不得因慢文件系统阻塞启动 |
| 装配 | 探测成功时把命中集合装进 `text.Shaper`；失败时保持现状并让诊断可见（见 §2 约束 10） |

### 1.2 不做（明确排除）

- 嵌入兜底 → **B**；按字形覆盖的 fallback 链 → **C**；懒加载与失败强制诊断 → **D**；IME → `WIDGET-001`。
- **不在实现里暗改 `Resolve` 签名**：若实测证明 `string` 不足以表达需求，
  先记录证据，再走 ADR 变更契约（见 §4.3）。
- **不动生成代码与 Codegen**：路线 B 不变，`Layout` 签名仍是
  `Layout(gtx layout.Context, th *material.Theme)`（由 docgate 锁住）。

### 1.3 为什么 B/C/D 不并行开工

1. **B 需要 A 的结论**：系统字体缺失时的处理边界（哪些状态算"缺失"、诊断长什么样）
   要先由 A 定下来，嵌入兜底才知道该在什么时候接管。
2. **C 依赖可靠的解析与加载基础**：fallback 链按字形覆盖选择，前提是 A 能稳定地
   把"字体集合标识"解析到真实 face。
3. **D 过早实施会把三件事混成一个难以验收的任务**：字体发现（A）、字体加载、
   shaping 三者耦合后，任何一个失败都无法归因。
4. **A 是 `Resolve(family string) (string, error)` 这个冻结接口的第一次实测**。
   够不够用必须在这里给出结论；不够就走契约变更，而不是在实现中绕过。

## 2. 实施边界（硬约束）

| # | 约束 | 依据 | 违约后果 |
|---|---|---|---|
| 1 | 跨平台差异只允许出现在 `pkg/platform/font` 与 `pkg/runtime/theme` | 规范 §1.6 | 平台细节泄漏到其它包/生成代码 → 违反不变量 6 |
| 2 | 不得把执行 `fc-match` 等外部命令作为**实现路径**（仅可作诊断信息） | 规范 §2.3 | 与平台矩阵「不伪装平台实现」冲突 |
| 3 | 探测必须有超时，且不得阻塞首帧；耗时实测登记 | 规范 §2.3 / §3.2（≤20ms 量级） | 冷启动预算被吃掉 |
| 4 | 文件存在但解析失败 → **继续**下一候选并记录原因 | 规范 §2.3 | 单点损坏字体导致整体失败 |
| 5 | 全部失败 → 返回 `ErrFontNotFound`（`errors.Is` 可判定）；**不得**返回空串 + nil，**不得**静默回退到「看起来能用」的字体 | `contracts.FontProvider`、规范 §1.2 | 缺字形静默渲染为空白/豆腐块——正是本契约要禁止的失败模式 |
| 6 | 同一 family 的解析结果必须可缓存：二次调用不重新扫描磁盘（用注入计数器证明） | 契约 | 违反「不允许每次调用重新扫描磁盘」 |
| 7 | 调用时机：主题构建期或首次 shaping；**不得每帧调用** | 性能预算 INV-PERF-2（`allocs/op == 0`） | 稳定帧分配红线被破 |
| 8 | 生成代码与 Codegen 不变；`Layout` 签名保持 `th *material.Theme` | ADR-0012 决策 1 | docgate `TestGeneratedLayoutSignatureMatchesRouteB` 报红 |
| 9 | 占位用例 `TestFontProbe` 由 `t.Skip` 换成真实断言，**函数名保持不变** | `TASK-THEME-SEAM-001-6` | 改名会让验收命令退化为空转，docgate 报红 |
| 10 | **A 阶段的已知缺口**：`Resolve` 失败时 `NewTheme()` 仍沿用 Gio 默认 Shaper（现状），只保证「诊断可见」 | 见下方说明 | 收紧时机 = B 落地后；不收紧则长期违反规范 §1.2 |

**关于约束 10 的取舍说明**：契约禁止「静默回退到看起来能用的默认字体」，但 A 阶段
并没有兜底可回退（嵌入子集属 B）。若在 A 就强制「探测失败 = 启动失败」，等于让所有
无 CJK 字体的精简环境直接不可用，而它们本可以在 B 之后被兜底救回。因此 A 的取舍是：
**错误语义严格（Resolve 必返 `ErrFontNotFound`）+ 行为暂不收紧（NewTheme 保持现状）
+ 诊断必须可见**，并把「B 完成后必须收紧」写成本卡的未闭合项（§7 风险 1）。

## 3. 技术依据（Gio v0.10.3，已核对源码）

| API | 签名/语义 | 用途 |
|---|---|---|
| `gioui.org/font/opentype.Parse` | `Parse(src []byte) (Face, error)` | 单字体文件 |
| `gioui.org/font/opentype.ParseCollection` | `ParseCollection(src []byte) ([]giofont.FontFace, error)` | `.ttc` 集合（Windows/macOS 多为 ttc） |
| `gioui.org/text.NewShaper` | `NewShaper(opts ...ShaperOption) *Shaper` | 装配 |
| `gioui.org/text.WithCollection` | `WithCollection([]FontFace) ShaperOption` | 注入探测到的字体集合 |
| `gioui.org/text.NoSystemFonts` | `ShaperOption` | 关闭系统字体加载（**零值 = 开启**） |
| `gioui.org/font.Font` | `{Typeface, Style, Weight}` | 字体族/风格/权重 |
| `material.NewTheme()` | `&Theme{Shaper: &text.Shaper{}}` | 现状：零值 Shaper → 隐式加载系统字体 |

> 「零值 `text.Shaper{}` = 开启系统字体」是本卡的重要前提：它意味着**现状能显示中文，
> 靠的是系统字体隐式回退，不是框架保证**（`TASK-FONT-001` §1 实测）。A 要把这条
> 隐式依赖变成显式、可诊断、可失败的行为。

## 4. `Resolve` 语义（A 必须给出判定）

### 4.1 入参：合法族名集合（v1 冻结）

| family | 含义 | 命中来源 |
|---|---|---|
| `"Body"` | 正文拉丁字体 | 系统探测 / B 之后的嵌入兜底 |
| `"Mono"` | 等宽 | 同上 |
| `"CJK"` | 汉字（含日文汉字、韩文） | 规范 §2.3 的候选清单 |

- 入参是**逻辑字体族名**，不是文件路径；
- **未知族名返回 `ErrFontNotFound`**，不做猜测、不做前缀匹配。

### 4.2 返回值：字体集合标识

- 格式（v1 建议）：`system:<文件名>#<集合内序号>`、B 之后增加 `embedded:<名>`；
- 要求：非空、稳定、可比较、可用于诊断；
- **禁止**空串 + nil。

### 4.3 「`string` 够不够用」的判定结果（A 实现后给出）

**结论：在 v1 语义下够用**（2026-10-20）。

- 证据：三族名只需「族」一维；命中集合由 `Provider.Faces(id)` 反查得到，
  标识本身不需要携带更多信息（A 卡评审要点 1 已落地为同一份缓存）。
- **限制一并登记**：标识**不含 Style / Weight**。若 C（fallback 链）需要按字重/风格
  选择 face，或 B 需要区分更多来源维度，`string` 就不够——届时按下表走 ADR。

| 结论 | 处理 |
|---|---|
| 够用（**当前结论**） | 契约保持不变；本表即为书面结论 |
| 不够用 | **记录证据**（例如需要 `Style`/`Weight`/variant 维度，或需要返回多个 face），
  然后走 **ADR** 变更 `contracts.FontProvider`，同步 `contracts/runtime_api.go` 注释、
  字体规范 §6.3、`make contract-lint` 等价命令；**不得**在实现中绕过契约 |

## 5. 验收矩阵

命令统一为 `go test -run TestFontProbe ./pkg/runtime/theme/...`（命中真实用例；
`SKIP` 计为未执行，**不计入通过数**）。

| 测试名 | 覆盖点 | 证据等级 |
|---|---|---|
| `TestFontProbeCandidatePaths` | 三平台候选表非空、顺序符合规范 §2.3、不含外部命令依赖 | 逻辑（三平台都跑，GOOS 注入） |
| `TestFontProbeContinuesOnParseFailure` | 候选文件存在但不是有效字体 → 记录原因并**继续** → 命中后续候选 | 逻辑（临时文件构造） |
| `TestFontProbeTimeoutAndDiagnostics` | 注入慢 opener + deadline → 超时后放弃剩余候选，诊断记录 `timeout`，总耗时登记 | 逻辑 |
| `TestFontProbeErrFontNotFound` | 全部候选失败 → `errors.Is(err, contracts.ErrFontNotFound)`；且**不**返回空串 + nil | 逻辑 |
| `TestFontProbeResolveCaching` | 同一 family 二次 `Resolve` 不再扫描磁盘（计数注入），结果相同 | 逻辑 |
| `TestFontProbeWindowsSystemFonts` | 真实 `C:\Windows\Fonts` 探测 + 解析成功 + 诊断可打印 | **实测（仅 GOOS=windows）**；其它平台 `SKIP` 并注明 |
| `TestFontProbeShaperAssembly` | 命中后装配的 Shaper 能解析出 CJK 字形（离屏渲染/字形查询，与 `TASK-FONT-001` §1 的实测方法一致） | **实测（仅 GOOS=windows）** |

**平台差异的诚实分层（强制）**：

### 5.1 实测结果（2026-10-20，GOOS=windows）

| 测试 | 结果 | 证据等级 |
|---|---|---|
| `TestFontProbeCandidatePaths` | PASS | 逻辑（三平台表均被断言） |
| `TestFontProbeContinuesOnParseFailure` | PASS（坏文件 → parse_failed 并记录原因 → 命中下一个） | 逻辑 |
| `TestFontProbeTimeoutAndDiagnostics` | PASS（Timeout=5ms / Elapsed=41ms / Scanned=1 / 剩余 skipped_timeout） | 逻辑 |
| `TestFontProbeErrFontNotFound` | PASS | 逻辑 |
| `TestFontProbeResolveCaching` | PASS（二次 Resolve 不再读盘，同一份集合） | 逻辑 |
| `TestFontProbeRejectsUnknownAndUnimplementedFamilies` | PASS（未知/未实现族名不触发扫描） | 逻辑 |
| `TestFontProbeWindowsSystemFonts` | PASS（命中 `C:\Windows\Fonts\msyh.ttc`，2 faces，110.5ms） | **实测** |
| `TestFontProbeShaperAssembly` | PASS（「中」的 `NominalGlyph` ≠ 0） | **实测** |
| `TestEmbeddedFallback` / `TestFontFallbackChain` / `TestFontLazyLoad` | **SKIP**（B/C/D 未实现） | — |

| 平台 | 本卡能给出的证据 | 标记 |
|---|---|---|
| Windows | 本机真机实测（有 CJK 字体） | **实测** |
| macOS | 仅候选表与流程的逻辑单测；无 Apple 硬件 | **待验证** |
| Linux | 仅候选表与流程的逻辑单测；真实发行版未跑 | **待验证** |

任何报告、卡片、CHANGELOG 都**不得**用单平台实测宣称全平台通过；
待验证项必须在验收表里显式写明，不得留白。

## 6. 门禁

| 门禁 | 命令 |
|---|---|
| A 本体 | `go test -run TestFontProbe ./pkg/runtime/theme/...` |
| 运行时 | `go test -count=1 ./pkg/runtime/...` |
| 构建 / 静态 | `go build ./...`、`go vet ./...`、`gofmt -l pkg internal contracts` |
| 契约 | `make contract-lint`（当前会话 `make` 不可用 → 等价：`CGO_ENABLED=0 go vet ./contracts/...` + `CGO_ENABLED=0 go test ./contracts/...`） |
| 文档门禁 | `go test -count=1 ./internal/docgate/`（含 `TestFont001AcceptanceCommandsAreNotVacuous`、`TestThemePackageBoundary`、`TestGeneratedLayoutSignatureMatchesRouteB`） |

## 7. 已知风险

1. **约束 10 是未闭合项**：A 阶段探测失败不强制失败，B 落地后必须收紧，否则长期违反规范 §1.2。
2. **`string` 标识可能不够表达**（§4.3）：当前结论「v1 够用」，限制是不含 Style/Weight；
   需要改契约必须走 ADR。
3. **装配 Shaper 会改变拉丁文本度量**（已实测）：px=16 下 `"ABCD"` 的 advance
   `45:26 → 44:13`。原因是 Gio 把 collection 中的 typeface 作为 `defaultFaces`
   查询首选（`text/gotext.go:392`）。全量测试用于判断「变化」还是「回归」。
4. **首次探测耗时 110ms，超出规范 §3.2 的 20ms 预算**（Windows 实测 msyh.ttc）。
   这是 **D（懒加载）必须存在的实测证据**；A 的缓解只是「结果缓存，进程内一次」。
5. **并发工作树**：同一工作树有多条线共写，禁用 `git add -A`（dev-log 10-20 §22.7 / §22.13）。
6. **对 §1.1 的一处偏离**：候选表未用 build tag 隔离，改为三表同文件 +
   `CandidatesFor(goos)`（理由见 dev-log §24.7 与 `pkg/platform/font` 包注释）。

## 8. 交接记录

| 项目 | 内容 |
|---|---|
| 当前提交 SHA | 见 dev-log 2026-10-20 §24 |
| 已完成 | 边界冻结 + **实现落地**：`pkg/platform/font`、`pkg/runtime/theme/font.go`、
  `NewThemeWithReport()` 装配、验收矩阵 7 项 + 族名严格性 1 项（Windows 实测 2 项、逻辑 6 项） |
| 未完成 | B 嵌入兜底 / C fallback 链 / D 懒加载（仍是显式 skip 占位）；
  **约束 10 的收紧**（B 后）；macOS / Linux 真机验证 |
| 已运行命令及结果 | `go test -count=1 -run TestFontProbe ./pkg/runtime/theme/` → ok（8 项）；
  `go test ./pkg/runtime/...` → ok；`go vet ./pkg/...` → 无输出；
  `go test ./internal/docgate/ ./contracts/` → ok；
  全量 `go test ./...` 中 codegen 包在 600s 默认超时被打断（并发负载，基线 558s），
  单独以 `-timeout 30m` 复跑验证中，结果登记在 dev-log §24.9 |
| 已知失败或风险 | 见 §7 |
| 下一位接手人 | B（嵌入兜底）；承接约束 10 的收紧义务 |
