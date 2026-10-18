# AGENT.md: Goxml Multi-AI Engineering Directives

> 本文件是所有参与 **Goxml** 框架开发、重构、测试与代码审查的 AI Agent（如 Claude Code、Cursor、Windsurf、Aider 等）的**最高行为准则与架构约束协议**。在执行任何代码改动、测试编写或架构调整前，必须严格通读并无条件恪守本规范。

## 1. 项目定位与核心指标 (Project Identity)

- **项目名称**：Goxml
- **业务定位**：极致性能、**通用**声明式 Go 原生桌面 GUI 框架。将 `.gxml` 标记语言（Vue 风格属性/事件绑定 + Svelte 风格结构化控制流）在编译期（AOT）直接转译为纯 Go / Gio 立即模式 GPU 渲染代码。
- **定位边界（重要）**：

  Goxml 是**通用框架**，面向任意 Go 桌面应用，**不针对任何特定应用领域**（管理后台、编辑器、工具软件或游戏工具等）做裁剪或特殊优化。

  由此派生三条设计原则：

  1. **布局原语必须正交、完备、语义最小**。判断标准是"能否表达绝大多数界面且组合无歧义"，**不是**"能否快速做出某类应用"。
  2. **框架层与组件层分离**。布局原语、绑定、状态 Diff、路由属框架层（改动需 ADR）；`Divider`/`Checkbox`/`Table`/`DataGrid` 等属 `pkg/widgets` 组件层（可直接新增，不改变 GXML 语义）。
  3. **不得以未经证实的应用场景作为设计依据**。若某决策需要场景假设，必须先由项目所有者确认该场景，并在文档中标注其来源；不得由 AI 自行推定。

- **技术栈规范**：
  - **核心语言**：Go **1.24+**（`gioui.org v0.10.3` 的 `go.mod` 要求 `go 1.24.0`，低于此版本无法构建）
  - **渲染底座**：Gio (`gioui.org`) GPU 立即模式图形库
  - **代码生成**：`text/template` + `go/format`
  - **静态分析**：`golang.org/x/tools/go/packages`
  - **平台与 OS 桥接**（详见 `docs/03-架构/平台与平台依赖矩阵.md`、ADR-0007）：
    - **Windows**：**保持 `CGO_ENABLED=0`**，用 `golang.org/x/sys/windows` 纯 Go Syscall，产物为单文件静态二进制。
    - **Linux**：**接受 CGO**（Gio 的 X11/Wayland/EGL/Vulkan 后端依赖它），需系统 `-dev` 包构建、需运行库运行。
    - **macOS**：**接受 CGO**（Gio 的 Cocoa/Metal 后端依赖它），需在 macOS 上构建。
    - ⚠️ 注意：Gio 在 Linux/macOS 上使用 `#cgo`，**不是** purego。不得据此声称三端可零 CGO 构建。
- **极限性能目标**（完整预算、实测基线与不变量见 `docs/03-架构/性能预算与优化策略.md`）：
  - 空窗与基础应用常驻内存：`15 ~ 30MB`
  - 进程冷启动时间：`< 50ms`
  - 单帧渲染 CPU 耗时相比纯手写 Gio 损耗：`< 5%`（**同时必须报告绝对 ns/op 与 allocs/op**，相对值不得单独作为通过依据）
  - **热路径稳定帧 `allocs/op` 必须为 0**（INV-PERF-2，可被 `go test -bench` 断言）
  - 万行虚拟列表单帧：`≤ 1ms`
  - **构建目标**：
    - Windows：`CGO_ENABLED=0` 单文件出包（已验证）
    - Linux：`CGO_ENABLED=1` 原生构建（编译已验证，运行 smoke 待完成）
    - macOS：`CGO_ENABLED=1` 在 macOS 上构建
    - **不再要求**三端一键交叉编译——该目标已被实测否决，见 ADR-0007

## 2. 五大架构红线 (Non-Negotiable Invariants)

任何 AI 编写的代码若违反以下任意一条，直接判定为**严重缺陷（P0 级阻断）**，严禁合并进主干：



1. **Windows 侧 Zero-CGO 红线**（**已修订**，原为"绝对 Zero-CGO 铁律"）：
   - **Windows 构建必须保持 `CGO_ENABLED=0`**：不得引入任何直接或间接需要 CGO 的第三方库（例如 `getlantern/systray`）。这是唯一仍强制成立的平台。
   - **Linux / macOS 允许 CGO**：Gio 的对应后端依赖 C 工具链与系统库，这是已实测确认的现实，不是缺陷。见 ADR-0007。
   - **禁止**在未登记到 `docs/03-架构/平台与平台依赖矩阵.md` 的情况下新增系统依赖（新的 `.so`/`.dylib`/`.dll` 或 `-dev` 包）。
   - 任何 AI 不得再声称"三端 `CGO_ENABLED=0` 一键交叉编译"——该结论已被真机实测否决。
2. **严禁运行时解析 XML 或 CSS**：
   - 所有 `.gxml` 标签扫描、属性转换（如 `Padding="10 20"` 转换为浮点数）、`<Def>/<Slot>` 组件内联展开必须在 `goxml generate` 阶段完成。运行时必须是纯粹的原生 Go 函数调用。
3. **严禁运行时反射脏检查 (Zero Reflection Diff)**：
   - 生成的 `*_gen.go` 代码中**绝对禁止出现 `import "reflect"`**。
   - 状态比对必须是编译器根据 `{Binding}` 路径生成的硬编码字段浅比对（`if v.oldVM.User.Name != newVM.User.Name`），未变动子树直接复用 Gio `op.CallOp` 宏缓存。
4. **异步隔离与非阻塞渲染主线程**：
   - 严禁在 Gio 的主渲染循环（`Layout(gtx)`）中调用任何同步阻塞 I/O 或系统原生弹窗（如 `GetOpenFileNameW`）。
   - 原生弹窗与文件操作必须运行在独立的 Goroutine 中，完成后通过 `window.Invalidate()` 唤醒 UI 渲染。
5. **严禁在帧热路径制造堆分配（零分配红线）**：
   - 静态子树必须在编译期固化 `op.CallOp`；`<For>` 渲染规模不得随集合长度线性增长（必须虚拟化）。
   - 严禁在 `Layout` 内每帧 `fmt.Sprintf` 或重建显示字符串——这会击穿 Gio 的文本 shaping 缓存。
   - 判定依据与实测数据见 `docs/03-架构/性能预算与优化策略.md`，违反即 P0。

## 3. 目录边界与职责拓扑 (Architecture Topology)

AI 在修改文件时，必须严格遵守以下目录的单向依赖规则，严禁逆向引用：



```
goxml/
├── contracts/             # [核心契约] 编译器 IR、Theme、Lifecycle 接口（全库唯一中立公共依赖）
├── internal/compiler/     # [AOT 编译器] 仅供 CLI 工具链调用，最终用户二进制不包含此目录
│   ├── parser/            # 带行列号的词法/语法分析器，EdgeInsets/Color 强类型转换
│   ├── expander/          # 组件 Schema 分类、属性/事件区分、<Def>/<Slot> 宏内联展开
│   ├── analyzer/          # go/types 语义校验：绑定路径、类型兼容、事件签名、For 作用域
│   ├── codegen/           # Gio 布局映射、AOT 状态 Diff、//go:embed 注入
│   ├── pipeline/          # 阶段串联与失败门禁：分析未通过不调 Codegen，失败不写文件
│   ├── grid/              # Grid 列宽计算的规范参考实现（oracle）
│   ├── sizelayout/        # 尺寸决策的可执行形式（主轴/交叉轴、无界判断）
│   └── diagdoc/           # 诊断码目录一致性门禁（AST 发射校验）
├── pkg/runtime/           # [运行时底座] 生成代码所依赖的轻量级运行时库
│   ├── router/            # 单窗口虚拟路由，MacroOp 转场快照动画器，OSWindow 逃生舱
│   ├── theme/             # 强类型 Theme 结构体，指针切换与暗色模式
│   ├── lifecycle/         # Context 驱动的 OnMount/OnUnmount 生命周期管理
│   └── hydration/         # Live Reload 状态序列化暂存与恢复 (.goxml_state.json)
├── pkg/platform/          # [Zero-CGO OS 桥接] 纯 Go 实现的托盘、菜单、异步弹窗、剪贴板
├── pkg/widgets/           # [标准组件库] 自绘 ContextMenu、DataGrid (虚拟滚动)、Modal 等
├── cmd/goxml/             # [CLI 工具链] generate、watch (双进程防抖热重启)、preview (静态预览)
└── docs/                  # [工程文档库] ADR、WBS 任务卡、交接包、踩坑记录
```

- **单向依赖红线**：
  - 允许：`cmd/goxml` -> `internal/compiler/*` -> `contracts`
  - 允许：生成代码 `*_gen.go` -> `pkg/runtime/*` -> `pkg/platform/*` -> `contracts`
  - **严禁**：`pkg/runtime` 或 `pkg/platform` 反向依赖 `internal/compiler`。

- **Codegen 入口红线**：`codegen.Generate` 只接受 `*analyzer.VerifiedDoc`。
  该凭证字段全未导出，只能由 `analyzer.Result.Verified()` 在 `StatusOK` 且无诊断时构造。
  **禁止**为「方便调试」给它加导出字段或旁路构造——那会让未验证的 IR 流进生成器。
- **产物边界**：Codegen **不写盘**，只返回 `[]byte`；写入与原子替换归 `pipeline.Commit`。
  `pipeline.Artifact` 存在只表示「允许提交」，不代表已落盘。失败时不得产生任何文件。

## 4. 多 AI 标准协同工作流 (SOP 七步闭环)

所有 AI 会话在接手任务时，严禁凭空臆断，必须按以下 7 步标准操作程序执行：



```
[步骤 1: 读 docs/00-文档索引.md 与契约] 
       ↓ 
[步骤 2: 读目标任务卡 TASK-xxx.md 与交接包] 
       ↓ 
[步骤 3: 运行 make setup && make test 验证基线全绿] 
       ↓ 
[步骤 4: 外科手术式编码，逐条运行 Subtask 验收命令] 
       ↓ 
[步骤 5: 运行全量门禁检查 (Lint + 交叉编译 + 覆盖率)] 
       ↓ 
[步骤 6: 更新任务卡状态、开发日志与新踩坑记录] 
       ↓ 
[步骤 7: 编写交接包，交接下一个 AI]
```

### 任务交付定义 (Definition of Done)

1. **命令实测为准**：Subtask 是否完成以约定的单行 CLI 验证命令返回退出码 `0` 为唯一判定标准，严禁“脑内推演”。
2. **测试不降级**：修改已有功能绝不允许直接注释或删除报错单测，必须保持行覆盖率 >= 85%。
3. **交接文档按需编写**：仅当任务在会话结束时**处于未完成 / 中断状态**（需要跨会话续接断点）时，必须在 `docs/05-开发/交接包/TASK-xxx-交接.md` 填写当前断点、验证命令与下一步指引。
   - 若任务在单次会话内**已完整闭环并通过验收**，以 Git 提交、`CHANGELOG.md` 与对应验收报告为准，**无需**额外生成交接包。
   - 事后补写交接包属于形式主义：提交记录与验收报告已提供可追溯的证据链。

## 5. 关键验证命令速查 (Tooling Cheatsheet)

AI 必须使用以下标准命令验证自身改动，严禁擅自修改验证参数：



| **验证场景**            | **执行命令**                                            | **严格通过标准**                                 |
| ----------------------- | ------------------------------------------------------- | ------------------------------------------------ |
| **契约一致性**          | `make contract-lint`                                    | `contracts/` 静态检查与契约测试 100% 通过        |
| **全库静态检查**        | `make lint` (`golangci-lint run ./...`)                 | **零错误、零警告**                               |
| **全库单元测试**        | `make test`                                             | 覆盖率不降低（纯 Go 部分）                       |
| **竞态检测**            | `make test-race`                                        | `go test -race` 无竞态告警（需 `CGO_ENABLED=1`） |
| **Windows 零 CGO 构建** | `make build-windows`                                    | `CGO_ENABLED=0 GOOS=windows` 构建成功，退出码 0  |
| **Linux 原生构建**      | `make build-linux`                                      | `CGO_ENABLED=1` 构建成功（需 §4.1 系统包）       |
| **MACOS 原生构建**      | `make build-darwin`                                     | 在 macOS 上 `CGO_ENABLED=1` 构建成功             |
| **逃逸与 GC 检查**      | `go test -gcflags="-m" ./internal/compiler/codegen/...` | 严禁多余的布局闭包指针逃逸至堆                   |
| **AOT 零反射校验**      | `! grep -r '"reflect"' testdata/*_gen.go`               | 生成代码中必须完全不包含 `reflect` 引用          |
| **零分配校验**          | `make bench-assert`                                     | 静态页面稳定帧 `allocs/op == 0`；万行列表 ≤ 1ms  |
| **性能对照基准**        | `make benchmark`                                        | 生成版 vs 手写 Gio，同时报告 ns/op 与 allocs/op  |

> ⚠️ `-race` 在**所有平台**都要求 `CGO_ENABLED=1`，因此它不能与"零 CGO 构建"写进同一条命令。二者必须拆成独立目标。

## 6. 代码生成与实现规范 (Codegen Standards)

### 6.1 生成代码质量准则 (`*_gen.go`)

- 顶部必须包含标准保护头：`// Code generated by goxml. DO NOT EDIT.`。
- 生成源码必须强制经过 `go/format.Source` 格式化，符合官方 `gofmt` 规范。
- **状态字段隔离**：在生成的 Shadow Struct 中，循环控件必须分配切片状态机（例如 `<For>` 内部的按钮使用 `btnSlice []widget.Clickable`），严禁整个循环共用单个实例。

### 6.2 布局代码生成映射

- `<Column>` / `<Row>`：
  - 当 `Padding` 为全 0 或缺省时，**严禁生成多余的 `layout.Inset{}.Layout(...)` 包装**，直接调用 `layout.Flex`。
  - 子组件默认映射为 `layout.Rigid(...)`，遇 `<Spacer>` 或 `<Expanded>` 映射为 `layout.Flexed(1, ...)`。
- **静态子树固化（强制）**：
  - 不含绑定/事件/条件/循环的子树，不得生成每帧新建的闭包，必须展开为直接调用并固化 `op.CallOp`。
  - 实测依据：闭包方案 28,000 ns/op + 51 allocs/op，展开方案 2,550 ns/op + 0 allocs/op（约 11×）。
  - 注意：仅做「预分配切片复用」无效（实测仅省 4%），必须消除闭包本身。
- `<Text Value="{User.Name}">`：
  - 为文本节点生成独立的 `v.textMacro = op.Record(gtx.Ops)` 缓存槽位，字段未改变时直接调用 `v.textMacro.Add(gtx.Ops)` 回放。
  - **严禁在 `Layout` 内每帧 `fmt.Sprintf` 拼接显示文本**：字符串每帧变化会使 Gio 文本 shaping 缓存完全失效（实测 876 ns/op → 82,053 ns/op，94×，且每帧 137KB 分配）。
- `<For>`：
  - 大于视口容量的列表必须走虚拟化路径（`Virtualize="true"`），渲染规模只与可见元素数相关。
  - 依据 `docs/03-架构/adr/ADR-0006-列表虚拟化与静态子树宏固化.md`。

### 6.3 契约演进原则

- 涉及修改 `NodeIR`、`BindingVal`、`CompilerError` 等公共模型时，**只能在 `contracts/` 目录下修改**。
- 修改后必须立即执行 `make contract-lint` 并更新 `docs/03-架构/架构说明.md`。

## 7. 已知深坑与防御性编码速查 (Known Pitfalls)

编写代码时必须对照以下已知踩坑记录进行防御性编码：



- ⚠️ **[PIT-001] 闭包堆内存逃逸**：
  - *现象*：Gio 嵌套布局闭包捕获局部变量导致每帧触发 GC 压力。
  - *方案*：**静态子树在编译期展开为直接调用并固化 `op.CallOp`**（实测 28,000→2,550 ns/op，51→0 allocs/op）。注意：**仅复用切片缓冲区无效**（实测只省 4%），必须消除闭包本身。用 `go test -gcflags="-m"` 监控逃逸。
- ⚠️ **[PIT-002] 原生弹窗卡死 UI**：
  - *现象*：直接同步调用系统文件对话框导致主界面渲染掉帧卡死。
  - *方案*：`pkg/platform/dialog` 强制设计为 `go func() { ...; window.Invalidate() }()` 异步回调模式。
- ⚠️ **[PIT-003] Windows 进程残留锁定 `.exe`**：
  - *现象*：热重启时旧进程未释放句柄，导致 `go build` 报错 `Access is denied`。
  - *方案*：`cmd/goxml/watch` 在 Windows 下执行 `taskkill /F /T /PID` 强制杀除整个进程树后再触发编译。
- ⚠️ **[PIT-004] CGO 间接污染**：
  - *现象*：引入三方“跨平台”库时隐式开启 CGO。
  - *方案*：**Windows 构建必须保持 `CGO_ENABLED=0`**，任何隐式引入 CGO 的依赖在 Windows 侧直接拒绝。Linux/macOS 允许 CGO，但新增系统依赖必须登记到 `docs/03-架构/平台与平台依赖矩阵.md`。
- ⚠️ **[PIT-008] 误以为 Gio 三端可零 CGO 构建**（本次实测新增）：
  - *现象*：相信"`CGO_ENABLED=0` 一键三端交叉编译"。实际在 Linux 上穷举 5 种 build tag 组合（`nox11`/`nowayland`/`noopengl` 及组合）**全部失败**，恒定报错 `imports gioui.org/internal/vk: build constraints exclude all Go files`。
  - *根因*：`internal/vk/vulkan.go`、`internal/gl/gl_unix.go`、`internal/egl/egl_unix.go`、`app/os_x11.go`、`app/os_wayland.go` 均 `import "C"`。
  - *方案*：Windows 保持零 CGO；Linux/macOS 使用 `CGO_ENABLED=1` 并安装对应系统包。见 ADR-0007 与平台矩阵。
- ⚠️ **[PIT-009] 误信文档说 Gio 的 macOS 后端用 purego**：
  - *现象*：`AGENT.md` 早期版本称 macOS 走 `purego`。实际 Gio 的 `go.mod` 中**没有 `purego`**，只有 `x/sys`、`x/net`、`godbus/dbus`；macOS 后端是 `#cgo` + Cocoa/Metal。
  - *方案*：平台层若自行绑定 Objective-C Runtime 可用 purego，但**不得声称 Gio 整体可在纯 Go 下构建**。
- ⚠️ **[PIT-005] 大列表全量渲染拖垮帧率**：
  - *现象*：`<For>` 直接渲染全部元素，10,000 行单帧 6.4ms、每帧 1.5MB 分配。
  - *方案*：启用虚拟化（`Virtualize="true"`），实测同场景降到 19,200 ns/op、0 allocs/op（335×）。详见 ADR-0006。
- ⚠️ **[PIT-006] 每帧拼接显示文本击穿 shaping 缓存**：
  - *现象*：在 `Layout` 内 `fmt.Sprintf` 生成文本，字符串每帧变化导致 glyph 布局全量重算（876→82,053 ns/op，94×，每帧 137KB）。
  - *方案*：缓存字符串化结果，只在依赖字段变化时重建；禁止在帧热路径做字符串拼接。
- ⚠️ **[PIT-007] CJK 文本渲染为空白**：
  - *现象*：Gio 自带 `gofont` 不含任何 CJK 字形，中文标签渲染为空白/豆腐块（实测 `GlyphIndex('中') == 0`）。
  - *方案*：按 `docs/03-架构/字体与文本渲染策略.md` 显式加载字体并建立 fallback 链。

## 8. 核心上下文与入口指针 (Context Pointers)

接手 AI 在进入具体任务前，优先阅读以下核心文件建立上下文：



1. **核心契约定义**：`contracts/ir_schema.go`、`contracts/runtime_api.go`
2. **GXML 语义规范**：`docs/03-架构/GXML语言规范.md`（v1 冻结，Parser/Analyzer/Codegen 的唯一语义依据）
3. **AOT 状态 Diff 规范**：`docs/03-架构/AOT状态Diff语义.md`（v1 冻结，Codegen/Runtime 的唯一语义依据）
4. **代码生成输出契约**：`docs/03-架构/代码生成输出契约.md`（v1 冻结，Codegen/CLI 的唯一输出依据）
5. **平台与平台依赖矩阵**：`docs/03-架构/平台与平台依赖矩阵.md`（CGO 开关、系统依赖、能力降级依据）
6. **运行时线程模型**：`docs/03-架构/运行时线程模型.md`（v1 冻结，UI/worker/dispatcher 边界依据）
7. **性能预算与优化策略**：`docs/03-架构/性能预算与优化策略.md`（v1 冻结，性能不变量与实测基线）
8. **字体与文本渲染**：`docs/03-架构/字体与文本渲染策略.md`（v1 冻结，CJK/IME/字体嵌入依据）
9. **诊断码目录**：`docs/03-架构/诊断码目录.md`（全部诊断码的唯一权威清单；新增码必须取号并登记，一致性由 `internal/compiler/diagdoc` 门禁守护）
10. **Analyzer 输入输出契约**：`docs/03-架构/Analyzer输入输出契约.md`（已冻结并实现，验收与未验证项见 `docs/06-测试/Analyzer验收报告.md` §5）
11. **编译编排契约**：`docs/03-架构/编译编排契约.md`（阶段中止点、失败门禁、产物与提交分离；实现见 `internal/compiler/pipeline`）
12. **测试与质量门禁**：`docs/06-测试/测试与质量门禁规范.md`（测试分层、覆盖率和发布门禁依据）
13. **架构决议留痕**：`docs/03-架构/adr/` (ADR-0001 ~ ADR-0008)
14. **当前开发进度总表**：`docs/04-WBS/WBS总表.md`
15. **当前断点（即时待办）**：`docs/04-WBS/任务卡/TASK-CODEGEN-001.md`（进行中：仅 `<Page>` + 静态 `<Text>`；布局/绑定/事件/`<For>`/Grid 未做）
16. **最新断点交接记录**：`docs/05-开发/交接包/TASK-ORCH-001-交接.md`（同目录另有 Parser/Expander/Analyzer/Codegen 的交接包）

> ⚠️ 上表第 15、16 项**会过期**。接手时若发现与 `docs/04-WBS/WBS总表.md` 或任务卡不一致，以任务卡为准，并按 SOP 步骤 6 修正本文档指针。

> ✅ **已验证事实**（2026-10-18，Windows + WSL2 Debian 13 实测）：Windows 可在 `CGO_ENABLED=0` 下构建；Linux 必须 `CGO_ENABLED=1` 并安装系统包；macOS 未验证，v1 仅保证功能设计。详见 ADR-0007。
