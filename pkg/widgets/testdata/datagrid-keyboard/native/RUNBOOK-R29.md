# R29-B DataGrid 交互扩展（横向滚动 / 排序 / 多选）— 原生验收场景手册

**执行前提**：协调者授权的原生阶段。作者阶段不开窗、不注入指针/按键、
不执行本手册。本手册是操作方案与场景定义，不是「原生通过」声明。

基线：R28 原生键盘 fixture（`datagrid_keyboard_native_test.go` +
`judge_native_log.py`，gridA/gridB/Editor 三带 + fixture 日志）。
R29 新场景复用该窗口形态；**fixture 需在协调者阶段按本手册扩展**
（gridA 开 `EnableHorizontalScroll`+`EnableMultiSelect`，提供排序触发
路径），扩展后 judge 需同步新增 R13–R15 判定——属生产者/消费者对齐，
不在作者阶段实现。默认测试集合保持无窗口（`nativewindow` 标签隔离）。

## 1. 场景标签（N1–N6）

| 标签 | 场景 | 判定要点（日志自动 + 人工） |
|---|---|---|
| **N1 双轴独立** | gridA 开横向滚动；滚轮 X/Y 交替 | 日志 `off-x/off-y` 按轴独立步进与钳制；人工：表头与表体列边缘同步移动，表头纵向不随 Y 滚动 |
| **N2 滚后命中** | 横滚 150px 后点击列 1 中部 | 日志 `sel-change` 命中列为点击时可见列（非视觉首列）；人工：高亮行/列与指针位置一致 |
| **N3 排序命中** | 操作者按 P（触发 fixture 调 `SortRows` 反序）后点击首行 | 日志记录排序事件与 `sel-change`；人工：首行内容翻转，点击命中翻转后首行 |
| **N4 Ctrl 多选** | Ctrl+点击行 2、行 5、再行 2 | 日志 `sel-multi` 集合 {2,5}→{5}；人工：两行同时高亮、再点后仅一行 |
| **N5 Shift 范围** | 点击行 2 后 Shift+点击行 6 | 日志 `sel-multi` {2..6}；反向 Shift+点击行 0 → {0..2}；普通点击收拢为单行 |
| **N6 双实例/Editor 交接** | gridA Ctrl 多选 → 点击 Editor 输入 → 点击 gridB | Editor 持焦期间 gridA 集合不变、↓ 不被网格消费；gridB 独立集合；焦点带记录与 R28 相同形态 |

## 2. 构建与运行（协调者阶段）

```bash
# 选择器锚定（两命令均须满足）
go test -tags nativewindow -list '.*' ./pkg/widgets   # 含原生用例名
go test -list '.*' ./pkg/widgets                      # 不含任何 nativewindow 用例

CGO_ENABLED=0 go test -tags nativewindow -c -o native-grid-fixture.test.exe ./pkg/widgets
```

运行走 `scripts/record-command.py`（win-job 容器收口 + 公共锁
`D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock`），形态与
[R28 RUNBOOK](RUNBOOK.md) §2 完全一致；日志路径须不存在（独占）。
每场景 N1–N6 按 §1 时段操作后优雅关闭（rc=0），judge 以日志判定；
fixture 未实现 R29 扩展前，本手册场景**全部记为未执行**，不冒充通过。

## 3. 未证边界（不冒充）

- N1–N6 均为**计划**：离屏 harness 已覆盖同构语义（`datagrid_combo_test.go`
  等 97 项），但真实指针滚轮轴向、真实 Ctrl/Shift 修饰键注入、DPI 变化
  下的横滚命中未在实机验证。
- 原生连发 Ctrl+点击、IME 组合期间多选、150%→125% DPI 切换下的列宽
  与命中一致性：待协调者实机执行。
