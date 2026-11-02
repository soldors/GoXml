# R6-B Input 独立复核（2026-11-02）

作者ready b04af77（测试334f55e）保持生产算法不变，独立修正其双Input测试的错误fixture。该fixture缺IdentityText方法，单Input也会报GXML-EVENT-003；不能称Analyzer禁止双Input共享字段。改用真实probeVM后，同字段双Editor实际生成、布局并同步为x，count=2；另有不同字段隔离和精确缺方法诊断对照。独立11个窄测试全绿，无Skip、无原生窗口，公共锁真实释放。

候选1925d1d技术BLOCK：MaxLen=1与AppendText叠加，一次用户Edit之后quiet帧4至8仍持续通知，count=2..6，模型AbA→AbAAAAA而编辑器保持A。真实生成、真实Router，编译成功并在目标有界性断言上红；恰好8帧停止。候选原“截断只通知一次”及一般非幂等有界声称不成立。未修改候选生产算法，保持其未批准、不可合入状态。

同帧外部写XYZ加用户替换首字符X的碰撞尝试，实测delta=1，用户事件正确通知。首次预期为0是本方驱动错误，首败原件保留；修正后作为合法对照通过，不把它算作候选缺陷。两Input真实方法也在候选生成/布局通过。

作者两次未skip全包会进入无guard的TestButtonInRealWindowRuns，其源码调用app.RunWindow；没有历史OS事件原件，实际窗口数未知，不能说0。两次未持共享锁及前期缺少强制空hooks/GOXML_AUTO_PUSH设置的违规保留。共享post-commit实际存在，默认env不为1时不推；历史env与原始提交输出缺失，不能据worktree未配置hooksPath声称没有hook触发面，也不能由可能性称已推送。独立复核只做安全窄测，未恢复任何窗口/采样任务。

证据位于night-product-r6/coordinator/review/input的ready-corrected-2与final-candidate-evidence。candidate独立红例单独冻结为审查证据ref，不能随ready进入产品；本次只接纳测试与事实文档，不作Input公开语义裁决。
