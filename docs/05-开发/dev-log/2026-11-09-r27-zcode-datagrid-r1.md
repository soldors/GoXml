# R27-B 返修 R1 开发日志（2026-11-09）

返修起点：本线原冻结 `44970ff3619ffa64dbc55e1f616b231b11b526ab`；
分支 `codex/r27-r1-zcode-datagrid`；原冻结树/旧 delivery/审查 raw 只读。
主库全程只读；真实 Go 全部一层公共锁经 record-command 留痕。

## 本线提交（docgate 覆盖登记）

- `a69d93d` — S1/S2：F-B1/F-B2 产品修复 + 持久回归（datagrid_r1_test.go）。
- `提交(本轮 S3)` — 无 body 边界基准（BenchmarkDataGrid/*/header-only-viewport）。
- `提交(dev-log)` — 本日志（日志维护提交）。

## 闭合明细

- **F-B1（表头占满视口退化全表访问绘制）**：根因是 `vis1` 初值取
  `len(g.rows)`、`bodyH<=0` 分支只挡 `vis0`——表头占满视口时访问/绘制
  窗口仍是全表。修复：`bodyH<=0` 时 vis0=vis1=0 且 overscan 不外扩、
  maxOff 钳 0（无隐形滚动余量）。协调者探针
  TestCoordR27HeaderOnlyViewportDoesNotVisitTable 原红→新绿。
- **F-B2（跨帧释放越出视口仍提交点击）**：Release 经按压时进入的处理者
  路由、位置可落在视口外，旧 cellAt 直接整数换算把越界小数折进界内。
  修复：cellAt 先按本帧 viewport/body 浮点比较拒绝（含 -0.1/+0.1），
  再整数换算；跨帧 Release Buttons=0 与合法边缘命中保留。协调者探针
  TestCoordR27ReleaseOutsideViewportDoesNotClick 原红→新绿。
- 持久回归：视口高度矩阵 0/1/header-1/header/header+1（手算期望，10k/
  100k 规模无关，Cell 回调计数）、滚动后缩小、四边+小数越界释放、缩小
  视口后的过期释放、越界按压惰性、取消序列、正控制（界内边缘/小数右缘）。
- 变异：撤销无body保护 → 视口高度矩阵守卫红；撤销 viewport 释放检查 →
  越界释放矩阵守卫红；恢复绿。原虚拟化/RowID 守卫（B3 MUT-DG-1/2）
  保留未动。

## 限定结论

- 全部验证为离屏 io/input.Router 帧循环；原生窗口/真实 DPI/IME 未验证。
- 主题/Shaper/字号缓存失效仍是 scope C-DG7 候选限制，未升级为动态主题支持。
- 返修只改 pkg/widgets/datagrid.go 与新增 datagrid_r1_test.go；
  既有菜单/Modal/overlay、公共 CLI/Compiler/依赖未动。
