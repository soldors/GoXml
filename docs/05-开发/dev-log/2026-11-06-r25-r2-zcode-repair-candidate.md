# 2026-11-06 R25-R2（ZCode）candidate 分支返修记录

candidateRef 谱系：234bbbf → 9102bcf（R25-R1）→ eb8fffc（S1/S2）→ ab33e8e（S3）。
基线主库 02f0385（C 发布后）只读；R25-R1 双 ref 原材料保留。

## S1（eb8fffc）：F-B4/P1 属性盘点显式拒绝

Expanded 自身准入属性盘点（NodeIR 字段 × Expander 分派）：Width/Height（支持，
尺寸职责）之外——Background/Padding 到达 NodeIR（codegen 层显式拒绝）；
Gap/Align/Justify 被 Expander 以 GXML-ATTR-005 拒绝；事件 ATTR-004。
守卫：TestR25R2ExpandedAttrRejections（2 codegen 拒绝 + 3 ATTR-005 上游 +
1 ATTR-004 事件）+ TestR25R2ExpandedWidthHeightStillSupported（尺寸职责
不消除报错凑绿）。判别力：移除 Background 拒绝 → Background 守卫红
（字节相同=静默丢失，协调者探针同判据）。

## S2（eb8fffc）：F-B3/P2 实质运行时矩阵

r25r2_matrix_test.go 七场景（rtProbe 离屏 + 真实指针事件，探针全部墙钟内）：
RowBothEnds（双弹性 A/B 各自命中）、ColumnFlex（rigid 150 上 y=175 命中）、
GapPaddingOffsets（Padding=6/Gap=4 偏移命中）、PerFrameConstraintChange
（300→500 宽逐帧约束，x=250/x=450 先后命中）、IfFlip（分支翻转同坐标
命中不同处理器）、ForRows（三行 Expanded 点击收到正确 item.ID）、
DualInstance（双实例交错，A=2 B=0 隔离）+ DefSlotParity（Slot 缺口对等
拒绝登记——Plain Row 同拒，缺口先于 Expanded）。
判别力：协调者零约束变异（Constraints 置零）→ 七场景全红（A=0/B=0 各场景
脱靶，-race 下复现）；恢复 PASS。原点击守卫保留。

## S3（ab33e8e）：F-B5/P2 成本对照

- generated 稳定 Layout（临时模块 rtBench）：Row[Text, Expanded[Button]]，
  预热 3 帧计时外 → **3687 ns/op 1615 B/op 0 allocs/op**。
- 手写 Gio 对照（等价 Flex{Rigid, Flexed} 同主题/约束）：
  **1802 ns/op 682 B/op 0 allocs/op**。差值=代码生成层包装
  （vm/click 字段+槽位决策），**稳态 0 allocs 两形态一致**。
- 多样本逐字节回归：4 样本（静态/Exact-Fill/动态 If+For/Padding 装饰）
  基线 95173db vs 候选全一致——替代 R25-B 单样本。
- R25-B 两个 Generate bench 保留并标注 Generate 层范围（Expanded 组含
  Button/VM，与 Plain 差值不得当 Layout 成本）。

## 门禁

- candidate 全套（-skip RealWindow）**88.7% rc0**（R25-B 记录沿用+本轮
  S1/S2/S3 新文件后复跑 rc0）；
- MinGW race `-race -count=1` rc=0（r25r2-race；含七场景矩阵与零约束变异
  红在 -race 下复现）；
- docgate rc=0（r25r1-docgate-2 candidate + r25b-docgate-3 ready）；
- gofmt 空、vet 干净。
