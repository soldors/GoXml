# R6-A 独立复核（2026-11-02）

作者ready521a51b仅有测试与日志，无Background。原固定/Fill/无界探针只看内容原点；独立overlay移除固定尺寸生成后这三条仍绿，不能完整证明固定外框次序。新增padding_order_review_test.go用首字形横坐标与后继兄弟纵坐标锁定120×80外框，再内缩Padding10。现行实现绿；移除固定尺寸或仅高度的可编译overlay均行为红；产品源码字节未改。

候选9930729/f34af98分别内容框/外框，仍未批准。本次独立Text与Input半透明像素验证以直接Gio paint.Fill作同后端对照，alpha=128、邻居透明、两帧重放正确；另跑带背景Input真实焦点/Edit/分帧Return与quiet-frame，以及Button两实例命中。没有原生窗口/采样/全包重跑。完整argv/时间/rc/raw与锁实放见night-product-r6/coordinator/review/layout。

decision.md仍写4aaf269是测试父提交，最终ready为521a51b；97a99c9仅旧未发布候选，不能当作正式兼容基线；CSS类比不作为本次技术裁决证据。语义A/B由负责人裁定，独立技术审通过不代表批准。

Reviewer过程披露：一次gofmt调用cwd漏设而找不到文件（未修改其它树），一次插入探针错落到raw字符串内导致编译失败，RGB首次硬编码128忽略后端编码与直接Gio控制对照不符，均非产品缺陷。另一个探索Fill/Align探针因未确认预期而红，仅保留外部草稿不纳入产品结论；既有Fill行为不因此改写。所有相关首败日志保留。最后有效测试已恢复绿。
