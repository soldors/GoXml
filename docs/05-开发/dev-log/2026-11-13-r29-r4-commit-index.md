# R29-C R4：提交索引（docgate）

本线在 `codex/r29-r4-hy4-window-host` 上的独立提交：

| 提交 | 内容 |
|---|---|
| `ce6b945c8f2e7138f0e0ef4d12189c7fb0ef773b` | `fix(app)`：并发 Run 结果线性化——`runVerdict` 发布点先于 Gio Main；等待者共享同一判定；协调者并发探针原样纳入 |
| `3566193efca47488f9ebbbd1869d68497d2a351e` | `test(app)`：OnMainConflict 候选的真实子进程退出码（默认 `os.Exit(1)` vs `MainConflictReturn` rc=0） |

详细说明见 `2026-11-13-r29-r4-run-verdict.md` 与
`2026-11-13-r29-r4-conflict-console.md`。
