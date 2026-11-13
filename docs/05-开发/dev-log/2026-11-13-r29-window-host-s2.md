# R29-C S2：显式多窗口宿主候选（HY4，2026-11-13）

- 切片：S2；父提交 = S1 `d2737b9`。新增 `pkg/runtime/app/window_host.go`
  （最小显式 API：NewWindowHost / Open / Run / Wait / ExitAll / Active /
  WindowHandle.Close / Done / Err / Frames / Label），不改 app.go、
  process_entry.go、Router/Lifecycle 生产语义，不改旧单窗进程入口。
- 线程与所有权：Gio Main 由宿主 Run 调一次；每窗独立事件泵 goroutine
  = 该窗 UI owner 串行点（Layout、WithOnDestroy、LifecycleFinalize）；
  宿主不持有 Router，不代做 Finalize，不 os.Exit。最后一窗自然关闭 =
  drained（Explicit=false）；调用方 ExitAll(code) = explicit；两者可判别。
- 受控真实生产分支（`r29_s2_host_test.go`，5 用例 PASS）：两个独立
  Router 关 A 保 B 布局响应、显式退出、打开失败与重复关窗幂等、
  并发后台取消后各 UI owner 一次卸载、旧入口兼容（Main 一次性、
  RunWith / RunSingleWindowProcess 语义不变）。
- 变异取证：移除 DestroyEvent 回调后主用例 rc=1（unmounts=0），
  按字节 SHA-256 `7edba42c…c221`（8526 B）恢复后 S2 全套 rc=0；
  原始输出见交付 `raw/s2-mutation-red.txt` / `raw/s2-mutation-restored.txt`。
- API 候选、兼容影响、回滚点与待裁决项见 `delivery-c/s2-api-candidate.md`。
  多窗口 API 与退出策略**未批准**，等待协调者验收；本切片未开窗。
