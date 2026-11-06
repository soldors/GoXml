# GXML

GXML 是一个面向 Go 桌面 GUI 的声明式 UI 框架。开发者用 `.gxml` 文件描述界面结构、
组件属性、数据绑定与事件处理，由**编译管线**在构建期完成语法解析、宏展开、
语义分析与代码生成，最终产出 Gio 绘制代码。

设计出发点：Go 做后端、界面用 Web 技术启动太慢。GXML 把界面声明编译成 Go 代码，
避免运行时解析 XML、避免反射、避免每帧重建视图树。

## 核心设计目标

- **声明式 UI**：用 XML 描述结构与组件配置。
- **静态校验**：代码生成前检查属性、绑定路径、类型兼容与事件方法签名。
- **分层编译**：阶段间职责明确，通过结构化中间表示（IR）传递结果，不共享可变状态。
- **契约先行**：语法、类型规则、诊断码与生成行为以规范/契约为准，实现服从契约。
- **可验证**：每个阶段都有可执行门禁；「通过」必须有真实测试结果支撑。
- **增量演进**：先做可编译的最小纵向切片，再扩展控件与语言特性。

## 编译流水线

```text
.gxml 源文件
  → Parser      语法分析 + 精确行列号                    [已实现]
  → Expander    组件 Schema 分类 + <Def>/<Slot> 宏展开    [已实现]
  → Analyzer    绑定路径 / 类型兼容 / 事件签名校验         [已实现]
  → VerifiedDoc 仅在 StatusOK 且无诊断时产出              [已实现]
  → Codegen     生成 Go 源码                             [已实现·最小切片]
  → 编排层      门禁 + 原子提交；失败不写任何文件         [已实现]
  → 运行入口    窗口 + Gio 主循环                        [已实现·最小]
```

当前实现已经接通 CLI 生成与运行时入口；功能实现和完整验收分别记录：

- 前端三阶段（Parser / Expander / Analyzer）已实现并有测试；
- 管线四阶段已串联，失败门禁有实测证据（Codegen 未被调用、不产生文件）；
- `examples/demo` 已能**打开真实窗口**并交互（绑定、事件、`<For>`、`<If>`）；
- CLI 已可用：`goxml generate` 支持单文件与目录批量生成、确定性输出、
  两阶段事务回滚与 `--clean`（见下方使用说明）；
- Grid、Input、If/For、绑定与事件已有生成实现及对应测试；`<Def>`/`<Slot>` 在编译期展开。完整功能验收范围仍以任务卡和实测记录为准，不能由“已有实现”推出全部边界已验收。
- **Image v1 已批准并合入发布**：静态 `Src`/`Fit`、编译期资产校验与 `//go:embed`，见 [ADR-0015](docs/03-架构/adr/ADR-0015-Image控件与编译期资产嵌入.md)。
- **Router 基础与关闭接线已合入发布**：`pkg/runtime/router` + `app.WithOnDestroy(r.View(), r.Shutdown)`；[routed-gallery 教程](examples/gxml/testprojects/routed-gallery/README.md) 与 [随仓验收入口](scripts/verify-gallery-accept.py) 可复跑。MacroOp 转场动画、独立 OSWindow 逃生接口仍未实现；有界调度预算语义未批准。
- **Expanded 基础范围已批准并发布**：流式单子节点、默认等权、显式尺寸优先；Grid、显式权重、自身新装饰继续拒绝，见 [输出契约 §4.9](docs/03-架构/代码生成输出契约.md)。
- **工程与验收入口**：[preferences](examples/gxml/testprojects/crosspackage-preferences/README.md)、[taskboard](examples/gxml/testprojects/crosspackage-taskboard/README.md)、[routed-gallery](examples/gxml/testprojects/routed-gallery/README.md)、[component-layout-lab](examples/gxml/testprojects/component-layout-lab/README.md) 均已进入发布基线。
- Watch、Preview、通用 Lifecycle/Hydration 尚未交付；强类型 ThemeRef 接线仍待批准，不属于现有字体 Theme 便利层。
- 当前发布基线为 `96a7067`；需求/WBS/任务卡的现行状态见 [R25 项目状态总览](docs/04-WBS/2026-11-06-R25-项目状态总览.md)。已实现、限定验收、已合入和已推送分别登记，不以旧审计分组计算完成率；历史证据与原始快照保留。

新用户可从[可构建入门工程](examples/gxml/README.md)开始：使用同一份 ViewModel/GXML 完成真实 CLI 生成、构建和可选手工窗口运行。历史并行开发范围见[10月31日计划](docs/05-开发/2026-10-31-并行开发计划.md)。

2026-10-31 的本地说明曾将 Image 记为待批准候选；该状态已由上述 R18/R20 记录取代。Expanded 基础范围也已于 R25 获批发布；扩展边界按当前输出契约登记。

### goxml generate 使用说明

```text
goxml generate 需 Go module 工程。在仓库根构建：

  go build -o goxml.exe ./cmd/goxml/

生成（单文件或目录，目录递归发现 .gxml）：

  goxml.exe generate ui/home.gxml          # 单文件，输出到同目录
  goxml.exe generate ./ui                  # 目录批量，按相对路径排序处理
  goxml.exe generate ./ui --out-dir ./gen  # 输出映射到指定根目录

参数：
  --check      写盘前用本批最终候选 overlay 执行真实 go build 编译检查；
               全命中也检查，失败时产物/Manifest/成功缓存保旧，--clean 尚未执行
  --clean      按 Manifest 清理「已登记且带保护头」的孤儿产物；
               保护头被改动的文件视为用户接管，绝不删除
  --no-cache   忽略增量缓存（仍更新 Manifest）

退出码：0 成功（含 skipped）；1 用法错误；2 运行失败。

行为保证（详见 docs/03-架构/代码生成输出契约.md）：
  - 同一输入重复生成逐字节一致（SHA-256 实测）；
  - 批次内任一文件失败，整批放弃，已有文件内容与 mtime 原样；
  - 生成登记写入 .goxml/generate-manifest.json，损坏即拒绝清理。

各阶段的实现进度与验收证据，以任务卡为唯一权威来源（见下方导航）。
```

## 开发与验证

需要 Go 1.26 或更高版本（见 `go.mod`）。

仓库提供 `Makefile`，当前可执行的完整门禁是：

```bash
make verify      # = contract-lint + test
```

等价的原生命令（Windows 通常未安装 `make`，用这组即可）：

```bash
go build ./...        # 构建
go vet ./...          # 静态检查
go test ./...         # 全部测试
go test -cover ./...  # 覆盖率
```

`go test ./...` 默认包含原生窗口测试；只做无窗口开发时应按[并行验证记录](docs/06-测试/2026-10-31-并行开发验证记录.md)选择范围，不能把未执行的原生项报为通过。Windows 构建/测试先设置 `CGO_ENABLED=0`；race 需要另有 C 工具链与明确运行条件。

诊断码与文档一致性门禁：

```bash
go test ./internal/compiler/diagdoc/
```

> `Makefile` 中的 `test-golden` / `test-e2e` / `test-generated` / `benchmark` /
> `bench-assert` 目前**故意 `exit 1`**，表示「未实现」而不是「通过」。
> 它们对应的实现落地前，这些目标不得被视为门禁已满足。

运行前端演示：

```bash
go run ./examples/demo
```

## 文档导航

入口是 [文档索引](docs/00-文档索引.md)。以下按用途分组：

### 规范与契约

| 文档 | 内容 |
|---|---|
| [GXML 语言规范](docs/03-架构/GXML语言规范.md) | 语法、布局语义、类型规则、绑定与事件 |
| [GXML 语法速查表](docs/03-架构/语法速查表.md) | 常用写法与常见错误速查（手册性质） |
| [Analyzer 输入输出契约](docs/03-架构/Analyzer输入输出契约.md) | 类型加载、校验规则、语义 IR、类型兼容契约 |
| [代码生成输出契约](docs/03-架构/代码生成输出契约.md) | Codegen 入口、门禁、输出结构与失败语义 |
| [诊断码目录](docs/03-架构/诊断码目录.md) | 全部诊断码的语义、阶段归属与空置号 |
| [运行时线程模型](docs/03-架构/运行时线程模型.md) | Gio 线程约束与回调调度 |
| [性能预算与优化策略](docs/03-架构/性能预算与优化策略.md) | 性能不变量、基准方法与证据等级 |

### 决策记录

架构决策记录位于 [docs/03-架构/adr/](docs/03-架构/adr/)，各文件标明批准状态；未批准草案不是开发许可。
其中 ADR-0001 说明「编译期 GXML → Gio 零抽象开销」的整体取舍。
ADR-0013 主题草稿仍待裁决；ADR-0015 Image 已批准，范围以各自批准状态为准。

### 进度与证据

| 文档 | 用途 |
|---|---|
| [WBS 总表](docs/04-WBS/WBS总表.md) | 任务清单、依赖与开发流程规则 |
| [任务卡](docs/04-WBS/任务卡/) | **实现状态的唯一权威来源**：进度、验收、证据 |
| [测试与质量门禁规范](docs/06-测试/测试与质量门禁规范.md) | 测试分层、覆盖率要求、门禁规则 |
| [Analyzer 验收报告](docs/06-测试/Analyzer验收报告.md) | 阶段性验收记录与**尚未验证**的事项（§5） |

## 当前状态与已知限制

本 README **不维护**测试数量、覆盖率或完成度——这些数据变化频繁，
分散在多处必然滞后。需要当前状态时请查 [R25 总览](docs/04-WBS/2026-11-06-R25-项目状态总览.md) 及对应任务卡。

几项稳定的已知限制：

| 限制 | 说明 |
|---|---|
| Codegen 分阶段验收 | 绑定/事件/布局/If/For/Grid/Input/Image/Expanded基础已有实现与测试；完整边界和性能验收不能仅凭生成成功确认 |
| Image v1 范围 | 静态 Src/Fit、PNG/JPEG、包内相对资产；绑定 Src/Fit、SVG、网络图不在已批准范围 |
| CLI 已可用（generate） | 单文件/目录生成、`--out-dir`、`--clean`、`--check`（提交前编译）、增量缓存；Watch 未做（归 TASK-DEVX-001/002） |
| 窗口渲染未自动验证 | 已有离屏像素/几何/事件语义和 Router 原生事件循环自动化；真实视觉仍需人工确认，R20 人工视觉与原生 race 结论限其声明的冻结提交 |
| 本批仅 Windows 验证 | 历史 Linux 原生编译记录见平台矩阵；不外推 Linux/macOS 运行结果 |
| 字体整体正式验收未通过 | M1 阈值、跨平台、第二成功 family 未闭环；比较机制与真实 App M1 已有历史证据但本轮无新采样，见 [R23-B 差距矩阵](docs/03-架构/字体正式验收差距矩阵-R23B.md) |
| Router 预算语义未批准 | 候选 `35c9ff3` 技术范围通过、未进主库；主库沿用同帧排空 |
| `<Def Events>` 不传播事件 | v1 只做契约声明，调用点处理器签名不被校验 |

更完整的未验证清单见 [Analyzer 验收报告 §5](docs/06-测试/Analyzer验收报告.md)。

## 开发约定

- 动手前先读相关规范、契约与任务卡。
- **契约先行**：涉及跨阶段数据结构、诊断码、语法或行为语义的改动，先改契约与测试，再改实现。
- 新增诊断码须遵循[诊断码目录](docs/03-架构/诊断码目录.md)的取号规则，并更新门禁。
- 不以文档描述代替测试证据；不以单元测试通过代替端到端门禁验收。
- 发现规范、实现与测试不一致时，先确认哪一份是权威契约，再修复并补回归测试。

## 贡献前自检

1. `gofmt -l .` 无输出。
2. `go build ./... && go vet ./... && go test ./...` 全部通过（或 `make verify`）。
3. `go test ./internal/compiler/diagdoc/` 通过（诊断码与目录一致）。
4. 新增或修改的行为有对应测试；诊断码有对应夹具或单元测试。
5. 文档链接可解析；状态描述指向任务卡而非另抄一份。
