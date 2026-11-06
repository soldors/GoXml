# 2026-11-06 R25-R1（ZCode）candidate 分支返修记录

返修起点：作者 candidateRef `234bbbf`（R25-B）；原始开工基线
`95173db73c9a0dc4d2d204a8aec4f808e79dead3`（R24 发布后主库）；本分支自
234bbbf 演进，未合入主库。readyRef（设计/记录）在
codex/r25-r1-zcode-ready（604aba4 谱系）。

## 修复内容（对应 R25 协调者 review-b.md 三项 finding）

- F-B1：Expanded 子节点自身尺寸不再丢失。sizeWrap 拆出
  sizeConstraintsExpr（纯约束改写语句）；Expanded case = 父容器对
  Expanded 的 sizeWrap（Flexed(1) 特例）+ 弹性 cell 内注入**子节点自身**
  Exact/Fill 约束改写（IIFE，gtx 影子）。Width=12 与无 Width 产物不再相同
  （TestCoordinatorExpandedChildSizeNotIgnored）。
- F-B2：Expanded 自身 Padding、Grid 内 Expanded **显式拒绝**（不再静默
  接受）；generator.inGridCell 由 gridCellExpr 设置。守卫：
  TestCoordinatorExpandedPaddingNotIgnored / ExpandedGridNotSilent。
- F-B3：curMainAxis 清理（R22 复核指出无消费点）后**因 F-B1 恢复**——
  Expanded 子约束方向判定是其唯一消费点（四容器设置/恢复）。矩阵扩展：
  轴×自身/子 Exact×多弹性/rigid×Gap/Padding×If/For×双实例+缓存
  （r25r1_matrix_test.go）；Generate 成本对照（Expanded vs Plain）。

## 成本对照（2000×；Generate 层，前端预热不计入）

| 场景 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Expanded 页（Row+Button，跨包 VM） | ~571616 | 126132 | 1808 |
| Plain 页（Row+Text，同 VM） | ~299984 | 77299 | 1305 |

差值 = 跨包 VM 导入 + Expanded 弹性包装 + Button 事件的生成成本；
**不与渲染性能混谈**（离屏 Layout 由 r25_expanded_matrix 点击命中判据承担）。

## 门禁

- 全套 codegen（-skip RealWindow）rc=0；docgate rc=0（本记录覆盖锚点）；
  gofmt 空、vet 干净；race 待协调者复核（可选）。
- 退化双红：MUT-A（移除子约束注入）→ ChildSize 守卫红（silently ignored）；
  MUT-B（移除边界拒绝）→ Padding/Grid 守卫红。均本地复录还原（本 dev-log
  登记，raw 由返修线 raw/ 承载）。

## 未验证边界

- Grid 内 Expanded 的准入拒绝是**显式拒绝**（未定义语义，未批准）；
  权重/Expanded 自带装饰仍待裁决；原生窗口未跑。
