# DataGrid 键盘导航候选语义表（R28-B，未批准不入规范）

本表随 pkg/widgets/datagrid_keyboard.go 冻结；为验收与裁决的单一口径。

## API

| API | 语义 |
|---|---|
| `EnableKeyboardNav(cfg DataGridKeyboardConfig)` | 显式开启（opt-in）。若已有数据，立即一次 O(N) 构建 RowID→索引；此后 SetRows 成功时在同一次 O(N) 校验遍历内重建（UpdateReport.KeyboardIndexBuilt/KeyboardIndexRows 披露） |
| `DisableKeyboardNav()` | 显式关闭：停止注册按键过滤器、释放索引 map；选择状态保持 |
| `KeyboardNavEnabled() bool` | 查询开关状态 |
| `Focused() bool` | 最近一帧是否持有键盘焦点（诊断；仅开启时有意义） |
| `DataGridKeyboardConfig.PageRows int` | PageUp/PageDown 移动行数；<=0 = 当前可见行数（bodyH/rowH，至少 1） |

## 按键语义（仅 key.Press 生效；Release 消费后忽略；平台连发=连续 Press 各一步）

| 键 | 无选择时 | 有选择时 | 滚动 |
|---|---|---|---|
| Down | 选中可见首行（vis0） | 下一行（+1，末行原地钳制） | 目标行滚入可见区（当帧生效） |
| Up | 选中可见首行 | 上一行（-1，首行原地钳制） | 同上 |
| Home | 第 0 行 | 第 0 行 | 滚到顶（offset=0） |
| End | 最后一行 | 最后一行 | 滚到底（offset=maxOff） |
| PageDown | 可见首行+page | 当前行+page（钳制） | 目标行滚入可见区 |
| PageUp | 可见首行-page（钳到 0） | 当前行-page | 同上 |

- 边界钳制：越界移动原地保持（不换行、不重滚、不产生事件）。
- 键盘选中不伪造鼠标点击事件：`Clicked()` 仍只反映指针点击（消费语义不变）。
- 空表（0 行）：全部按键明确无操作（不产生选择、不访问）。
- 空 body（bodyH≤0，如视口被表头占满）：选择仍按 RowID 更新（含 End 到末行），
  但不滚动（maxOff=0）、不产生任何全表访问（访问窗口保持为空）。

## 焦点语义（候选）

- 过滤器全部 Focus 限定到本网格标签：只有当前持有焦点的网格消费按键；
  不用全局 catch-all、不逐帧夺焦、不吞 Editor/Modal 的按键。
- 获得焦点：主键按压网格内任意位置（含表头/空白带）= 点击聚焦
  （使「初始无选择」可达：表头按压只聚焦不选择）。
- 失去焦点：焦点转给 Editor 等可聚焦消费者后，网格自动停止消费
  （按键归还消费者）；点击网格可重新取回。
- 未持焦时不轮询按键过滤器（成本口径；语义等价——未持焦本就不匹配）。

## 成本口径（本机微基准，10k/100k 同视口 300×200）

| 口径 | 10k | 100k |
|---|---|---|
| 默认关闭 Layout-only | 4928 ns/op，0 B/op，0 allocs/op | 5674 ns/op，0 B/op，0 allocs/op |
| 开启空闲帧（未持焦） | 5263 ns/op，0 B/op，0 allocs/op | 6330 ns/op，0 B/op，0 allocs/op |
| 按键发生帧（含 Router.Frame + Gio 键路由 3 allocs/帧固有） | ≈28.7μs | ≈29.8μs |
| SetRows 更新（关闭 / 开启含索引） | 546μs / 1071μs | ≈4.5ms / ≈9ms |

规模比（Layout-only）1.15× ≤1.5×（INV-PERF-3）；万行 ≤1ms 保持。
`TestDGStableLayoutZeroAllocs` 对默认关闭与开启空闲帧同时断言
0 allocs/op 与 0 B/op（字节口径复核隔帧分配）。

## 索引

- RowID→索引 map：启用时构建；内存 ≈ 每 RowID 一个 map 条目
  （100k 行约数 MB 量级）；DisableKeyboardNav 立即释放。
- 重排/缩短后按当前数据邻接移动（RowID 不退化为旧索引）；
  选中行被删除 → 选择显式清除（既有 SetRows 语义），Down 重新选可见首行。
