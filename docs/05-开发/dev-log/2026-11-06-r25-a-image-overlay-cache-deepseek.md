# 2026-11-06 R25-A IMAGE-OVERLAY-CACHE-SAFETY（DeepSeek）dev-log

固定基线 `95173db73c9a0dc4d2d204a8aec4f808e79dead3`（开工核验 clean，主库 9 WIP，索引空）。分支 `codex/r25-deepseek`，树 `r25-deepseek`。所有权：`internal/cli` 资产/overlay/缓存及专属测试、`internal/compiler/pipeline` 资产校验及专属测试；安全边界补正建议；本 dev-log。codegen/layout 归 ZCode 未触碰；collector/font 工具未动；`font_lazy_strict_test.go` 未动。
记录提交：`95173db`（base）→ 8c1b8a4（组合回归 + 边界补正建议）→ 本 dev-log。

## A1 事实矩阵（真实 Go + 真实 CLI，见 deepseek/raw 与 fact-matrix.md）
冷生成→真命中（产物字节/mtime/Manifest 不动）；同路径内容变→安全 miss 重生成；删除/损坏→rc2 miss 拒旧保留；恢复→命中；大小写错拼→miss 拒绝（既有用例）；`--no-cache` 冷批坏资产→整批不落盘；暖批坏资产→整批拒绝保兄弟/Manifest；overlay 指向资产仍读磁盘真相（不替换 embed 字节）；跨包 out-dir 资产根=生成包目录。
**定性**：Cache 键含资产内容指纹（K12，`build_inputs.go:assetsFingerprint`→`cache_wire.go` 并入；读失败 `dependencyUnsafe` 保守 miss）；资产门禁先于 Codegen（`pipeline.go` 两处 validate），失败不写盘保旧；overlay 不改 embed 资产字节。**跨阶段一致，基线无生产违例**（不硬造修复）。

## A2 安全边界补正建议（不改原 WIP；见 docs/05-开发/2026-11-06-R25-A-安全边界补正建议.md）
9 WIP 中「安全边界与威胁模型.md」提案 §3.8 末「缓存命中例外：缓存键不含资产依赖…待接入」**已过期**（K12 已接入）。给出替换段落与如实保留的未覆盖边界（硬链接/TOCTOU/解码器/解压炸弹/未支持格式）。**不新增威胁模型承诺**，共协调者三方归并。

## A3 组合回归 + 行为变异（新增 `internal/cli/asset_overlay_batch_r25_test.go`，7 用例）
基线绿（`-run TestR25` 全 PASS）。两条**可编译行为变异**（独立源码副本 + Go `-overlay`，不污染生产）：
- MUT1 K12-off（`cache_wire.go` 不并入 assetsFingerprint）→ Delete/ContentChange **FAIL**（假命中）。
- MUT2 双门禁-off（`pipeline.go` validateAssets+validateAssetsDoc 关）→ BatchNoCacheBrokenAtomic/BatchWarmMixedPreserves **FAIL**（坏资产仍落盘）。
- 恢复（无 overlay）→ 目标 PASS。原件/raw 全留盘（deepseek/raw/mut/、deepseek/raw/probe-*.log）。

## 门禁（Windows/amd64，CGO=0）
受影响 `internal/cli`（asset/cache/overlay/batch combo）+ `internal/compiler/pipeline` test ok；`go vet` rc0；`gofmt -l` 空；`diagdoc` ok；`docgate` ok；`internal/cli` 覆盖 **86.4%**（既有基线，未为凑数堆无关测试）。无窗口/无采样/无安装/无新依赖。
> 记录：本次唯一"失败"均来自临时探针的**未设 workdir 误跑主库**（无新文件→无测试），已改用 worktree workdir 复跑，属取证过程，非行为红。

## 未证边界
Linux/macOS 未跑；无真实性能采样；解码器漏洞/解压炸弹沙箱/未支持格式未覆盖；TOCTOU/硬链接原始位置未证；并发同目标写未审。本轮不宣布整体安全验收通过。

## 五态
实现 done / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
