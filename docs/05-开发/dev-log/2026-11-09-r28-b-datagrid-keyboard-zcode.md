# R28-B DATAGRID-KEYBOARD-NAVIGATION 开发日志（2026-11-09）

基线 `255c39251127c39d1a684baa881767a5fb7677b9`，分支 `codex/r28-zcode-grid-keyboard`。
主库全程只读；真实 Go 全部一层公共锁（D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock）
经 record-command 留痕。所有权内变更：pkg/widgets/datagrid.go（最小集成）、
pkg/widgets/datagrid_keyboard.go（新）、pkg/widgets/datagrid_keyboard_test.go（新）、
pkg/widgets/datagrid_bench_test.go（基准）、pkg/widgets/README.datagrid.md、
pkg/widgets/testdata/datagrid-keyboard/、本日志；菜单/Modal/overlay 未动。

## 本线提交（docgate 覆盖登记）

- `49d8eee` — S1/S2：键盘单选/纵向导航候选（默认关闭）+ 真实离屏 key.Event
  矩阵。显式 opt-in（EnableKeyboardNav/DisableKeyboardNav/KeyboardNavEnabled/
  Focused）；Up/Down/Home/End/PageUp/PageDown 按稳定 RowID 单选；索引在
  SetRows/EnableKeyboardNav 同一次 O(N) 遍历构建（UpdateReport 披露）；
  过滤器全部 Focus 限定；主键按压网格任意位置=点击聚焦（初始无选择可达）；
  scroll-into-view 当帧生效；空 body 记选择不滚动不访问；键盘不伪造
  Clicked；默认关闭路径无监听无分配。
- `e1d72a5` — S3：成本口径与隔帧分配守卫。按键过滤器轮询改为仅持焦时
  进行（未持焦注册只会让 Gio 全局 key.filter 每帧增长 2.6KB——
  AllocsPerOp 整数除法掩盖）；kb-enabled-idle 10k/100k 均 0 B/op、
  0 allocs/op；kb-key-frame/set-rows-update-with-index 单列；
  门禁以字节口径复核隔帧分配。

## 实测结论（本机微基准，不外推 GPU/整应用）

- 默认关闭 Layout-only：10k=4928ns / 100k=5674ns，0 B/op 0 allocs/op。
- 键盘开启空闲帧（未持焦）：10k=5263ns / 100k=6330ns，0 B/op 0 allocs/op。
- 按键发生帧（含 Router.Frame 与 Gio 键路由）：≈28.7-29.8μs、3 allocs/帧
  （Gio 键路由固有开销，单列口径，非组件分配）。
- SetRows 更新：关闭 546μs / 开启（含索引重建，同一次遍历）1071μs（10k），
  100k 关闭≈4.5ms / 开启≈9ms（单列，随 UpdateReport 报成本）。
- 覆盖率 94.5%（pkg/widgets 全包）；race rc=0（CGO MinGW）。

## 变异（红→恢复绿；均已提交后进行，未发生 checkout 丢修复）

- MUT-K1 移除过滤器 Focus 约束：TestKBDualInstanceFocusIsolation 红
  （未持焦实例消费他人按键）→ 恢复绿。
- MUT-K2 关闭 scroll-into-view：TestKBClickToFocusAndNavigate/
  TestKBScaleIdentity10k100k/TestKBInitialNoSelectionAndPageKeys 红
  （选择正确但行不可见）→ 恢复绿。
- 事故披露：MUT-K2 录红时公共锁被 workbuddy-r28c 持有，record-command
  正确拒绝（无子进程）；重试循环因成功判定键名写错（recorded vs
  recordOk）未及时中断，同 id 重复记录 11 条同内容红——全部为同一
  变异态真实运行，保留不删（台账可见）。

## 限定结论

- 全部验证为离屏 io/input.Router 帧（key.Event/EditEvent/pointer）；
  原生键盘（平台连发形态、IME）、真实 DPI 未验证，归协调者原生阶段。
- 键盘语义全部为候选（scope-matrix C-DG10..C-DG16），未批准不入规范；
  不关闭 TASK-WIDGET-001-3 整卡。
- 默认选择器不进入 Window.Event/Win32 后端；未运行任何用户 main/init。
