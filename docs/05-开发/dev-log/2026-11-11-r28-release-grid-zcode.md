# R28-B release-grid 复核/准备线开发日志（2026-11-11）

基线：组合验收分支 tip `611948d49525744cbb4127265983fd24236e6bf5`
（含已批准 ADR-0017 与 R28-B 键盘候选 f960dff）。
分支 `codex/r28-release-grid`。主库全程只读（HEAD 255c392，0 WIP）；
真实 Go 一层公共锁经 record-command 留痕。

## 本线提交

- `37133c8` — 原生键盘验收 fixture（默认不编译不执行）+ judge 脚本 +
  RUNBOOK + README 指引。

## 逐项落实

1. **fixture**：`pkg/widgets/datagrid_keyboard_native_test.go`
   （//go:build nativewindow）。真实公开 API：`pkg/runtime/app.RunWindow`
   （R27 原生验收同款入口，无内部注入）。10k 行 ROW-%05d 可核验 RowID；
   gridA 键盘 opt-in + gridB 禁用对照；真实 widget.Editor。留痕 JSONL
   每帧 flush：焦点（gridA/gridB/editor/none，两个可区分焦点消费者 +
   none）、选择与当帧可见性、滚动偏移、resize 与 pxPerDp、Editor 编辑
   长度增量（不记录用户输入内容）、未消费导航键（泄漏观察）、close/exit。
   放置于所有权内（datagrid* 文件 + DataGrid 自有 testdata）。
2. **判据**：离屏判据全部由既有套件覆盖（六键/连发=连续 Press/当帧可见/
   Editor 隔离/默认关闭/0 B/op 稳态——`TestDGStableLayoutZeroAllocs` 含
   0 B/op 字节口径），本轮未再发现可判别缺口，未新增离屏测试。
   原生操作表 T1–T13（RUNBOOK §3）明确「日志自动判定（judge R1..R8）」
   与「人工观察（IME 组合窗归属/视觉/卡顿/Tab 走向）」的分界；IME 检查
   是焦点隔离与编辑归属，不代表 DataGrid 提供 IME 编辑。
3. **编译与交接**：普通（CGO=0）与 race（CGO=1 MinGW 16.1.0）测试二进制
   编译并记录 SHA-256/大小；运行 argv、timeout、containment win-job +
   taskkill /T 收口办法、终止条件写入 RUNBOOK §2/§5。
   默认选择器不执行证明：build tag 首行 + 默认 `go test -v` 中 native
   出现次数 0 + `go list ./pkg/...` 无 testdata + 无 tag vet 通过。

## 门禁（readyRef 态 37133c8）

70 PASS / 0 FAIL / 0 SKIP；race rc=0；CGO=0 build rc=0；vet rc=0；
gofmt 空；coverage 95.4%；docgate+diagdoc rc=0；nativewindow 编译 rc=0。
台账 delivery-b/commands.jsonl：verify-evidence PASS（6/6）。

## 限制与诚实声明

- 原生键盘连发/键位映射、IME 组合窗、DPI 视觉、真实窗口显示与标题栏
  关闭：**未验证**（作者阶段不开窗、不执行 fixture）。
- 任务所述「同目录 README.md」在 product-r28/ 下未找到——按 common-task
  既有纪律执行；已在 handoff/slices 登记，请协调者确认所指。
- 二进制不提交（.exe 不入 git）；SHA-256/大小/构建命令已记录。
