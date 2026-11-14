# 2026-11-14 · R29-B DataGrid 原生交互收口（WAIT_NATIVE）

- 基线 90a0ac750af63886191ded2bfb56c9a3fc6ca289（R29 组合候选，含协调者
  修复 1e52179），分支 `codex/r29-closeout-glm-datagrid`，独立工作树。
- 本线**源码零改动**：S1 审查确认同版生产者（fixtureR29View）、判定器
  v5、锚定选择器与操作单在组合基线上齐全且一次实机可走完 N1–N6；
  18 项判定矩阵（健康/宽容正例 PASS；陈旧偏移、越界、错选择、缺字段、
  早退等 16 拒）在组合基线复跑全符，未发现会误判实际日志的情形。
- 交付（coordinator-r5-integration/delivery-b/）：operator.md（逐步操作
  单，补滚轮方向/可视行号口径/DPI 约束）、native-evidence.md（实机 run
  对账清单与失败三分类）、decision-matrix.md（公开语义裁决表，P 键仅为
  夹具行为）、native-evidence 固定三份 SHA（普通 b5876a53…、race
  c7281064…、回放 5aefaaf6…）与选择器锚定。
- 门禁：widgets 107 PASS / 0 FAIL；build/vet/gofmt/docgate/diagdoc rc0；
  race（MinGW）rc0；10k/100k 访问窗与零分配门禁 rc0；台账 22 条
  verify PASS。
- 状态 WAIT_NATIVE：三个原生场景由协调者统一排程，实机 run 产生后按
  native-evidence.md §2 对账；需返修时按原红/新绿/受影响范围交同一轮。
