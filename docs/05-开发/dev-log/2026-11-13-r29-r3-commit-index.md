# R29-C R3：提交索引（docgate）

本线在 `codex/r29-r3-hy4-window-host` 上的独立提交：

| 提交 | 内容 |
|---|---|
| `f657241330aa318e7961e9c2c4aa8099cb975eaa` | `fix(app)`：注册与启动分离，先认领 Gio Main 再启动任何窗口事件源；`ExitAll` 补 `hostStarting` 分支；协调者 R3 探针与 R1/R2/S2 受控用例按新语义显式启动 |
| `239b5eace4b82da3d0b14e711b204be02f986099` | `feat(app)`：候选 `ProcessOptions.OnMainConflict`（零值保持 R28 退出契约），含两套收口对照与健康对照 |

详细说明见 `2026-11-13-r29-r3-startup-order.md` 与
`2026-11-13-r29-r3-process-conflict.md`。
