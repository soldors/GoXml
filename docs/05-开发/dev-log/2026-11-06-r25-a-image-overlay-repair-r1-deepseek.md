# 2026-11-06 R25-A 集中返修 R1 dev-log（DeepSeek）

父冻结 `38c2657170f2d0f62786d428cfcf98b8e80cd4e4`（协调者 BLOCK）；主库/远端 master `02f0385ad3781be9a50c6a2fa56f15a82f620692`（C 已发布，9 WIP，字节不动，不回滚）。分支 `codex/r25-deepseek-repair-r1`，树 `r25-deepseek-repair1`。仅关闭 F-A1~A3。旧报告/raw 不回改，追加新批次。
记录提交：`38c2657`（父）→ `531be90`（F-A1/A2 生产修复+测试）→ 本 dev-log。

## F-A1 资产校验与实际 Go embed 分叉（真实缺陷，已复现并修复）
- **复现**（本机 Go 1.26.1，我的 gateway 探针）：磁盘 `a.png` 合法，`encoding/json` 生成的 overlay 把该 abs 路径映射到坏字节 replacement，GOFLAGS 整项引号含空格（`-mod=readonly "-overlay=..."`）。修复前 `goxml generate home.gxml --check` **rc0**、"提交前检查通过"、"已生成 ui/home_gen.go"——门禁按**磁盘**字节判通过。
- **根因**：真实 Go `-overlay` 替换 `//go:embed` 字节；但 `assets.go` 资产门禁与 `assetsFingerprint`（K12）只读磁盘字节 ⇒ "校验一份、嵌入另一份"分叉。协调者 runtime-confirm 亦证构造期 panic `decode embedded asset assets/a.png: image: unknown format`。
- **修复**（`531be90`，最小、fail-closed、不扩信任边界、不改 Codegen 模板、不新增诊断号）：
  - `pipeline.Config.AssetOverlay map[string]string`（abs 磁盘路径 → abs 替换路径，`""`=删除）。
  - `readAssetBytes`：Src 路径/边界/拼写校验通过后，若该 abs 命中 overlay → 校验**有效字节**：删除→fail-closed 缺失（CODEGEN-ASSET-002 read diag）；替换文件须普通文件且受同一 `MaxAssetBytes` 预算；格式仍由 `ValidateAssetBytes` 判（坏字节→CODEGEN-ASSET-002）。
  - `ReadAssetForFingerprint`/`assetsFingerprint` 接收 overlay ⇒ K12 缓存键以**有效字节**入键，overlay 有效字节变化→安全 miss。
  - CLI `runPipeline` 复用只读 `userOverlayMap` 注入 `resolvedInput.assetOverlay`（缓存键）与两处 `pipeline.Config.AssetOverlay`（预检/生成）。
  - **未扩大 ADR-0015 可信目录边界**：Src 路径/边界/拼写仍按包目录磁盘路径校验；overlay 目标字节本即构建实际消费输入，校验它是"让校验字节==嵌入字节"，非新增信任。未改 Codegen；未新增诊断码；无生产测试退化开关。
- **修复后**：坏 overlay → `--check` **rc2** + `CODEGEN-ASSET-002: image: unknown format` + 不写产物；合法替换 → 接受；overlay 删除 → 拒绝。

## F-A2 overlay 回归曾是假绿 → 重写为真判别
旧 `TestR25OverlayCannotSubstituteAssetBytes` 实际把未引用的 `b.png` 映射自身、坏的是 `a.png`、JSON 手拼反斜杠、GOFLAGS 未整项引号 ⇒ 非零来自 flags/JSON 解析而非门禁（协调者已证 `invalid character U in string escape code`，缺 CODEGEN-ASSET-002）。
**重写**（`asset_overlay_batch_r25_test.go`）：用 `encoding/json` 生成 overlay、整 flag 加引号、映射**被引用**的 `a.png`；断言**精确** `CODEGEN-ASSET-002` 且 rc2 不写产物；并新增真实 Go embed-truth 助手（`asset_overlay_embedtruth_r25_test.go`，临时 module `//go:embed` + 同 overlay，go test 构造 decode）：坏替换 → panic `unknown format`，合法替换 → 不 panic，正控制成立。**旧 6 例保留绿 + 新 4 overlay 例绿**。

## F-A3 保旧/快照与口径补齐
- 冷/暖/`--check`、正常/坏替换/删除/恢复、out-dir、混批：均在同环境核**产物字节 + mtime + Manifest + 成功缓存**保旧（见既有 6 例 + 新增 overlay 例；`--check` 坏替换不写产物）。
- **两条针对修复的可编译行为变异**（独立源码副本 + Go `-overlay`）：
  - MUT-A 关闭 overlay 有效字节读取（`assets.go` 门禁只读磁盘）→ `TestR25OverlayBadBytesRejected` **FAIL**。
  - MUT-B 指纹忽略 overlay（`build_inputs.go` `ReadAssetForFingerprint(...,nil)`）→ `TestR25OverlayEffectiveBytesEnterCacheKey` **FAIL**。
  - 恢复（无 overlay）→ 全 PASS。**新增读取/查询成本口径**：仅当资产 abs 命中用户 overlay 时**多开/读一次替换文件**（Stat+ReadAll，受 MaxAssetBytes 约束），无 overlay 时零额外读取；无新增 go list/go 查询。
- **撤销**旧 fact-matrix/源码注释/补正建议/dev-log 的"Go overlay 不改 embed"结论；补正建议追加 repair 段（旧材料不改）。硬链接/TOCTOU 未证边界保留。

## 门禁（Windows/amd64，CGO=0）
`internal/cli`（R25+asset/cache/overlay/batch combo）ok；`internal/compiler/pipeline` ok；vet rc0；gofmt 空；diagdoc ok；docgate ok；覆盖 cli **86.3%** / pipeline **77.7%**。无窗口/无采样/无安装/无新依赖。无窗口构造验证仅自有 Go 测试，不启动用户应用。

## 未证边界
Linux/macOS 未跑；无真实性能采样；硬链接原始位置/TOCTOU/解码器漏洞/解压炸弹沙箱/未支持格式 未覆盖；overlay 目标若指向包外文件属构建方显式输入（其路径安全由构建方负责，本修复只保证"校验字节==嵌入字节"）。合并/发布由协调者。

## 五态
实现 done(`531be90`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
