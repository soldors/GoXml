# 2026-11-06 R25-A 集中窄返修 R2 dev-log（DeepSeek）

父冻结 `7fee57dccebe465841c59f21bb975e619f6660bf`（repair-r1，独立复核 BLOCK 于 F-A4/F-A5）。主库/远端 master `02f0385ad3781be9a50c6a2fa56f15a82f620692`（C 已发布，9 WIP 原字节不动，不回滚）。分支 `codex/r25-deepseek-repair-r2`，树 `r25-deepseek-repair2`。仅关闭 F-A4/F-A5（原 F-A1 剩余分叉）。旧冻结/旧 raw/作者树保留不改。
记录提交：`7fee57d`（父）→ `97b2c02`（F-A4/A5 生产修复+测试）→ 本 dev-log。

## S1 立红、定案（同材料复现）
- **F-A4 父目录删除**（`independent-probes-2/raw/a-parent-and-owned`）：磁盘 `ui/assets/a.png` 合法，overlay 将 `ui/assets`→`""`。上一修复只查 `overlay[Clean(文件abs)]`，祖先键未命中 → 门禁读盘 → **默认生成 rc0 写产物**；`--check` 才被 Go embed missing 拒（`pattern assets/a.png: no matching files found`）。Go 1.26 `fsys.stat` 对祖先替换为删除表现为后代不存在；输入稳定，非 TOCTOU。
- **F-A5 owned/候选冲突**（`final-controls/raw/a-owned-final-truth`）：先正常生成建立 owned `home_gen.go`；页面 `<Image Src="home_gen.go"/>`，用户 overlay 把该 owned 输出映射到合法 PNG。资产门禁用**原始用户 map**（读 PNG），而 precommit 候选 overlay 对同路径**优先**（候选 Go 源），`--check` rc0 写盘；移除 overlay 后真 `go test ./ui` 仅构造 `NewGoxmlHomeView` → `EFFECTIVE-ASSET-PANIC: decode embedded asset home_gen.go: image: unknown format`。两阶段优先级未统一。

## S2 最小修复 + 判别回归（`97b2c02`）
- **F-A4**：新增 `resolveEffectiveAsset(fullAbs, overlay)` 按**最长前缀祖先**解析有效来源：`effFile`（精确替换→校验目标字节，沿用 `readOverlayAssetBytes` 预算）、`effDeleted`（精确/祖先删除→fail-closed 缺失）、`effUnprovable`（祖先替换为非空目录→保守 fail-closed）、`effNotOverlaid`（读盘）。门禁 `readAssetBytes` 与 K12 `assetsFingerprint`/`ReadAssetForFingerprint` 共用同判定。
- **F-A5**：`pipeline.Config.AssetOutputPaths map[string]bool` = 本批 `resolved[].target` + Manifest `Entries[].Output`（绝对化）；`readAssetBytes` 在写盘前若 Src 解析到输出路径 → fail-closed（有效字节是生成 Go 源，不可能为图片）。CLI `runPipeline` 构造该集合并注入两处 `pipeline.Config`（生成/预检）。**不扩可信目录、不改 Codegen、不新增诊断号、无生产变异开关、只读 overlay 目标/祖先映射（不无差别重读全部）。**
- **测试**：`internal/cli/asset_overlay_r2_test.go`（祖先删除 default+check 均 fail-closed 不写；祖先替换 unprovable 不写；**无关未用 overlay 不误拒**；owned-output Src default+check 均写盘前拒且旧产物保留）；`internal/compiler/pipeline/asset_overlay_effective_r2_test.go`（`resolveEffectiveAsset` 五类 + `readAssetBytes` 祖先删除/替换/outputPath fail-closed、未 overlay 读盘）。**保留旧 10 个 R25 用例 + embed-truth 绿**（`-run TestR25` 全 PASS）。
- 修复后协调者同探针（`a-parent-probe.go`）：F-A4 default/check 均 rc2 `CODEGEN-ASSET-002 deleted (exact or ancestor)`；F-A5 rc2 `CODEGEN-ASSET-002 resolves to a generated/owned Go output`；均不写盘。

## 行为变异（独立副本 + Go overlay；健康守卫红→恢复绿）
- **MUT-A4**（`assets.go` 关闭祖先解析，仅精确文件）→ `TestR25OverlayAncestorDeleteRejected` **FAIL**、`TestReadAssetBytesOverlayFailClosed`（pipeline 单元）**FAIL**。
- **MUT-A5**（`readAssetBytes` 忽略 `outputPaths`）→ `TestR25OwnedOutputAsSourceRejected` **FAIL**。
- 恢复（无 overlay）→ 全 PASS。不以编译失败/flags/任意非零算红。

## S3 门禁（Windows/amd64，CGO=0）
cli（R25+asset/cache/overlay/batch combo）ok、pipeline 全 ok；vet rc0；gofmt 空；diagdoc ok；docgate ok；覆盖 **cli 86.4% / pipeline 85.0%**（repair-r1 为 86.3%/77.7%，pipeline 提升来自新单元）。先跑阻断窄测再跑门禁；新路径（祖先删除/替换、output-path）均有专属断言。未触碰 Codegen/Expanded、字体、Router 预算、ThemeRef、collector；未新增公开语义/诊断号；无缓存结构/输出修订变化（`gen-3`/cacheFormatVersion 不动）。

## 未证边界
Linux/macOS 未验；无原生窗口/安装/性能采样；硬链接原始位置/TOCTOU/解码器漏洞/解压炸弹沙箱/未支持格式 未覆盖；ancestor-replace 目录用例本机更早由 go list 拒绝（写盘前），资产门禁 unprovable 分支由单元测试直接覆盖；overlay 目标指向包外属构建方显式输入。

## 五态
实现 done(`97b2c02`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
