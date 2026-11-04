# 2026-11-04 R17-A IMAGE-RELEASE-READINESS（DeepSeek）dev-log

父提交 `55423279463711f2a787263bf57713960da11bda`（R16 Image 复核候选：`3dae8ed` + `5816042` + `5542327`）。
树 `product-r17-deepseek`，分支 `codex/product-r17-deepseek`。主仓 `b261551`（63 WIP，未变，无差异登记）。
范围：Image 的 CLI/Compiler 资产路径与直接测试 + 本线 dev-log。Router/验收工具/公共索引不在本线。

## A1 诊断与归属（commit `811ec5e`，证据 raw/A1-diagnostics.json）

真实 CLI 对照（同一 CLI 修复前后）：

| 用例 | 修复前 | 修复后 |
|---|---|---|
| 首建缺失·绑定页 | CODEGEN-ASSET-002 | CODEGEN-ASSET-002 |
| 首建缺失·静态页 | CODEGEN-ASSET-002 | CODEGEN-ASSET-002 |
| **热缓存后删除·绑定页** | **GXML-ROOT-002**（embed `pattern a/logo.png: no matching files found`） | **CODEGEN-ASSET-002** |
| 热缓存后删除·静态页（对照） | CODEGEN-ASSET-002 | CODEGEN-ASSET-002 |
| 损坏（热缓存后） | CODEGEN-ASSET-002 unexpected EOF | 同 |
| 恢复 | rc0 | rc0 |
| 不相关 VM 编译错误 | GXML-ROOT-002（保留 `cannot use "bad"` 真实 cause） | **不变** |
| 不相关 Go 包错误 | GXML-ROOT-002（保留 `undefined: undefinedSymbol`） | **不变** |

**独立证实 ROOT-002 抢先条件**：仅当「**绑定页** + 目标包内旧生成文件含 `//go:embed` 指向已删除资产」时，Analyzer 加载 VM 包先因 Go embed 缺失失败并报 `GXML-ROOT-002`；静态页无 VM 加载，`validateAssets` 先跑 → 正确 ASSET-002。

**统一资产诊断建议**：应统一——但**只统一到「资产缺失/损坏」这一类**，不能把任意 Go 包加载错误改成资产诊断。

**最小修复（隔离候选）**：`frontEnd` 在 **Expander 之后、Analyzer 之前**新增 `validateAssetsDoc(er.Doc, cfg, file)`（基于 IR 的 `codegen.AssetRefsFromDoc`），资产问题先报 `CODEGEN-ASSET-*`；与资产无关的 VM/Go 加载错误资产校验会通过，仍由 Analyzer 原样报出并保留真实 cause。沿用候选现有语义（同上 001/002/003、同一预算与路径规则），不新增诊断号。
- 判别回归：`internal/compiler/pipeline/asset_diag_order_r17_test.go`（绑定页旧 embed 悬空→ASSET-002；无关 VM 错误→ROOT-002 且保留 cause）。
- **变异红**：移除 frontEnd 前置门禁后，绑定页用例立即变回 `GXML-ROOT-002`，测试红。

## A2 安全及组合（raw/A2-combinations.json、raw/A2-hardlink.json）

真实生成，重点补 R16 未证组合：

| 组合 | 结果 |
|---|---|
| 合法真命中（关键对照） | 命中，产物字节/mtime/Manifest 不变 |
| **同字节包外 junction**（Windows mklink /J，指向包外同字节文件） | 拒绝 `CODEGEN-ASSET-001 ... junction or reparse point`；旧 gen/mtime/Manifest 保留 |
| 大小写不匹配（磁盘 Logo.PNG） | 拒绝 `CODEGEN-ASSET-001`；旧产物保留 |
| 同路径替换为坏字节 | 拒绝 `CODEGEN-ASSET-002 unknown format`；旧 gen/Manifest 保留；修复后 rc0 恢复 |
| 有效+无效资产混批 | 整批拒绝；合法兄弟页 `home_gen.go`、`other_gen.go`、Manifest 均未创建 |
| 跨包输出（`--out-dir` 到另一包） | rc0 生成成功（资产按 TargetDir 解析） |
| 真正 `--check` | 通过 + 二次命中 |

**硬链接**（受控夹具取证）：包外同字节硬链接到包内资产**被接受**（rc0）——Lstat 无法区分（同 inode、名字在包内）。登记为**未覆盖边界**，不在本轮改契约。
**并发替换(TOCTOU)**：无法确定性复现，登记 **UNVERIFIED**。**跨平台**：仅 Windows 实测。

## A3 发布包（raw/A3-cache-identity.json、ADR-0015-decision-table.md）

- ADR-0015 逐项核对（Src/Fit/预算/诊断/测试）**全部一致**（见决策表 §1）。
- 文档候选差异（待批准后应用）：D1 资产校验时机（RunPrepared→前移 Analyzer 之前）；D2 删除「缓存命中不校验资产」负向条目（R16 K12 + R17-A 已解决）；D3 诊断码目录 §2.9 正式登记随批准。
- `generatorOutputRevision`：当前 `gen-2`；**字节规则变化才提升**（Image 模板/embed 形态/符号/Fit 映射），仅诊断归属/顺序变化（本 A1）不提升；记录结构变化才升 `cacheFormatVersion`（本线未改）。
- 验证：二次真命中（字节/mtime/Manifest 不变）；旧记录（version 7 vs 8）安全不命中。
- **受影响包门禁与 pipeline 覆盖差异**（如实）：pipeline 基线 80.4% → 当前 80.0%（两值均 <85%，基线既有缺口；A1 新增代码有专属测试覆盖；**未新增无关测试凑门槛**）；codegen 88.8%、cli 86.2%（≥85%）。

## 门禁
`go build ./...` ok；`go vet`（codegen/pipeline）rc0；`gofmt` 空；受影响包测试全绿；台账 `verify-evidence.py` PASS。

## 五态
实现 done(候选) / 自测 pass / 外审 PENDING（C 有新冻结 SHA 后独立复核）/ 主仓未合 / 远端未推送。
