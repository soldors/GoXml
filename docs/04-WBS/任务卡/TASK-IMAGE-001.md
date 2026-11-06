# TASK-IMAGE-001：`<Image>` 静态资产与编译期嵌入

## 2026-11-06 发布状态补记

R25-A 修复校验字节与 Go overlay 实际嵌入字节分叉，祖先删除/本批产物冲突写盘前拒绝；历史 Manifest 需保护头和 Source 证明。作者 R2 加协调者补正发布于 `76f0669`，已纳入 `96a7067`；可信目录和未证安全边界不扩大。

现行证据与边界见 [R25 总览](../2026-11-06-R25-项目状态总览.md)。下面原阶段/验收记录按日期保留。


## 元信息
- 状态（三段式，**不得合并表述**，2026-11-05 R23-C 重核）：
  - **实现：已完成并合入本地 master**（R20 `4e413f0` 及之后各轮）；
  - **验收：隔离分支验收通过**（R18 `technicalAcceptance=pass`、R20 `acceptance=pass`），限 Windows / headless / 可信本地构建目录；
  - **发布：已推送**（`4e413f0` 是远端 `a8a40e3` 的祖先）。
- 优先级：P1 | 预计：4h | 来源：主库未跟踪草稿（2026-10-31），本卡为**逐 hunk 比对后的可审查版本**，
  主库原草稿保持不动；原文「待项目负责人验收」是当时的历史状态，不删除、按日期注明。
- 依赖：TASK-CODEGEN-001、TASK-SEC-001
- 契约：`docs/03-架构/GXML语言规范.md` §3.2.2、`docs/03-架构/代码生成输出契约.md` §4.8、
  `docs/03-架构/adr/ADR-0015-Image控件与编译期资产嵌入.md`（状态：**已批准**，2026-11-04）

证据链（按轮次，均为版本化 dev-log 或轮次 state）：R16/R17 候选实现与诊断归因 → R18 批准 ADR-0015 与静态 Src/Fit、诊断码并给出 pipeline 覆盖 86.3% → R20 `d7acf3d` 合入 Image、发布组合 `4e413f0` → R21-C `563f536` 增加 [routed-gallery 教程工程](../../../examples/gxml/testprojects/routed-gallery/README.md) → R22-C 把缓存/资产/变异验收变成 [随仓入口](../../../scripts/verify-gallery-accept.py)。

## 1. 目标

让 `<Image Src="..."/>` 在编译期把静态资产嵌入生成程序，运行时只解码一次并绘制；
不引入运行期文件 I/O，不破坏 Windows `CGO_ENABLED=0`。

## 2. Subtask 与验收

| 子任务 | 内容 | 验收命令 / 证据 |
|---|---|---|
| TASK-IMAGE-001-1 | 契约：Src 静态/包内相对/格式/预算/embed 语义 | 规范 §3.2.2、输出契约 §4.8、ADR-0015 |
| TASK-IMAGE-001-2 | Codegen：`//go:embed` + `embed.FS` + `img<n> paint.ImageOp` | `go test ./internal/compiler/codegen/` |
| TASK-IMAGE-001-3 | 管线门禁：缺失/越界/格式/预算 → `CODEGEN-ASSET-001..003` | `go test -run TestRunImage ./internal/compiler/pipeline/` |
| TASK-IMAGE-001-4 | 命名预检：`goxmlAssets<Source>` 计入 `HasAssets` | `go test -run TestAssetSymbolInNamingPrecheck ./internal/compiler/codegen/` |
| TASK-IMAGE-001-5 | 运行时：临时模块真实 PNG 编译 + 布局 | `go test -run TestGeneratedImageCompilesAndLaysOut ./internal/compiler/codegen/` |
| TASK-IMAGE-001-6 | 诊断码登记与文档同步 | `go test ./internal/compiler/diagdoc/` |
| TASK-IMAGE-001-7 | `Fit` 静态属性：Contain/Cover/Unscaled，默认 Contain；绑定/非法值拒绝 | `go test -run TestImageFit ./internal/compiler/codegen/` |

## 3. 作者自检结果（2026-10-31，原文保留）

> 下列结果是作者当时在隔离候选上的自检，**不等于**项目负责人验收。接纳与发布见 §5。

| 检查 | 结果 |
|---|---|
| `gofmt -l .` | 0 个文件 |
| `CGO_ENABLED=0 go build ./...` | rc=0 |
| `CGO_ENABLED=0 go vet ./...` | rc=0 |
| `CGO_ENABLED=0 go test ./... -count=1` | 全部包 ok，rc=0 |
| headless 像素绘制 | `TestGeneratedImageDrawsPixels` 通过 |
| Fit | `TestImageFitMapping`、`TestImageFitRejectsInvalidAndBinding` 通过 |
| 资产门禁 | 缺失/损坏/越界/超限均有负例；`CODEGEN-ASSET-001..003` |
| CLI | 同目录、`--out-dir`、命名冲突、缺失资产 |

**边界**：以上为 Windows/headless 自动化证据；未做真实窗口目视、macOS/Linux 实机。
`-race` 当时因本机无 C 工具链未跑；R23-B 起本机已有 MinGW gcc（见 [R23-B 差距矩阵](../../../docs/03-架构/字体正式验收差距矩阵-R23B.md)），但**本卡未因此补跑 Image 的 race**，不得据工具链可用倒推已验证。

## 4. 明确边界

- v1 只支持静态 `Src`/`Fit`；绑定 Src/Fit、平铺、SVG、动图不做。
  `Fit` 仅 `Contain`（默认）/`Cover`/`Unscaled`，位置固定居中。
- 资产必须位于生成文件所在 Go 包目录；`--out-dir` 使用者需把资产放到输出包目录。
- 静态页构造期解码失败 panic（pipeline 已完整解码校验，属生成文件损坏护栏）；
  解码同步计入启动耗时，本轮未做启动性能测量。
- ~~CLI 缓存命中路径不重新校验资产存在性；资产缺失在 `go build` 暴露。~~ **该条已被 R16/R18/R22 的实现推翻**：资产文件与拼写进入缓存身份（K12），生成前用 `DecodeConfig` 与完整 `Decode` 校验；坏图或删图在**生成期**以 rc=2 + `CODEGEN-ASSET-002` 拒绝、批次全部放弃，两页产物与 Manifest 及成功缓存记录保旧（见 [图库教程的缓存矩阵](../../../examples/gxml/testprojects/routed-gallery/README.md)）。此处保留删改线，是为了让来源差异可审。
- 本轮未执行真实窗口目视验收、未跑 `-race`、未做 macOS/Linux 实机验证；
  不得据此宣称跨平台或视觉验收通过。

## 5. 2026-11-05 状态重核（R23-C，依据接纳记录）

| 维度 | 结论 | 依据 |
|---|---|---|
| 已实现 | `<Image>` v1：静态 `Src`、静态 `Fit`（Contain/Cover/Unscaled）、`//go:embed`、构造期解码缓存 `paint.ImageOp`、1 MiB / 4M 像素预算、诊断码 | 基线 `internal/compiler/codegen`、`internal/cli/pipeline.go`、[ADR-0015](../../../docs/03-架构/adr/ADR-0015-Image控件与编译期资产嵌入.md) |
| 已验收 | R18 隔离分支技术验收 pass（pipeline 覆盖 86.3%）；R20 `acceptance=pass` | [R18 日志](../../../docs/05-开发/dev-log/2026-11-04-r18-image-acceptance.md)、[R20 日志](../../../docs/05-开发/dev-log/2026-11-04-r20-router-production-close.md) |
| 已合入 | R20 `d7acf3d` 合入 Image；基线 `369703bb` 含全部实现 | `git merge-base --is-ancestor 4e413f0 369703bb` |
| 已推送 | `4e413f0` 是远端 `a8a40e3` 的祖先 | 本轮 `git ls-remote --heads origin master` 实测 + 祖先判定 |

**下游使用与随仓验收**：[routed-gallery](../../../examples/gxml/testprojects/routed-gallery/README.md)
的两页各嵌一张版本化 PNG，共用一份模型与一个 Router；`scripts/verify-gallery-accept.py`
把「真命中三不动 / 换图只重验首页 / 坏图删图 rc=2 且保旧 / 两条可编译变异」变成
可重复入口，R22-C 实测 76/76 条判据成立。

**仍未验证**（不冒称）：真实窗口目视、原生关窗事件、IME、真实 DPI、跨平台
（仅 Windows 实测）、并发替换资产、硬链接被读取之外的资产树稳定性。
R23-B 的字体正式验收、R23-A 的 Router 预算语义与本卡**互不构成证据**。

> 归并提示：本文件在独立分支是**新文件**，在主库是**未跟踪草稿** `??`。
> 协调者归并时必须三方比对，不能直接覆盖主库原件；见本轮交接包 `merge-plan.md`。
