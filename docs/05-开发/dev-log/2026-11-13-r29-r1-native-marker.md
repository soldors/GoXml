# R29-C R1：双窗 native fixture 判据收紧（HY4，2026-11-13）

- 复核 `windowhostnative` marker 与生产路径：`frame:B-after-A` 旧实现
  只检查 `close:A`（A 的销毁回调第一行），可能早于 `unmount:A` /
  `finalize:A`，不能证明「A 清理完成之后」。
- 修正 `main.go`：B 的 Layout 仅在 `finalize:A`（A 的 LifecycleFinalize
  返回之后写入，即 A 清理完成）已落盘且 `close:B`（B 收尾起点）尚未
  出现时写 `frame:B-after-A`。判定器 `ExpectedOrder` 保持
  `finalize:A < frame:B-after-A < close:B`。
- `judge_test.go` 增两条日志回放红证：把该 marker 前移到 finalize:A
  之前、后移到 close:B 之后，LogJudge 均须以 `marker-order` 拒绝。
  回放集合共 12 例 PASS（合成日志只证明判定器敏感，不冒充原生）。
- 两场景（natural / cancel-first）候选构建身份、状态字段与可复跑
  runner 见交付 `native-identity.json` / `runner.md`。真实 Windows
  标题栏场景待协调者组织；本轮未执行原生窗口。Linux/macOS 仅报告
  台账中实际验证的范围（本轮无实机，标未验证）。
