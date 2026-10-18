# WBS 总表

| Task ID | 模块 | 标题 | 依赖 | 核心验收命令 |
|---|---|---|---|---|
| TASK-CONTRACT-001 | contracts | 定义编译器 IR 与运行时核心接口契约 | - | go test -v ./contracts/... |
| TASK-PARSER-001 | parser | 实现带行列号的 GXML 语法分析与控制流解析 | TASK-CONTRACT-001 | go test -v ./internal/compiler/parser/... |
| TASK-PARSER-002 | parser | 实现强类型属性解析器 (EdgeInsets/Color/Binding) | TASK-PARSER-001 | go test -v -run TestAttrUnmarshal ./internal/compiler/parser/... |
| TASK-EXPANDER-001 | expander | 实现组件 Schema 分类、属性事件区分与 Def/Slot 展开 | TASK-PARSER-002 | go test -v ./internal/compiler/expander/... |
| TASK-ANALYZER-001 | analyzer | 实现 ViewModel AST 静态扫描与校验 | TASK-EXPANDER-001 | go test -v ./internal/compiler/analyzer/... |
| TASK-CODEGEN-001 | codegen | 实现 Gio 布局（含 Align/Justify/Gap/尺寸/Grid）与控件代码生成器 | TASK-ANALYZER-001 | go test -v ./internal/compiler/codegen/... |
| TASK-STATE-001 | codegen | 实现编译期 AOT 浅对比 Diff 与 MacroOp 缓存 | TASK-CODEGEN-001 | go test -v -run TestAOTStateDiff ./internal/compiler/codegen/... |
| TASK-STYLE-001 | macro/theme | 实现强类型 Theme 与组件宏展开 | TASK-CODEGEN-001 | go test -v ./internal/compiler/macro/... ./pkg/runtime/theme/... |
| TASK-FONT-001 | theme/font | 实现 CJK 字体探测、嵌入兜底、fallback 链与 IME 验收 | TASK-STYLE-001 | go test -v ./pkg/runtime/theme/... |
| TASK-ROUTER-001 | runtime | 实现单窗口路由、MacroOp 转场动画与 OSWindow | TASK-STATE-001 | go test -v ./pkg/runtime/router/... |
| TASK-LIFE-001 | runtime | 实现 Context 生命周期与 //go:embed 资产管线 | TASK-ROUTER-001 | go test -v -race ./pkg/runtime/lifecycle/... |
| TASK-DEVX-001 | cli/watch | 实现双进程防抖热重启与状态 Hydration | TASK-LIFE-001 | go test -v -race ./cmd/goxml/watch/... ./pkg/runtime/hydration/... |
| TASK-DEVX-002 | cli/preview | 实现独立毫秒级静态 UI 预览器 | TASK-STYLE-001 | go test -v ./cmd/goxml/preview/... |
| TASK-OS-001 | platform | 实现 Zero-CGO 剪贴板与异步原生文件弹窗 | TASK-LIFE-001 | CGO_ENABLED=0 go test -v ./pkg/platform/clipboard/... ./pkg/platform/dialog/... |
| TASK-OS-002 | platform | 实现系统托盘与系统菜单栏（Windows 纯 Go，Linux/macOS 允许 CGO） | TASK-OS-001 | make build-windows && make build-linux |
| TASK-WIDGET-001 | widgets | 构建标准自绘组件库 (ContextMenu/DataGrid) | TASK-STYLE-001 | go test -v ./pkg/widgets/... |
| TASK-BENCH-001 | bench | 性能基准测试与旗舰 Demo 验收 | TASK-CONTRACT-001, TASK-PARSER-001, TASK-PARSER-002, TASK-EXPANDER-001, TASK-ANALYZER-001, TASK-CODEGEN-001, TASK-STATE-001, TASK-STYLE-001, TASK-FONT-001, TASK-ROUTER-001, TASK-LIFE-001, TASK-DEVX-001, TASK-DEVX-002, TASK-OS-001, TASK-OS-002, TASK-WIDGET-001 | go test -bench=. -benchmem ./benchmarks/... |

## 说明

- `TASK-FONT-001` 为本次补充新增：此前 WBS 缺失字体与 IME 相关工作项，而 Gio 默认字体不含 CJK 字形（实测 `GlyphIndex('中') == 0`），中文界面会渲染为空白。依据见 `docs/03-架构/字体与文本渲染策略.md`。
- 性能相关不变量与阈值见 `docs/03-架构/性能预算与优化策略.md`；列表虚拟化与静态子树固化的决策见 `ADR-0006`。
