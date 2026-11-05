# 2026-11-05 R23-R1-B FONT-TIMING-TEST-CLOSURE（DeepSeek）dev-log

固定基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`（开工核验 clean，主库 porcelain 38，索引空）。分支 `codex/r23-r1-deepseek-font-timing`，树 `r23-r1-deepseek`。范围：`pkg/runtime/theme` 测试 + 专属字体验收文档；不改通用总览/WBS/README；不接管 Router/CLI/编译器。
记录提交：`bab7a35`（base）→ `193a301`（B1/B2 测试修正+守卫）→ `a466419`（B3 执行方案）→ 本 dev-log。

## B1 先定性质（不靠重复直到绿）

- 读 `coordinator/b/review.md`、`baseline-clock/result.json`、`regression` 首败/`regression-repeat`、`tested-go-files.json`、R23-B 字体差距矩阵、TASK-FONT-001-D/IMPL-1/2/3/ACCEPT。
- **性质判定**：`TestFontLazyLoadTempBuiltOnceConcurrent` 的 `TempElapsed > 0` 断言是**计时前提问题**，不是生产并发缺陷：
  - `TempElapsed` 是墙钟测量（`lazy.go:328-330` `time.Now()`/`time.Since`），32 goroutine 争用时临时主题构建可快于本机时钟分辨率（~0.5ms）→ 读成 0。
  - **本轮实测复现**：并发用例 `-count=100` → **9 FAIL，失败值恒为 `TempElapsed = 0s, 期望 > 0`**。与协调者 baseline-clock 100 次 96PASS/4FAIL 同源。
  - **与共享 Provider 互斥修复关系**：基线已含 R23-B 的 `embMu`（loadEmbedded 串行化）；该 flake 在**有无 embMu 均出现**（baseline-clock 仅还原 font.go 仍 4FAIL）⇒ 非互斥引入。业务判据（只构造一次 `cl.calls==1`）不因 0 而失败。
- **明确关闭**：本次只关闭"并发用例里依赖时钟分辨率的 `TempElapsed>0` 测试前提"。**保留**的正式性能门槛：M1 阈值、sync-vs-lazy 正式比较阈值、跨平台、第二成功 family —— 均**未定/未闭环**（B3 决策输入）。

## B2 有判别力的可靠性修正

改 `pkg/runtime/theme/lazy_test.go` + 新增 `pkg/runtime/theme/font_timing_r23r1_test.go`。**未改生产代码**（不 clamp、不加 sleep 到生产、不改计时语义、不解冻 `font_lazy_strict_test.go`）。
- **并发用例**：保留业务判据 `cl.calls==1`、`Attempts==1`、`TempSource==embedded-subset`；把 `TempElapsed>0` 改为**只记录**（0 合法，低于分辨率不判失败）。
- **写路径守卫 `TestFontLazyLoadTempElapsedWrittenFromMeasurement`**：注入**可控慢嵌入加载器**（delay 20ms，远大于分辨率）→ 单 Lazy → 断言 `TempElapsed ≥ delay/2`，证明 `l.tempElapsed = time.Since(start)` 与 `l.rep.TempElapsed = l.tempElapsed` 两处写入真实发生。
- **合法零值守卫 `TestFontLazyLoadTempElapsedZeroIsLegal`**：快加载下 `TempElapsed` 允许 0，只断言非负 + `cl.calls==1` + `TempSource` 正确；区分"合法零"与"未写入"。
- **可编译变异**（独立源码副本 + Go overlay，不污染生产）：
  - 变异A `l.rep.TempElapsed = 0` → 写路径守卫 **FAIL**（`TempElapsed = 0s，期望 ≥ 10ms`）；恢复 PASS。
  - 变异B `l.tempElapsed = 0`（保留 `_ = time.Since(start)` 保编译）→ 写路径守卫 **FAIL**；恢复 PASS。
  - 两变异下**并发用例仍 PASS**（无假阳性，业务判据独立）。
- **有界重复（预登记）**：并发用例 `-count=100` → **100/100 PASS**（原 9/100 FAIL，flake 消失）；写路径+合法零守卫 `-count=20` → 全 PASS；Temp 族 `-count=50` → 全 PASS。**未靠重复直到绿**：先定性为计时前提，再拆断言 + 加判别守卫。

## B3 正式字体验收执行方案
新增 `docs/03-架构/字体正式验收执行方案-R23R1B.md`：复用既有 `scripts/font`（`font-init-v1`）与 `m1nightprobe`（comparison.go，真实 NewApp/NewLazyApp/FirstFrame）。方案含：被验 SHA/二进制身份；none/sync/lazy 模式与**同源起止点**；Ready 与 FirstFrame **分别判定**；独立进程/Provider 冷缓存；真实 argv/退出码；无效/失败样本保留；窗口启动/关闭与人工观察项；历史 90 样本与新批次**不混用**；**决策输入**（正式阈值/跨平台/第二 family/IMPL-3 解冻）各自给建议与代价、**不擅定阈值**。**不开窗、不采样、不安装**；不声称比较机制或真实 App M1 探针不存在；原 `font_lazy_strict_test.go` 不解冻。

## 门禁（本机 Windows/amd64）
`pkg/runtime/theme` 全包 `-count=1` ok；`pkg/runtime/app` 字体/懒加载无窗口族 ok（排除 RealWindow/RunWindow）；`go vet` rc0；`gofmt -l pkg/runtime/theme` 空；`docgate` ok；覆盖 **theme 86.7%**。
`-race`（MinGW gcc 16.1.0，`CGO_ENABLED=1 CC=… GORACE=halt_on_error=1`）：theme 全包 `-count=3` **0 DATA RACE**；Temp 计时族 `-count=5` 0 DATA RACE；app 字体/关闭族 0 DATA RACE。原始 argv/rc/env 见 `raw/race/`（record-command 的 `GO_ENV_FIXED` 固定 CGO_ENABLED=0 且 `--env` 不透传，race 用直接子进程取证，非 race 门禁走随仓 recorder）。

## 未完成 / 边界（不得写成通过）
跨平台（Linux/macOS）未跑；M1 阈值未定；sync-vs-lazy 正式比较阈值未定（只准备方案）；真实窗口人工观察本轮未开窗；第二成功 family 未实现；IMPL-3 冻结核 `font_lazy_strict_test.go` 未解冻；证据本机执行，独立复核待协调者。
`7f6621c` 历史 90 样本属旧批次/条件，不与本轮混用。

## 五态
实现 done(`a466419`+dev-log) / 自测 pass / 独立复核 PENDING / 主仓未合 / 远端未推送。
