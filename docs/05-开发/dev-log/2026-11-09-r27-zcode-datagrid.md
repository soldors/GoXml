# R27-B DATAGRID-VIRTUALIZATION 开发日志（2026-11-09）

基线 `d6b1fc063bb5671ab6340181cd8b16160c5b676c`，分支 `codex/r27-zcode-datagrid`。
主库全程只读；真实 Go 全部走公共锁（D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock）
经 record-command 留痕。所有权内变更：pkg/widgets/datagrid*.go、
pkg/widgets/README.datagrid.md、本日志；既有菜单/Modal/overlay 文件未动。

## 本线提交（docgate 覆盖登记）

- `ad7819c` — B1：datagrid.go 组件与尺寸/身份契约（候选）。固定行高、
  显式列宽（Dp）、稳定 RowID、整格单事件区（无逐行 Clickable）、可见区+
  有界 overscan(2)、显式更新阶段 O(N) RowID 校验（fail-closed）、
  LastFrame 白盒披露；B1 单测。
- `34adf5b` — B2：真实离屏行为矩阵。10k/100k 同视口逐帧访问/绘制 RowID
  集合精确断言（首/中/尾/回顶）、点击身份跟随本帧几何、跨帧
  Press/Release（Buttons=0）、cancel、重排/缩短/清空、视口/列宽变化、
  双实例（父级偏移）隔离、父偏移不泄漏、键盘不支持为明示契约、绘制回调
  逐单元格精确计数。
- `455fa81` — B3：基准与零分配门禁 + README.datagrid.md。修复两项实现
  缺陷：绘制宏缓存改条目私有持久 Ops（ADR-0009 形态——Frame 级 ops 每帧
  新建，录制其中的 CallOp 跨帧失效，带 Router.Frame 的门禁会 panic）；
  帧统计切片自切片重置（原从独立缓冲重置导致每帧容量重增长 5+4=9 allocs）。
- `0a03723` — 事故产物重写：MUT-DG-2 录红后 `git checkout -- datagrid.go`
  把未提交的上述两项修复一并还原（B3 提交当时只含 bench/README），
  以同内容重写落盘；closure-matrix.md 披露。
- `866ab14` — gofmt。

## 实测结论（本机微基准，不外推 GPU/整应用）

- 稳定帧 Layout 热路径：10k=5025ns / 100k=4882ns，均 0 B/op、0 allocs/op
  （规模比 1.03×，INV-PERF-3 ≤1.5×；万行可见布局 ≤1ms 目标达成）。
- 完整 Router.Frame 帧：10k=23656ns / 100k=23107ns，0 allocs/op（单列口径）。
- 冷启动帧（宏缓存重建）：≈94μs、450 allocs（单列）；SetRows 显式更新
  （含 O(N) RowID 校验）：10k≈0.47ms / 100k≈5.4ms（单列，成本随更新报告）。
- 对照 BenchmarkListBaseline（等价 layout.List 手写形态、逐帧 shaping）：
  22919ns/帧——组件完整帧与其同量级且组件侧含表头/选择/事件语义。
- 覆盖率 95.8%（pkg/widgets 全包，含 R26 既有代码）。

## 限定结论

- 全部验证为离屏 io/input.Router 帧循环；原生滚轮/真实 DPI/IME/原生选择
  交互未验证，由协调者组织原生阶段；不关闭 TASK-WIDGET-001-3 整卡
  （表头固定/溢出滚动等候选单列 scope-matrix.md，未批准不入规范）。
- 键盘选择/滚动首版不支持（明示契约，未注册按键过滤器）。
- 返修事故（checkout 还原未提交修复）已在 closure-matrix.md 如实登记，
  以同内容重写恢复，全套件 53 PASS/0 FAIL/0 SKIP 复绿。
