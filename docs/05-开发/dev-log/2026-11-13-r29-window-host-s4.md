# R29-C S4：平台与发布边界、门禁与冻结（HY4，2026-11-13）

- 切片：S4；父提交 = S3 `6179335`。本切片只补 dev-log 与树外交付文档；
  生产代码/正式 ADR/共享台账不改动，主库合并与发布由协调者统一处理。
- 正式重负载门禁全部走 `scripts/record-command.py run --containment win-job
  --lock-mode acquire`（公共锁 heavy.lock，逐条释放），原始双通道在
  `delivery-c/raw/cmdlogs/`，台账 `commands.jsonl`：
  | id | rc | 结果 |
  |---|---|---|
  | r29-controlled-suites | 0 | app/router/lifecycle/windowhostnative 受控全包（含 S1/S2 与回放） |
  | r29-mingw-race | 0 | CGO=1 MinGW `-race`，`GORACE=halt_on_error=1`，双通道 0 DATA RACE |
  | r29-windows-build | 0 | `CGO_ENABLED=0 go build ./...` |
  | r29-doc-diag | 0 | docgate / diagdoc |
  | r29-vet | 0 | `go vet` app 与专属 fixture |
  | r29-gofmt | 0 | 改动 Go 文件 `gofmt -l` 输出为空 |
  `verify-evidence.py` 终检 6/6 PASS（19564 B，sha 6354e35b…）。
- 平台矩阵（详见 `delivery-c/platform-matrix.md`）：Windows CGO=0 编译与
  无窗口/race 已实测；Windows 单窗 R28 两场景原生已验（对照）；R29 双窗
  fixture 只编译 + 日志回放，**实机未执行**；Linux X11/Wayland、macOS
  Cocoa/Metal 需 CGO=1 与系统依赖（ADR-0007 / PIT-008），本轮未安装、
  未交叉编译、未实机运行，一律标未验证。给出可移交 runner 命令与预期结果。
- 宿主一次性初始化与线程规则：`app.Main` 只由 `WindowHost.Run` 调一次；
  每窗一条事件泵 goroutine = 该窗 UI owner 串行点；宿主状态用 mutex；
  `ExitAll` 只发关闭请求，清理由各窗回调完成；最后一窗自然关闭只
  drained、不自动 os.Exit（进程级动作由调用方/fixture 显式决定）。
- API 取舍、兼容影响、回滚点见 `delivery-c/s2-api-candidate.md` 与
  `handoff.md`。多窗口 API 与退出策略为**候选**，等待协调者验收；
  原生实机、native race、跨平台、DPI/IME、发布级性能列入 pending。
