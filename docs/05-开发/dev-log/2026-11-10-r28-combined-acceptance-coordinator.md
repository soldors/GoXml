# R28 组合验收协调记录

本日志登记 R28 三条已独立复核线路在组合树中的接纳与门禁范围：

- `3d594cf`：Hydration generation 关联合同候选合入组合树。
- `7e69a36`：DataGrid 键盘导航候选及独立复核基线。
- `d25136d`：DataGrid 候选合入组合树。
- `bf28b1e`：Hydration generation 并发与迟到响应回归。
- `f960dff`：DataGrid keyboard Page clamp 边界回归。
- `b8759da`：Hydration 收口测试合入组合树。
- `4f4ac49`：DataGrid 收口测试合入组合树。
- `67894e1`：Lifecycle combination 候选合入组合树。
- `059f360`：Lifecycle 收口合同探针合入组合树。
- `7973cc8`：在全仓并发负载复现后放宽既有进程收口测试的夹具预算，不改生产行为。

组合树仅用于最终联合验收，主库 `255c3925` 未修改。联合测试、MinGW race、全量构建、vet、gofmt、diagdoc 与 docgate 由协调者在组合树重新执行；原生窗口、跨平台运行、公开 API 与默认策略仍按各线交付材料列为未批准或未验证事项。

最终组合复跑期间，`internal/devx/watch/TestStopTimeoutTreeKill` 在全仓并发负载下两次触发 400ms 夹具超时，独立 `-count=5` 又通过。仅将测试夹具的 `StopTimeout` 从 400ms 放宽到 2s（仍有 5s 上界与进程收口断言），定向 `-count=5` 与最终全仓测试均通过；未改生产代码。两次原始失败记录保留在 coordinator `combined-review-final/`，终态通过证据在 `combined-review-final3/`。
