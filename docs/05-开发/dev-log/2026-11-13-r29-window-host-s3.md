# R29-C S3：双窗原生验收工具（HY4，2026-11-13）

- 切片：S3；父提交 = S2 `759a870`。新增专属 fixture
  `pkg/runtime/app/testdata/windowhostnative/`：`main.go`（nativewindow 标签，
  标签 A/B 双窗、各窗独立 Router/Owner、宿主候选显式收口）、`judge.go`
  （LogJudge 日志层 + Judge 完整层）、`judge_test.go`（回放 10 例）。
- marker 轨迹：scenario → open/frame A,B →（cancel-first 的
  background-cancel 只取消）→ close:A → unmount:A 一次 → finalize:A →
  frame:B-after-A（关 A 后 B 仍服务）→ close:B → unmount:B 一次 →
  finalize:B → exit:code=0。红证：early-exit / duplicate-cleanup /
  cross-stop 不可逆拒绝；runner rc、race、无残留、人工观察由执行者实测填，
  fixture 只内嵌 log_judge 并列 judge_fields_required，不代填。
- 本轮**未执行原生**：`CGO_ENABLED=0 go build -tags nativewindow` rc=0
  （产物 SHA-256 d91928c0…c730，10,016,768 B，只编译）、
  `go test -count=1 -v ./…/windowhostnative/` rc=0（10 PASS 回放自检，
  合成日志只证明判定器可红可绿）、`go vet` rc=0。
- 交付 `delivery-c/s3-native-tool.md`：marker/退出码/race/无残留判据、
  红证、可移交实机步骤。原生执行、native race、跨平台运行均为
  **未验证（pending）**，等协调者发布最终候选身份与人工授权。
