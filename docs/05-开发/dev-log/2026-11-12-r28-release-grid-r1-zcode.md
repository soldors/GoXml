# R28-B release-grid R1 修正线开发日志（2026-11-12）

返修起点：release-grid readyRef `69fa8fe7f56652cd9c5ee07ed52c25e16df06e82`。
分支 `codex/r28-release-grid-r1`。主库全程只读；真实 Go 一层公共锁
（heavy.lock）经 record-command 留痕。所有权内变更：仅 DataGrid native
fixture、judge、RUNBOOK 与本日志。

## 本线提交

- 本线修正提交（fixture/judge/RUNBOOK）——见 git log；含
  `docs/05-开发/dev-log/2026-11-12-r28-release-grid-r1-zcode.md`（本文件）。

## 修正明细

1. **入口可达**：`RunWindow(..., nil)` 在创建窗口前即被拒绝
   （"app: nil stop channel"，app.go nil-stop 校验）。修复：传入自有且
   永不关闭的 stop channel（Windows 测试入口；窗口经标题栏关闭自行
   返回，stop 不关闭即不会提前终止测试宿主）。未改用显式进程入口。
2. **判定不得缺证通过**：
   - 真实 close 经公开 `app.WithOnDestroy` 观察记录（destroy-callback 事件）；
   - RunWindow 成功返回/错误经 run-return 事件区分（err 原样，不互相冒充）；
   - judge R1 修正：缺 destroy-callback/close、exit 先于 close、空日志、
     「只选择未导航」（无 scroll 且无 ≥2 行跳变）一律 FAIL；
     scroll/sel-change 必须自带几何留痕（bodyHpx/rowHpx/headerHpx），
     滚动边界按留痕的本帧 Dp 换算值判定（不再用整窗高度和固定 24px）；
   - 旧判定器对两事件缺关窗反例（sel-change + exit）假绿 rc=0 已复现
     并录台账（r1-judge-falsegreen-repro）；修正后同反例 rc=1。
3. **几何与操作判据**：fixture 每帧留痕 bodyHpx/rowHpx/headerHpx
   （band 300 − Dp 表头；Dp 换算行高）与 gridB 选择对照（禁用对照：
   仅鼠标选择可用）；同尺度与缩放后几何轨迹经 resize+pxPerDp 留痕；
   Editor 持焦不串键与选择当帧可见性经 judge R2/R5 判据。
   键位、按住连发、IME 组合窗、视觉/DPI 外观无自动证据，明确留给人工
   （native-acceptance-plan A/B/C/D 表），不用合成事件替代。

## 判定器五场景（修正后）

反例（缺关窗）FAIL / 空日志 FAIL / 错序 FAIL / 只选择未导航 FAIL /
结构完整合成对照 PASS——录制于 r1-delivery-b 台账。

## 限制

- 原生键盘/IME/DPI/真实窗口仍未验证（作者不开窗）；RUNBOOK 修正了
  构建命令笔误（go build -c → go test -c）并补选择器锚定两命令。
- 任务引用的 r1-common.md 与 grid-missing-close.jsonl 在 product-r28/
  下未找到（已检索）——反例按描述语义重建（两事件缺关窗）并原样
  交付于 raw/grid-missing-close.jsonl；请协调者确认所指。

## R28-B2 补充（生产者/消费者对齐 + fixture 拆分）

- `cfb93b8` — R28-B1 修正线：fixture 入口可达（stop channel）/WithOnDestroy
  close 记录/judge 几何留痕判定；独立复核确认。
-  — R28-B2 拆分提交：datagrid_fixturelog.go（默认编译的生产者）+
  datagrid_fixturelog_test.go（三用例对齐）+ nativewindow 测试精简。
  键盘因果经 keyboard-nav 事件由生产者测试留痕（测试已知按键），judge
  据此区分纯鼠标/键盘轨迹。原生键盘/IME/DPI 仍未验证。

- 72bc121 — R28-B2 fixture 拆分与生产者/消费者对齐

- d216f18 — judge R9/R12 修正 + dev-log 补记 72bc121
