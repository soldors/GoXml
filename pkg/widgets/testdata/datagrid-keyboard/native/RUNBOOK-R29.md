# R29-B R1 DataGrid 交互扩展（N1–N6）— 原生验收运行手册

**执行前提**：协调者授权的原生阶段。作者阶段不开窗、不注入桌面输入、
不执行本 fixture。本手册是操作方案，不是「原生通过」声明。

R1 已实装：`TestDataGridR29NativeFixture`（`//go:build nativewindow`）、
`fixtureR29View`（默认编译，与无窗口回放共用同一生产者）与
`judge_r29_native_log.py`（模型化结构判定器）。

## 1. fixture 形态

- gridA：10k 行（ROW-%05d），5 列 ×300dp（总宽 1500dp > 1200dp 视口），
  `EnableHorizontalScroll` + `EnableKeyboardNav` + `EnableMultiSelect`；
- gridB：同数据、无 opt-in 禁用对照；editor：真实 `widget.Editor`；
- **显式排序入口**：视图级 P 键（Editor 持焦时由 Editor 消费、不触发）→
  `SortRows` 反序（ROW 降序）；
- 日志事件：`axis`（双轴偏移+几何）、`click`（列/RowID/偏移+几何）、
  `sort`（排序前后行序样本）、`sel-multi`（选择集+lead）、`sel-change`、
  `focus-change`、`edit-delta`（仅字符数）、`unconsumed-key`（仅导航键）、
  生命周期三事件。**不记录真实用户输入**（无按键名、无文本内容）。

## 2. 选择器锚定（两命令均须满足）

```bash
go test -tags nativewindow -list '.*' ./pkg/widgets   # 含 TestDataGridR29NativeFixture
go test -list '.*' ./pkg/widgets | grep TestDataGridR29   # 空（默认不编译原生入口）
```

带标签构建（作者阶段已完成，SHA 见交付 raw/r1-native-fixture/binary-sha256.txt）：

```bash
CGO_ENABLED=0 go test -tags nativewindow -c -o r29-native-fixture.test.exe ./pkg/widgets
```

## 3. 运行（record-command 留痕 + 容器收口）

```bash
python scripts/record-command.py run --id r29-native-fixture \
  --jsonl <ledger> --log-dir <rawdir> \
  --cwd <worktree> --containment win-job --timeout 900 \
  --lock-path D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock \
  --lock-mode acquire --lock-agent <runner> --lock-task R1-B-NATIVE-N1N6 \
  -- ./r29-native-fixture.test.exe -test.run '^TestDataGridR29NativeFixture$' \
     -test.v -r29-fixture-log <abs path>/r29-native-log.jsonl
```

- 日志路径须不存在（独占）；每条事件立即 flush；标题栏优雅关闭（rc=0）；
- 超时由容器杀进程，judge 以已有帧判定（缺 exit → R7 FAIL，如实记录）。

## 4. 操作脚本（N1–N6；与判定器模型一一对应）

| 标签 | 操作 | 判定要点 |
|---|---|---|
| N1 双轴独立 | 滚轮 Y 下两步 → 滚轮 X 右两步 | `axis` 每事件单轴变化（错轴拒）；域钳制 |
| N1.5 | 点击 gridA 表头（持焦不选择）→ 按 Home | `axis` offY 精确回 0；`sel-multi` [ROW-00000]（Home 初始选择） |
| N2 滚后命中 | 在可见第 3 列区域点击视觉行 1 | `click` col=2 且 offX>0（错列拒）；rowID=ROW-00001 |
| N3 排序命中 | 按 P（反序）→ 点击视觉首行 | `sort` after-head 严格降序（无排序拒）；rowID=ROW-09999 |
| N4 Ctrl 多选 | 点视觉行 2 → Ctrl 点行 4 → Ctrl 点行 2 | `sel-multi` [09997]→[09997,09995]→[09995]（错误选择集拒） |
| N5 Shift 范围 | 点行 1 → Shift 点行 3 → Shift 点行 0 → 点行 2 | [09998]→[09996..09998]→[09999,09998]→[09997] |
| N6 双实例/Editor | 点 gridB 首行 → 点 Editor 输入两字符 → 点 gridA 视觉行 5 | gridB rowID=ROW-00000；edit-delta chars=+2 focus=editor；gridA 收拢 [09994] |

结束后标题栏关闭，然后判定：

```bash
python pkg/widgets/testdata/datagrid-keyboard/native/judge_r29_native_log.py <log.jsonl>
```

STRUCTURAL PASS 要求：N1–N6 模型时间线全部一致、双轴独立、命中列几何
可见、排序恰一次且行序翻转、选择集逐步匹配、生命周期完整且 exit 为最后
事件。SCENARIO 恒为 MANUAL-REQUIRED（因果由操作者确认）。

## 5. 无窗口回放证据（作者阶段已完成）

`TestR29FixtureReplayN1N6`（默认编译）以**同一事件脚本**经离屏
`input.Router` 驱动同一 `fixtureR29View.Layout`，产出与原生夹具完全
相同生产者格式的日志；`judge_r29_native_log.py` 对其 STRUCTURAL PASS，
并对五类反例（错列/错轴/无排序/错误选择集/早退）逐一拒绝（判定记录见
交付 raw/r1-judge-negatives/judge-verdicts.txt）。该证据证明 judge
链路与日志形态；**不代表原生窗口已执行**——原生 N1–N6 由协调者在授权
桌面复验。

判定器负例覆盖（已验证拒收）：
`neg1-wrong-col`（错列）、`neg2-wrong-axis`（错轴）、`neg3-no-sort`
（无排序）、`neg4-wrong-selection`（错误选择集）、`neg5-early-exit`
（早退）。

## 6. 未证边界（不冒充）

- 原生窗口下的 N1–N6：**未执行**（需授权桌面）。
- 真实滚轮轴向/修饰键注入、DPI 切换下几何一致性：未验证。
- 操作者偏离脚本（多滚/少滚、点错行）会被判定器以错行/步骤不完整拒收
  ——属预期行为，按实际日志如实记录。
