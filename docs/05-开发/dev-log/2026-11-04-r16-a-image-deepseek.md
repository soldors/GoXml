# 2026-11-04 R16-A IMAGE-RELEASE-CANDIDATE（DeepSeek）dev-log

基线 `b26155115ee76b1415ae36870fa18b252cb7a5d5`；树 `product-r16-deepseek`，分支 `codex/product-r16-deepseek-20261104`。
范围：Image 相关 CLI/编译器/诊断与直接回归 + 本线 dev-log。主仓 WIP 只读，按精确路径/blob 移植。

## A1 识别并恢复候选（commit `1630558`）

### 来源（主仓未提交 WIP，按 blob 记录）
新增（untracked）：
| 路径 | bytes | blob |
|---|---|---|
| internal/compiler/codegen/image.go | 4817 | 76a8ac44745e33b9d62ced120cc399b30805c4a6 |
| internal/compiler/codegen/assets.go | 7748 | 58c08e78d1ab17350f8bd5035677831a8ca92bc2 |
| internal/compiler/codegen/image_codegen_test.go | 13913 | cadd921a6ed3db857ae6c8934157c6f924cd24c8 |
| internal/compiler/codegen/naming_assets_test.go | 690 | 919d9d1ce8a1c2b50aa255da76a18f5d038ad015 |
| internal/compiler/pipeline/assets.go | 8679 | 1a11ccbebd669a9638313b57a5f6875dd00ba919 |
| internal/compiler/pipeline/assets_path_windows_test.go | 5225 | afc22e64c2255715c5a6e112a33abb588d8de0ee |
| internal/compiler/pipeline/assets_test.go | 9204 | 14cdf8dc508e05bad3025a5125937a29d0a60d3c |
| internal/cli/image_test.go | 5100 | 01b883e2e2c0c8fced5e3bf64ae199ebff767ae8 |
| examples/gxml/valid/image.gxml | 442 | d303679cbf923ddd8663ea814aac3160fb7cef11 |

修改（主仓 ` M`，逐 hunk 复核**全部为 Image 相关**后整体移植）：
- `contracts/ir_schema.go`：新增 `CodePrefixCodegenAsset`。
- `internal/compiler/expander/schema.go`：Image 增 `Fit` 属性。
- `internal/compiler/codegen/codes.go`：`CodeAssetPath/Load/Limit`（CODEGEN-ASSET-001/2/3）。
- `internal/compiler/codegen/naming.go`：`HasAssets` + `assetSymbol`（goxmlAssets<Source>）。
- `internal/compiler/pipeline/pipeline.go`：`Config.TargetDir`、`Prepared.AssetCount`、`validateAssets` 前置门禁、Codegen 资产错误上抛。
- `internal/compiler/codegen/codegen.go`：Image 节点/embed 变量/blank 导入(png,jpeg)/解码回放。
- `internal/cli/pipeline.go`：向 pipeline 传 `TargetDir` 与 `HasAssets`。
- 测试伴生：`codegen_test.go`（<Image> 从"未支持"改"已支持"，改用 <Expanded> 作未支持样本）、`naming_diag_test.go`（按 CodeNaming*/CodeAsset* 分组校验前缀）、`binding_runtime_test.go`/`cache_runtime_test.go`（新增 `rtProbeAssets`/`genModuleWithProbeAssets` 支持 embed 资产）。

### 组合验证（与 R14 导入别名消歧、R15 缓存 v8）
- 既有：`<For><Image/></For>`（TestGeneratedImageInsideForWithItemUsed）、`<If>` 内 Image、多图、含空格路径、绑定页。
- 新增（本线 `internal/cli/image_test.go`）：
  - `TestGenerateImageSamePackageMultiPage`：同包多页各含 Image，view/asset 变量按源文件唯一，两页同时生成不冲突。
  - `TestGenerateImageWithImportConflict`：目标包已有与生成代码默认导入同名的包级标识符（image/layout/paint），R14 别名消歧仍成立。
- 结论：`go test ./internal/compiler/codegen ./internal/compiler/pipeline ./internal/cli ./contracts ./internal/compiler/expander` 全绿。

### 来源无法确认的差异
无（9 个新增 + 11 个修改均逐 hunk 确认为 Image 相关；未引入其它 WIP）。

## A2 资产门禁真实复验（raw/A2-asset-gate.json，全 PASS；**门禁本身无需改代码**）

真实 CLI（合法 PNG/JPEG、大小写不匹配、相对路径越界、symlink、缺失、损坏、超字节/超像素预算）：

| 用例 | 结果 |
|---|---|
| 合法 PNG / JPEG | rc0，生成成功 |
| 大小写不匹配（Logo.PNG vs logo.png） | rc2 `CODEGEN-ASSET-001 ... use "a/Logo.PNG"` |
| 相对路径越界 `../outside.png` | rc2 `CODEGEN-ASSET-001 ... must not contain '..'` |
| symlink 组件（a/link.png -> 外部） | rc2 `CODEGEN-ASSET-001 ... is a symbolic link` |
| 缺失 | rc2（ASSET 诊断） |
| 损坏（截断 PNG） | rc2 `CODEGEN-ASSET-002 ... unexpected EOF` |
| 超 1 MiB | rc2 `CODEGEN-ASSET-003 ... limit is 1048576` |
| 超 4M 像素（3000×3000） | rc2 `CODEGEN-ASSET-003 ... limit is 4194304` |

预算常量：`MaxAssetBytes = 1<<20`、`MaxAssetPixels = 4<<20`。
平台分级：symlink/junction（`assetBoundaryProblem`）；**硬链接**同一数据两名字仍属包内时 Lstat 看不出差别（已在源码注释登记为未覆盖边界）；并发替换（TOCTOU）未证。这些按"推断/未证"记录，未改契约。

## A3 缓存与构建闭环（修复 + 证据 raw/A3-cache-asset-closure.json，全 PASS）

### 历史反例（缺口）
资产被删除后二次 `generate` 仍报 `跳过 home_gen.go（缓存命中）` → 旧产物沿用、资产门禁不执行。
根因：缓存身份 K3（目标包 .go 源）与 K11（overlay/modfile 内容）都**不含 `<Image Src>` 资产文件**。

### 修复（本 commit）
- `internal/cli/build_inputs.go`：新增 `assetRefsFromSource`（parser→expander→遍历 Page 取静态 Src）与
  `assetsFingerprint`（按目标目录读资产内容 SHA-256；缺失记稳定标记）与 `onDiskSpelling`（Windows 大小写不敏感下，
  把磁盘**实际拼写**纳入键，使"改错大小写"也安全 miss）。
- `internal/cli/cache_wire.go`：将 `assetsFingerprint` 并入 `BuildInputs`（K11 分量）。无静态资产 ⇒ 空串，不影响普通工程。
- **不改变缓存格式版本**（K11 语义扩展，键结构不变；无记录字段变化）。

### 真实 CLI 闭环（预热→变更→拒绝保旧→恢复→构建）
| 步骤 | 结果 |
|---|---|
| 预热生成 | rc0，产物+Manifest+cache |
| 缓存命中态下**删除资产** | **安全 miss（不再报缓存命中）**→ 拒绝；旧 gen 字节/mtime、Manifest 全保留 |
| 恢复资产（内容不同，Go 源等价） | **安全 miss** → 重新生成成功（区分"内容变化但源码等价"与"非法资产"） |
| 改坏资产（截断） | 拒绝，旧 gen/Manifest 保留 |
| 改错大小写（磁盘 Logo.PNG） | 拒绝（spelling 入键 => miss => ASSET-001），旧产物保留 |
| 恢复后 | generate rc0 且 `go build ./...` rc0（真实构建） |
| 批次坏一页（other.gxml 缺资产） | **整批拒绝**：home_gen.go / other_gen.go / Manifest 均未创建 |

### 判别回归 + 变异红
`internal/cli/asset_identity_r16_test.go`：DeleteInvalidates / ContentChangeStillRegenerates /
SpellingChangeInvalidates / AssetFingerprintUnit 全 PASS。
**变异红**：禁用 K12（`assetsFingerprint` 不并入键）后，三个用例立即失败并复现历史反例
`跳过 ui/home_gen.go（缓存命中）`；恢复后全绿。

### 生成器字节规则
本修复**不改变生成字节**（仅缓存身份），无需提升 `generatorOutputRevision`。若后续 Image 生成模板改动，需由协调者提升该修订（见 R12-A 机制）。

## A4 离屏验收与接纳清单
见 `handoff.md` A4 节与 `raw/A4-pixels.json`（代表性尺寸/Fit 的有效像素判据）。

## 门禁
`go test ./internal/cli -cover` = 86.2%；`codegen` = 88.8%；`pipeline` = 81.3%（**基线本就低于 85%**，非本线回归；
新增 `assets.go` 函数覆盖 67–100%）。`go build ./...` ok；`go vet` rc0；`gofmt` 空。

## 五态
实现 done / 自测 pass / 外审 PENDING（C 线未冻结）/ 主仓未合 / 远端未推送。
