# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Fixed
- **P0-2 `Fill` 语义错误**：原规范把 `Fill` 等同于「生成 `Flexed(1)`」，但 `Flexed` 只作用于主轴——在 `<Column Width="Fill">` 里完全无效。现改为语言层语义（「占满父容器在对应方向上的可用空间」），由 Codegen 按主轴/交叉轴分别实现。同步修正 `TASK-CODEGEN-001` 中错误的映射指令。
- **P0-1 v1 移除 `AlignSelf`（子项级对齐）**：原表写「同 `Align`」，语义与实现都不成立。核查 Gio 后发现 `layout.FlexChild` 没有对齐字段，`Flex.Alignment` 是容器级——实现 `AlignSelf` 只能给每个子项包一层 `Stack`，代价是多一层嵌套、破坏静态子树固化、每帧多一次闭包分配（实测约 11× 慢）。而 `AlignSelf` 的价值恰是「省一层嵌套」，用嵌套换嵌套净收益为零，故 v1 不提供，需要时用嵌套 `<Row>`/`<Column>` 表达。详见 ADR-0008 §2.6.1。
- **P0-3 `Baseline` 适用范围未收紧**：原规范只禁止 `<Column>`，`<Grid>` 未定义。现限定仅 `<Row Align="Baseline">` 合法，其余报 `GXML-ATTR-003`。
- **放弃 `Baseline == 0` 的隐式兜底**：原方案让 Codegen 生成 `if d.Baseline == 0 { d.Baseline = d.Size.Y }`，会把「基线在顶部」误改成「底部基线」。现明确 `Baseline == 0` 是合法值，Codegen 不做任何改写，基线正确性由控件实现方负责。
- **尺寸数值格式未校验**：`strconv.ParseFloat` 接受 `1e5`、`Inf`、`NaN`，与规范「只接受十进制」冲突。现显式拒绝指数形式与单位后缀。
- 补全尺寸约束冲突规则：无界约束下 `Fill` 退化为自然尺寸；固定尺寸超出父容器不裁剪不报错；多个 `Fill` 兄弟均分剩余空间。
- **Grid 补位必要性**：实测确认不补位时缺失列的宽度被弹性列吸收（400 → 500），且 `Gap` 计数少一个，导致列错位。补位改为强制，且只做 Codegen 内部布局项，不写回 IR。
- **弹性取整非保证项**：实测发现 Gio 的弹性分配**不保证用满可用宽度**（116px 五等分合计 115）。规范改为只承诺「不超过可用宽度」，明确 v1 不做补偿。
- **§3.1.5 与 §3.1.6/§3.1.7 的三处矛盾**：§3.1.5 原表允许 `<Grid>` 单元格设 `Width`（与 §3.1.6 的禁止冲突）、把 `Grid` 内 `Height="Fill"` 定义为填充行高（与「行高由内容决定，填充基准循环」冲突）、把无界 `Fill` 一律描述为退化（与主轴 `Fill` 仍参与弹性分配冲突）。现 §3.1.5 收敛为摘要并指向 §3.1.7 作为唯一权威。
- **`<Page>` 布局语义缺失**：规范从未定义 `<Page>` 如何排列多个子节点。现明确等价于没有 `Gap`/`Padding` 的 `<Column>`（垂直排列），避免按 `Stack` 叠放导致的静默覆盖。

### Added
- **ADR-0001 / 0002 / 0003 重构**：三份此前只有 5–6 行结论的根基文档补齐为完整决策记录，统一采用「证据分级」结构：项目实测 / 项目证据 / 外部事实 / 设计判断四类分离，外部框架事实附来源且明确限定适用范围。
- **ADR-0001**：加入构建实测（Windows 零 CGO，Linux/macOS 必须 CGO）与性能量级证据，并声明 11×/5.9× **当前不可复现**（仓库无 Benchmark 代码，复现任务为 TASK-BENCH-001-5/9）。外部事实只引用 Gio 官方 README、Fyne Quick Start、Wails How-does-it-work，并记录 Fyne「C 编译器仅开发环境需要」这一官方限定。
- **ADR-0002**：记录四组语法决议、`@` 非合法 XML 字符的实测、`Clik` 拼写错误绝不退化为事件，以及 Parser/Expander/Analyzer 分层是被两个具体发现逼出来的（Parser 不能内置控件白名单；适用性检查需 Schema 知识）。
- **ADR-0003**：明确浅对比的**适用边界与漏更新清单**（指针别名、原地修改、Map 不参与 Diff），并声明 ShadowState/Dirty Mask 尚未实现。
- **规范 §7.2.2 补充 `<Slot/>` 单子节点限制的完整论证**：插槽可能处于任意布局上下文，自动包裹会改变布局语义（`<Row><Icon/><Slot/></Row>` 注入多子节点会被强制纵向堆叠且无法覆盖）。
- **TASK-BENCH-001-9**：新增抽象开销基准子任务，使 5.9× 有明确的复现路径。
- **尺寸组合与约束传递冻结（P1-5）**：规范新增 §3.1.7，定义主轴/交叉轴判定（含 `<Page>` 等价于无 `Gap`/`Padding` 的 `<Column>`）、单方向解析、无界约束（阈值 `1 << 20`）、嵌套传递与 9 种 `Width`×`Height` 组合矩阵。ADR-0008 新增 §2.8 记录选项依据。
- **`internal/compiler/sizelayout`**：尺寸决策的可执行形式（`Resolve` / `ResolveMainCross`），Codegen 应直接调用而非自行解释规范。13 个测试，覆盖率 94.4%。
- **Grid 布局模型冻结（P1-4）**：规范 §3.1.6 与 ADR-0008 §2.7 完整定义行优先排列、最后一行补位、列宽计算、`Gap` 双轴、单元格约束与取整规则。
- **`internal/compiler/grid`**：列宽计算的规范参考实现（oracle），100% 覆盖。18 个测试，其中 `TestMatchesGioMeasurements` 逐项复现 Gio v0.10.3 的实测输出。
- **Grid 输入校验**：`<Grid>` 无子节点、直接子项设置 `Width` 均报 `GXML-ATTR-003`。

- **事件语法与 Schema 分类机制**（规范 §5 重写）。事件由 `@Click` 改为 `Click`（`@` 非合法 XML 属性名字符，标准库直接报语法错误）；属性/事件由组件类型化 Schema 区分，未知名称**绝不退化为事件**。
- **`internal/compiler/parser`**：GXML 语法分析器。复用 `encoding/xml` 处理 XML 规则，用 `InputOffset()` 维护字节偏移到行列的映射，并对起始标签原文重扫以获得**每个属性**的精确 `SourceSpan`（`xml.Attr` 本身不带位置）。列号按 rune 计数，中文与 ASCII 混排定位正确。93 个测试，覆盖率 **86.8%**。
- **`internal/compiler/expander`**：语义分类、组件契约与宏展开层。
  - 三层分类（全局布局属性 → Schema.Properties → Schema.Events → 未知报错），分类与适用性校验分离（`Columns` 出现在 `<Button>` 上报「不适用」而非「未知」）。
  - **`<Def>`/`<Slot>` 宏展开**：Prop 值替换（绑定/静态/路径拼接三种情况）、插槽单子节点替换、克隆隔离、递归检测。
  - 73 个测试，覆盖率 **91.6%**。
- **`contracts` 新增**：`RawAttr`/`RawAttrList`（Parser 输出的未分类属性）、`AttrKind`、`EventSchema`/`EventParam`（组件事件契约，与处理器调用 `EventBinding` 语义分离）、`DefClause.Events` 与 `Prop`/`Event` 查找。38 个测试，覆盖率 **92.9%**。
- **`<Def Events>` 声明**：`Events="Click; Open(id:string)"`，支持带类型参数签名。
- **`ADR-0008`：布局属性模型**。新增 6 个属性（`Align`/`Justify`/`Gap`/`Width`/`Height`/`Columns`）与 `<Grid>` 标签，附四项实测依据（Gap 性能无差异、Auto 必然破坏 0 分配、Grid 性能无差异、Baseline 仅横向生效）。
- **`contracts/layout_test.go`**：布局枚举与尺寸规格测试。
- **`contracts/ir_schema.go` 新增布局类型**：`CrossAlign`、`MainJustify`、`SizeMode`/`SizeSpec`、`GridColumn` 及对应 `Parse*` 函数与 `NodeIR` 字段。
- **`go.mod`**（`module github.com/goxml/goxml`，`go 1.26.0`）。仓库首次具备可编译基线。
- **`.gitignore`**：覆盖生成产物（`*_gen.go`）、工具链目录（`.goxml/`）、构建产物、编辑器与 OS 文件。
- 初始化 Goxml 顶层架构设计、ADR 决策集合与全阶段 WBS。
- 新增 `ADR-0007`：Zero-CGO 降级为 Windows 侧约束，Linux/macOS 接受 CGO。附真机实测证据（Linux 穷举 5 种 build tag 组合均失败；`CGO_ENABLED=1` 构建成功；`readelf -d` 依赖清单）。
- 新增 `docs/03-架构/平台与平台依赖矩阵.md`，取代 `Zero-CGO平台兼容性矩阵.md`：按平台区分 CGO 开关、系统构建依赖与运行时依赖。
- 新增踩坑 PIT-008（误以为三端可零 CGO 构建）、PIT-009（误信 Gio macOS 用 purego）。
- 新增 `docs/03-架构/性能预算与优化策略.md`：实测基线（渲染规模、闭包分配、文本 shaping、接口派发）、6 条性能不变量（INV-PERF-1~6）、性能预算与 8 条反模式。
- 新增 `docs/03-架构/字体与文本渲染策略.md`：CJK 字体探测、嵌入兜底、fallback 链、IME 与 shaping 缓存策略。
- 新增 `ADR-0006`：列表虚拟化与静态子树宏固化的编译期决策。
- 新增 `TASK-FONT-001`：CJK 字体与 IME 工作项（此前 WBS 完全缺失）。
- 新增踩坑记录 PIT-005（大列表全量渲染）、PIT-006（每帧拼接文本）、PIT-007（默认字体缺 CJK）。

### Changed
- **布局语义扩展（ADR-0008）**：`GXML语言规范.md` §3.1 重写为 4 个子节（通用属性/对齐分布/尺寸/Grid），新增轴向感知默认值表（`<Row>`→`Center`、`<Column>`→`Start`）；`Spacing` 属性更名为 `Gap`；§9 增补不支持项；§11 增加变更记录表。
- `AOT状态Diff语义.md`：Dirty 传播表将 `Spacing` 改为 `Gap`，并新增 `Align`/`Justify`→`DirtyLayout`、`Width`/`Height`/`Columns`→`DirtyLayout|DirtyChildren`。
- `TASK-CODEGEN-001`：新增 §1.3 布局契约与 6 个子任务（TASK-CODEGEN-001-8~13），重点标注 `Center→Middle` 与 `Start→SpaceEnd` 两处反向映射。
- `contracts/ir_schema.go` 全面重构**，落实 `GXML语言规范.md` §10 的全部要求：新增 `NodeKind` 枚举（含 `IsStructural()`）、`Position`/`SourceSpan`、结构化 `IfClause`/`ForClause`/`DefClause`、分段绑定路径 `BindingVal.Segments`、`DefProp`+`PropType`、带 `Span` 的 `EventBinding`/`EventArg`。
  - **修复 `ForKey` 缺失**：新增 `ForClause.Key` 与 `HasKey`（Key 零值无法区分「未提供」），此缺陷会导致循环内控件状态串位原样复现（机理见 `AOT状态Diff语义.md` §5.1）。
  - **修复绑定路径不可分段**：`BindingVal` 由单个 `Path string` 改为 `Segments []string` + `Path()` 回显。
  - `CompilerError` 增加 `Code`（对齐 §8 的 `GXML-BIND-001` 格式）、`EndLine`/`EndCol`、`Span()`、`NewError()`；新增 7 个诊断码前缀常量。
- **Go 版本要求修正为 1.24+**：原文档写 1.23+，但 `gioui.org v0.10.3` 的 `go.mod` 明确要求 `go 1.24.0`，按原文档安装会导致构建直接失败。项目基线取 1.26。
- `Makefile`：`test` 改为 `go test ./...`（原显式列举 `./internal/...` 与 `./pkg/...`，在目录尚不存在时会使门禁卡死）；新增 `test-all`；`verify` 移除尚未实现的 `bench-assert`（否则永久为红）。
- `TASK-CONTRACT-001` 标记为 **Done**，附完成对照表与验收结果。
- **`Zero-CGO` 从全平台强制红线降级为 Windows 侧约束**（ADR-0007）：原「严禁 CGO、三端 `CGO_ENABLED=0` 一键交叉编译」已被真机实测否决，现行策略为 Windows 零 CGO、Linux/macOS 开 CGO。
- `AGENT.md`：红线 1 重写为「Windows 侧 Zero-CGO 红线」；技术栈删除与 Gio 实际依赖不符的 purego 表述；验证命令表把 `test-cross-compile` 拆为 `build-windows`/`build-linux`/`build-darwin`；架构红线由四条增至五条（新增零分配红线）；性能目标补入 `allocs/op == 0` 与万行列表预算。
- `AGENT.md` PIT-001：**修正原建议**——实测证明「复用切片缓冲区」仅省约 4%，真正解法是编译期展开静态子树消除闭包（11×）。
- `Makefile`：修复 `make test` 中 `-race` 与 `CGO_ENABLED=0` 的硬冲突，拆出独立 `test-race`；移除 `test-cross-compile`，改为 `build-windows`（零 CGO）/`build-linux`/`build-darwin`（CGO）。
- `环境搭建.md`：新增 §4「平台前置条件」——Linux 13 个 `-dev` 包、macOS Xcode CLT、Windows 无需工具链，并补充对应故障排查。
- `测试与质量门禁规范.md`：编译矩阵按平台区分 CGO 开关；CI 规则改为分平台 Runner；`TC-CI-01` 改为分平台构建门禁；§2.7 增加 6 条可自动断言的性能门禁。
- `发布单.md`：产物表新增「原生依赖」列，并列出 Linux 产物实测需要的 10 个系统库与终端用户安装命令。
- `平台与平台依赖矩阵.md`：取代原 `Zero-CGO平台兼容性矩阵.md`，按平台给出 CGO 开关、构建依赖、运行依赖与验证矩阵。
- `架构说明.md`：新增 §5「平台后端边界」，说明 Goxml 核心产出（`op`/`layout`/`widget`）三端均为纯 Go，CGO 仅集中在 `app`/`gpu` 最后一层。
- `ADR-0005`：标记为「已被 ADR-0007 部分取代」，保留以记录决策演变。
- `GXML语言规范.md`：`<For>` 新增可选 `Virtualize` / `ItemExtent` 属性（向后兼容扩展）；明确字体不属于 GXML 语法。
- `AOT状态Diff语义.md`：新增 §5.1.1 虚拟化可见性窗口；§6.1 补入静态子树固化的强制性与量化依据；验收项增至 19 条。
- `代码生成输出契约.md`：新增 §15 生成代码的性能约束；验收项增至 19 条。
- `WBS总表.md`、`TASK-CODEGEN-001`、`TASK-BENCH-001`、`TASK-OS-001`、`TASK-OS-002`：补充静态子树固化、虚拟化、零分配、分平台构建与依赖断言子任务。

### Known Issues
- **macOS 完全未验证**：无 Apple 硬件，v1 仅保证功能与设计层面的正确性，不承诺运行验证通过。
- **Linux 运行 smoke test 未执行**：编译路径已在 WSL2 Debian 13 验证通过，但窗口实际渲染、剪贴板、文件弹窗、托盘尚未在真实桌面会话中测试。
- **`internal/`、`pkg/`、`cmd/` 尚未创建**：编译器与运行时实现全部待开发，`make test` 目前只覆盖 `contracts/`。
- **`-race` 在本机不可用**：Windows 缺少 C 工具链；需在 Linux 或安装 MinGW 后执行。
- **布局原语不完备**：v1 内置布局标签仅 `Column`/`Row`/`Spacer`/`Expanded`，且未暴露 cross-axis 对齐、main-axis 分布、显式宽高与尺寸约束。Gio 的 `layout.Flex` 本身已提供 `Alignment`、`Spacing`、`Flexed(weight)`，但 GXML 语法尚未映射这些能力，导致无法表达"固定 + 弹性 + 固定"这类组合。这是**框架层**缺口，待决策。
- **组件库尚未建立**：`Divider`、`Checkbox`、`Radio`、`Select`、`Table`、`Tabs`、`ProgressBar`、`Scroll` 等属于 `pkg/widgets` 组件层，**不涉及 GXML 语法变更**，不在布局原语讨论范围内。当前 `TASK-WIDGET-001` 仅规划 ContextMenu/Modal/DataGrid。
- **无双向绑定**：GXML v1 明确不支持，输入控件的回写需手写对应数量的事件处理函数。待决策是否进入 v1.1。
- **无 CI 配置**：文档描述的 CI 矩阵尚未落地为实际配置文件。
