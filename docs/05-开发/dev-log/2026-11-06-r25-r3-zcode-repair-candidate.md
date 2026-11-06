# 2026-11-06 R25-R3（ZCode）candidate 分支返修记录

candidateRef 谱系：234bbbf → 9102bcf（R25-R1）→ 912ca27（R25-R2 夹具）→
68efc90（本线 S1/S2）。基线主库 76f0669（A+所有权补正发布后）只读。

## S1（68efc90）：Gap/Padding 边界特异性 + Def/Slot 正例/负例改名

- **Gap/Padding 特异性**（review-repair-r2 缺口1）：TestR25R3GapPaddingBoundaries
  以内容命中（x=100→A、x=300→B）+ **边界不命中**（x=2 左 Padding 区、
  x=200 Gap 区）锁定几何。协调者同款变异（含 Expanded 的 Row 生成时
  Padding/Gap 置零，overlay 独立副本编译 rc0）→ x=2 变为命中（A=2）红；
  恢复绿。不再以按钮内部点 x=40/250 顶替。
- **Def/Slot 正例**：TestR25R3RealDefSlotRuntime 采纳协调者合法形态
  （Def[Row[Expanded[Slot]]] + Page<Card><Button Click=TapA>）——
  生成/编译/离屏真实点击 CountA=1 PASS。**撤销 R25-R2「Def/Slot Codegen
  不支持」的错误结论**（旧负例的 card 从未实例化）。
- **裸 Slot 负例改名**：TestR25R2DefSlotParity → TestR25R2BareSlotNegative，
  范围准确收窄为「未经 Def 实例化的裸 Slot 负例」。

## S2（68efc90）：Exact/无界边界 + 竞争修正

- **PerFrameConstraintChange 修正**：click 后保持**当前阶段宽度**（第二阶段
  持续 500，不再每帧切回 300），逐阶段断言：300 x=250 命中 → 500 x=450
  命中 → 500 x=250 命中（内容跟随当前约束）→ 回 300 x=450 脱靶（约束
  回退生效）。
- **自身 Exact 边界**：Expanded Width=120 → x=100 命中、x=250 脱靶。
- **子节点 Exact 边界**：Expanded[Button Width=60] → x=30 命中、x=100
  脱靶（无子约束注入的退化=按钮拉伸命中=红，即 R25-R1 MUT-A 的运行时
  形态）。
- **无界主轴**：Gio Flex 实测语义——Flexed 子项获得全部剩余 Max（h=
  100000），真「无限」不可表达；登记为边界记录，不伪造自然高度目标值。
- **双实例缓存轨迹**：injectProbeRecs 注入 probeRecs0（无生产计数器），
  双实例交错 5 轮各录一次、绑定变化不引发静态重录、几何一致不串写。

## 门禁（candidate 68efc90）

- 全套（-skip RealWindow）**88.9% rc0**（r25r3-suite）；
- MinGW race `-race -count=1` rc=0（r25r3-race）；
- docgate rc=0（candidate dev-log 本文件覆盖锚点，r25r3-docgate-2）；
- gofmt 空、vet 干净。
