# 2026-11-04 R21-A GO-OVERLAY-CONSISTENCY 第二次窄返修（DeepSeek）dev-log

父提交 `e8f0a03cbb0674b935ba970c07aba7b84263bb35`（初审三反例已独立转绿，**保留该修复与覆盖**）。新隔离分支 `codex/r21-deepseek-overlay-rework2`，树 `r21a-deepseek-rework2`。旧提交/证据保留。
只拥有 `internal/cli` 与直接回归、dev-log。

## 本次两个新分叉（review-a-rework.md + additional-repros/result.json）
| 情况 | Go 权威 | 父 `e8f0a03` | 根因 |
|---|---|---|---|
| overlay 同时映射 `target/doc.go` 与 `./target/doc.go` | rc1 duplicate paths | rc0 静默生成 `q` | `userOverlayBytes` 归一化后重复路径静默覆写 |
| overlay 含当前查询用不到的缺失替换文件 | rc0 `q [doc.go]` | rc2 预读缺文件报错 | 重建 overlay 时预读**全部**替换字节 |

## 修复（commit `cd60434`）

**overlay 改为「路径映射」而非「预读字节」**：
1. `userOverlayBytes` → **`userOverlayMap(flags, moduleRoot) (map[string]string, error)`**：返回 `from(abs,Clean)→to(abs 替换路径；"" 删除)`；**不读取替换文件内容**。fail-closed：
   - **空路径键** → 错误；
   - **归一化后重复 from**（`target/doc.go` 与 `./target/doc.go`）→ 错误（稳定诊断，零写盘）；
   - 读不到 overlay / 坏 JSON → 错误（可消费失败）。
2. `resolveTargetPackageGo(..., overlay map[string]string, owned map[string]bool)`：构建 `go list -overlay` JSON 时——**用户条目直接使用其替换路径/删除语义**交给 Go 权威查询；**工具自有条目（owned）用 `//go:build ignore` 临时占位**。不再预读整份映射。
3. **逐文件校验只读「Go 选中的文件」的替换源**：`to==""` 跳过（删除）；否则 `os.ReadFile(to)`（选中源缺失即失败）；非 overlay 文件读磁盘。单查 `resolveTargetPackageGo` 与批查 `validateBatchObject` **一致**。
4. `effectiveTargetResolutionWithUser` / `effectiveUserGoFiles` / 批查 `mergedOverlay` 同步为路径映射 + `mergedOwned`。
5. **用户 overlay 每运行只读一次**（pipeline `userOverlayMap` 调用 1 次），有界；不再重复复制全局替换字节。

**缓存**：overlay 无法完整测量时沿用既有 K11 保守 miss；未改 K10/K11/K12；`cacheFormatVersion` 仍 **8**。

## 两新反例先红后绿（真实 Go + CLI；raw/rework2-raw/new-blockers.json、prefix-red.json）
| 反例 | 父 `e8f0a03`（红） | readyRef（绿） |
|---|---|---|
| duplicate | rc0 静默生成 `q` | **rc2 fail-closed**「用户 overlay 路径归一化后重复：\"target/doc.go\" 与 \"./target/doc.go\"」，**零写盘**（产物/Manifest 未创建） |
| unused missing | rc2「读取用户 overlay 替换文件 …absent.go: 不存在」 | **rc0 生成 `package q`**（不预读未用替换） |

## 全回放（raw/rework2-raw/full-replay.json）
- **原三反例**：virtual→`q`；ignored→rc2 不写盘；ownership→`q`（保留）。
- **单查 vs 批查**：single p1=`q1`；batch p1=`q1`, p2=`p2`（一致）。
- **`--check` 最终候选覆盖**：通过（用户 overlay 改名场景）。
- **合法删除**（overlay `""` 删除 p/a.go）：rc0。
- **缓存**：冷生成→真命中，bytes/mtime/Manifest 不变。
- **25 目标查询次数**：**2 次** go list（无退化）；`userOverlayMap` pipeline 调用 **1 次/运行**（有界）。

## 判别回归 + 变异红（`internal/cli/overlay_rework2_r21_test.go` 及既有）
- `TestR21OverlayDuplicateNormalizedPathFailClosed`、`TestR21OverlayEmptyKeyFailClosed`、
  `TestR21OverlayUnusedMissingReplacementNotBlocking`、`TestR21OverlaySelectedMissingReplacementFails`；原 `TestR21Virtual…`/`…ExcludedFile…`/`…Ownership…` 全绿。
- **变异红**：关闭重复检测 → `TestR21OverlayDuplicateNormalizedPathFailClosed` FAIL（`归一化重复路径必须 fail-closed`）；恢复全绿。
- 既有 API 变更同步更新：`overlay_blockers_r21_test.go`、`overlay_consistency_r21_test.go`、`target_batch_authority_review_test.go`、`target_package_s1_test.go`（`resolveTargetPackageGo` 新增 owned 参数）。

## 门禁
`go build ./...` ok；`go vet ./internal/cli` rc0；`gofmt -l internal/cli` 空；`go test ./internal/cli -cover` = **86.2%**；台账 verify PASS（4 条工具记录）；每命令锁 token 释放（lockRelease=true）。

## 五态
实现 done(`cd60434`) / 自测 pass / 独立复核 PENDING（协调者重放两新反例+原三反例+组合门禁）/ 主仓未合 / 远端未推送。
