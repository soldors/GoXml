# R29-C R1 提交号登记（dev-log 维护）

本文件只用于满足 `internal/docgate` 的「近期提交须在 dev-log 中出现提交号」
机械校验；内容准确性由对应 dev-log 正文负责。

| 提交 | 说明 | 正文 |
|---|---|---|
| `7cdaab4` | fix(app): R29-C R1 关窗时序（首个 Event 就绪门闩 + 关闭请求由 loop 串行交付）与进程级 Gio Main 单宿主约束 | `2026-11-13-r29-r1-host-close.md` |
| `a4517ea` | test(app): R29-C R1 `frame:B-after-A` 收紧到 A 清理完成之后、B 收尾之前，判定器两条红证 | `2026-11-13-r29-r1-native-marker.md` |

首份 docgate 红（`r1-doc-diag` observed rc=1）即因这两个提交号当时尚未产生/未登记，
原始输出保留在交付台账，未改门禁凑绿；本文件补齐后重跑 `r1-doc-diag-2`。
