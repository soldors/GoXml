# R28 Windows/amd64 源码发布（2026-11-13）

- 文档提交 204998b327dc4a576519c47e26c333fa6825b39b 更新 CHANGELOG、README、文档索引、AGENT.md 和 DEVX 任务卡。受验代码为 35b61b519165339d55e37053c3230b6444efbb79，之后的提交仅修改 Markdown。
- Windows 组合验收覆盖全树测试、10 包 race、零 CGO 构建、vet 和文档门禁。process 覆盖率为 86.2%，watch 为 89.4%。DataGrid 补验 run 为 candidate-native-grid-20261113-021654。Lifecycle 单窗两场景沿用 19c86a1486506e9453c9cae79de74075712a0c26 的原生证据，其构建输入与受验代码相同。
- 本次发布范围为 Windows/amd64 源码。Hydration 保持开发态 opt-in，公共 CLI 自动恢复尚未接线。跨平台实机、多窗口及整应用性能仍待验收。
