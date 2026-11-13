# 2026-11-13 · R29-B R3 原生夹具收口

- 基线 875601f791fc5b63b600b601e8aafc498c56b97c（R29-B R2 冻结树），分支
  `codex/r29-r3-glm-datagrid`，独立工作树；协调者证据只读。
- **提交 71f5fb0**：协调者两份反例（视口外右边界 x==viewWpx / gridB 快照
  与单选身份矛盾）在 v2 判定器下放行（缺陷复现入台账 r3-legacy-pass-invalid）。
  收口三处：① DataGrid 点击观察收口为包内——R2 候选公开 API `LastClick()`
  改为包内 `lastClick()`（夹具与组件同包，公开面保持 Clicked 不变），观察
  扩展命中当刻偏移（cellAt 实际使用的 hScrollOff/scrollOff）；② fixture
  click 事件携带命中当刻偏移与 bodyHpx，回放增加同帧滚动+点击、同帧
  DPI 缩放+点击两个几何一致性验证尾（不改变选择——宽容计数）；③ judge
  v3——域校验先行（视口四边半开区间、偏移域）、命中当刻几何×轴时间线
  一致性（同帧轴事件按帧预索引）、gridB 单选身份模型（快照/lead/selB
  时间线/留痕收口），--json 增 R3-domain 阶段与域校验计数。
- 判定矩阵：健康回放 PASS（含同帧验证尾）；协调者两份反例拒收；九类
  负例全拒；宽容正例 PASS。
- 未证边界：原生窗口 N1–N6 实机执行待协调者在最终组合安排。
