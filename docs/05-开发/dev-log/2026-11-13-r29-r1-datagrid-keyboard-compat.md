# 2026-11-13 · R29-B R1 键盘兼容返修与 N1–N6 原生夹具实装

- 基线 cd7821178143b5916379a785c68a3dcf710a96ab（R29-B 冻结树），分支
  `codex/r29-r1-glm-datagrid`，独立工作树；原冻结树只读。
- **提交 572c4a6**：协调者 b-probe 两条真实 Router 反例收编为回归并修复——
  ① Shift 导航仅在多选模式声明进过滤器 Optional；单选模式保持 R28 精确
  过滤器，Shift+方向键不被网格消费、外部事件消费者可收（Gio 语义：排队
  键事件按上一帧持久过滤器把关，测试助手据此先注册一帧）。② 多选普通
  导航在首尾钳制处仍收拢为当前锚行（不重滚、不伪造 Clicked）；单选边界
  保持「无操作」。兼容正反例 7 项（跨轴、Ctrl/Shift、非空选择、失效锚点）。
  变异 MUT-4/MUT-5（重引入两 bug）红→恢复绿，台账留证。
- **提交 4fbdeed**：RUNBOOK-R29 的 N1–N6 实装——`fixtureR29View`（默认
  编译，横滚+多选+键盘 opt-in、5 列横向溢出、视图级 P 键显式排序入口；
  日志记录双轴偏移/点击列与 RowID/排序前后行序/选择集与 lead/Editor 焦点，
  不记录真实用户输入）；`TestDataGridR29NativeFixture`（nativewindow 标签，
  默认选择器不含）；`judge_r29_native_log.py` 模型化结构判定（拒错列/
  错轴/无排序/错误选择集/早退，五类负例逐一验证拒收）；
  `TestR29FixtureReplayN1N6` 同脚本离屏回放产出同构日志（judge PASS）。
  带标签测试二进制 SHA-256 = 1ca86bd0823eecea643a5743dd3505f78a3b222eb9519c0234874cc72998c04e。
- 回归口径（本提交前）：`go test ./pkg/widgets/ -count=1` 106 PASS /
  0 FAIL / 0 SKIP；build/vet/gofmt 干净；docgate/diagdoc 见台账。
- 未证边界：原生窗口 N1–N6 未执行（需授权桌面）；候选语义（Shift 过滤器
  声明策略、边界收拢语义、排序入口键）待负责人裁决。
