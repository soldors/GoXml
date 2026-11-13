# R28 DataGrid Windows 原生补验（2026-11-13）

- 0864415546398efaed03ba78b77015886848294d 同步 ADR-0017、TASK-WIDGET-001、文档索引与 AGENT.md 的实机验收结果。
- 代码候选 35b61b519165339d55e37053c3230b6444efbb79，run ID candidate-native-grid-20261113-021654。操作者确认四项，日志 241 次选中行变化均可见，DPI 150% → 125% → 150%，实际退出码 0、无 DATA RACE、无残留进程。
- 本次验收范围为 Windows/amd64 本机。布局及 IME 提交由现场观察，报告登记了 fixture 日志能证明的内容。发布身份以协调者记录和 Git 为准。
