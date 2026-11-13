# 2026-11-13 · R29-B R4 原生证据校验

- 基线 cee2c62fa881992587d5c1329425f62870934b03（R29-B R3 冻结树），分支
  `codex/r29-r4-glm-datagrid`，独立工作树；主库 d20714d 与协调者组合树
  becb9f0 只读。
- **提交 e62e551**：判定器 v4。协调者两份反例复现——`b-r3-gridb-false-from`
  （selB-change.from=ROW-00099 错误前态仍 PASS）与 `b-r3-missing-header`
  （gridB click headerHpx=None 触发裸 TypeError，无结构化结论）——首败
  入台账（r4-legacy-pass-invalid rc0 原样留证；r4-legacy-typeerror 以
  expect-rc0 记 pass=False，异常不算有效拒收）。修复：① 统一域校验层，
  click 全部字段先类型/枚举/位置域校验，缺字段/错类型统一结构化 item；
  seq 违规与意外异常经逐事件守护转 R0-internal 结构化 FAIL，JSON 与
  退出码共同验证；② gridB 前态核销——from 等于模型变更前值、to 等于
  变更后值，快照只核销一次，跨步骤时序在三处场景边界收口；③ 偏移对
  成对比较——(offX,offY) 对可达对集合（轴历史对含 prev ∪ 同帧轴对），
  neg12（(180,320) 组合）验证拒收。
- 判定矩阵：健康回放 PASS；12 负例全拒；宽容正例 PASS；协调者两份反例
  JSON+退出码双验证拒收（gridb-false-from → R17-gridb-from；
  missing-header → R14d-fields/R14b-fields）。
- 未证边界：原生窗口 N1–N6 实机执行待协调者最终组合。
