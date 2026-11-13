# TASK-LIFE-001：实现组件生命周期与静态资产管线

## 元信息
- 状态：R28-C opt-in Owner/Router 页面生命周期已批准，Windows 单窗两种原生关窗场景已验证。整卡**仍为部分完成**（跨平台、多窗口和通用资产范围待完成） | 优先级：P1 | 预计：4h
  - 2026-11-11 本线补记：原状态里的「静态资源管线待完成」措辞不精确。`TASK-LIFE-001-3` 的**静态资源发现 / 嵌入声明 / 生成代码引用**三件事，在 [ADR-0015](../../03-架构/adr/ADR-0015-Image控件与编译期资产嵌入.md) 已批准、**Image v1 已发布**的合同内（静态 `Src`、PNG/JPEG、包内相对路径、`//go:embed`）已经实现并有回归；逐条映射见 §2.2。这不等于整卡完成，也不等于通用资产管线已获批准，见 §2.3。
- 分支：feature/TASK-LIFE-001
- 依赖：TASK-ROUTER-001

## 1. 目标
为页面和组件提供 Context 驱动的挂载/卸载生命周期，并将静态资源编译期嵌入生成应用。

## 1.1 规范依据
- 生命周期线程所有权和异步取消遵守 `docs/03-架构/运行时线程模型.md`。
- 状态提交和缓存边界遵守 `docs/03-架构/AOT状态Diff语义.md`。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-LIFE-001-1 | 实现挂载回调启动及 Context 取消传播 | go test -v -race ./pkg/runtime/lifecycle/... |
| TASK-LIFE-001-2 | 实现卸载清理并保证重复挂载/卸载行为确定 | go test -v -race ./pkg/runtime/lifecycle/... |
| TASK-LIFE-001-3 | 实现静态资源发现、嵌入声明和生成代码引用 | go test -v ./internal/compiler/codegen/... |

## 2.1 R28-C Lifecycle Owner/Router 验收边界

- 已批准范围为 opt-in Owner/Router 页面生命周期，不改变默认关闭策略。
- 通过组合验证的时序为：成功 publish 当前页后，先取消旧页，再对旧页执行一次 `OnUnmount`，随后在首个业务 `Layout` 前挂载待定新页。
- 失败、合并导航与 close-winning 路径不挂载页面；后台 `Router.Shutdown` 仅执行 `CancelOnly`。
- `Owner.CancelOnly()` 只取消 owner 根。若页面 Context 使用 independent parent，由 Router 的 `Shutdown`/`Finalize` 负责该页面的取消与卸载；`Finalize` 不隐式调用 `Shutdown`。
- TASK-LIFE-001-1/2 的生命周期与 Router 组合门禁已在 R28-C 基线验证。
- **TASK-LIFE-001-3 据实修正（2026-11-11 本线补记）**：子任务原文「实现静态资源发现、嵌入声明和生成代码引用」在 **ADR-0015 / Image v1 合同内**已实现、已发布、有随仓回归与真实生成/构建/负例保旧证据（§2.2）。原表的 `Todo` 与「静态资源管线待完成」不再准确，故改为「v1 合同内已覆盖；合同外范围单列裁决」。**不宣布整卡完成**。
- `natural` 和 `shutdown-first` 两种 Windows 单窗场景均已通过真实标题栏关闭验证。两次 race 进程实际退出码为 0，未报告 DATA RACE，页面各卸载一次。实际运行提交为 `19c86a1`，证据沿用至 Lifecycle 构建输入相同的代码候选 `35b61b5`。运行身份、判据与未验范围见 [ADR-0018](../../03-架构/adr/ADR-0018-Lifecycle-Owner与Router页面生命周期.md#4-app-destroyevent-最小接线)。

## 2.2 TASK-LIFE-001-3 独立对账（2026-11-11，HY4 / R28-C 后续收口）

对账范围只读：**不修改生产 Compiler / CLI / 缓存 / 规范**，不复制资产管线实现。
完整矩阵与原始证据在
`D:/Codes/Goxml-agent-rounds/2026-11-11/product-r28-release-followup/delivery-c/image-mapping.md`
与同目录 `raw/image/life-asset-link-report-r2.json`。

| 子任务三段 | ADR-0015 / TASK-IMAGE-001 依据 | 现合同内实现与回归（逐条指针） | 本轮真实复跑 |
|---|---|---|---|
| 静态资源发现 | ADR-0015 §2.3「路径收集只遍历展开后的页面；Pipeline 在 Analyzer 加载 Go 类型之前读取并验证资产」；TASK-IMAGE-001-3 | `internal/compiler/codegen/assets.go` 的 `AssetRefs` / `AssetRefsFromDoc`（含 Def/Slot 展开、条件与循环）；`internal/compiler/pipeline/assets.go` 的 `validateAssets` / `validateAssetRefs`；回归 `TestAssetRefsFindsImagesFromDefExpansion`、`TestAssetRefsWalksConditionalsAndLoops` | 连接回归 `discovery.source_static_image_src`：两页 `.gxml` 的静态 `Src` 与同包 PNG 真实存在；窄测 codegen 5 PASS / pipeline 13 PASS |
| 嵌入声明 | ADR-0015 §2.2（只写路径，不 Base64）、TASK-IMAGE-001-2 | `internal/compiler/codegen/codegen.go` 发射 `//go:embed "<path>"` 与 `var goxmlAssets<Source> embed.FS`；`TestImageCodegenEmbedsAsset`、`TestImageDuplicateSrcSharesField`；命名预检 `NamingTarget.HasAssets`（TASK-IMAGE-001-4） | 连接回归 `embed.home_go_embed_directive` / `embed.detail_go_embed_directive` / `embed.embed_fs_var` / `generated.no_base64_inline` 全通过 |
| 生成代码引用 | ADR-0015 §2.4（`img<n> paint.ImageOp`、构造期 `ReadFile`+`image.Decode`、`Layout` 只回放）、TASK-IMAGE-001-5/7 | `TestImageCodegenEmbedsAsset` 断言 `ReadFile` / `image.Decode` / `paint.NewImageOp` / `widget.Image{Src: v.img0,…}`；`TestGeneratedImageCompilesAndLaysOut`、`TestGeneratedImageDrawsPixels`；routed-gallery `route_test.go` 断言 `img0.Size()` 等于 PNG 像素尺寸 | 连接回归 `generated.ref_readfile_decode` / `generated.ref_imageop_field_and_layout` / `build.generated_project`（`CGO_ENABLED=0 go build ./...` rc=0）全通过 |
| 资产门禁 / 负例保旧 | ADR-0015 §2.3 `CODEGEN-ASSET-001..003`；TASK-IMAGE-001 §4 已推翻的旧「缓存不重验」条目；routed-gallery C6/C7 与 `scripts/verify-gallery-accept.py` | `TestRunImageAssetGate`、`TestRunImageAssetRejectsTraversal`、CLI `TestGenerateImage*`；`TestAssetSymbolInNamingPrecheck`；diagdoc 守护诊断码 | 连接回归：损坏资产 → rc=2 + `CODEGEN-ASSET-002` + 「批次全部放弃」，两页 `*_gen.go`、`generate-manifest.json`、`.goxml/cache/*.json` 的字节与 mtime 全部保旧；还原后重新生成 rc=0。窄测 CLI 6 PASS / diagdoc 24 PASS |

**连接证据**：既有判据分散在 codegen 单测、pipeline 门禁、CLI 缓存矩阵与图库离屏测试里，
没有一条在**同一个真实工程**上把「发现 → embed 声明 → 生成代码引用 → 真实构建 →
负例保旧」串起来。本线补一条**现合同内**回归 fixture
（`delivery-c/raw/image/verify_life_asset_link.py`，只用公开 `goxml generate` 与随仓
routed-gallery 模板），实测 **18/18 判据通过**；首轮把被故意改坏的**输入资产**误算进
「产物保旧」快照导致 1 条判据失败，已按事实收窄到产物/清单范围后重跑转绿，首败原件保留。

## 2.3 现合同之外（**单列裁决，不擅自实现**）

ADR-0015 §4 明确限制：v1 只支持静态 `Src`/`Fit`、PNG/JPEG、包内相对路径、
1 MiB / 4M 像素预算；不做绑定 Src/Fit、平铺、SVG、动图；首版不做 pipeline
复制资产到输出目录 / manifest 语义。若原卡 `TASK-LIFE-001-3` 的「静态资源管线」
意指下列任一范围，均**不在**当前已批准合同内，需负责人单独裁决：

1. 通用资产 API（跨控件/跨资源类型的统一注册、查询、运行期解析）；
2. 额外格式（SVG、动图、字体子集以外的图标格式）或额外资源类型；
3. 跨包/跨输出目录的资产复制、产物 manifest 与分发语义；
4. 绑定 `Src`/`Fit`、运行期资源加载或缓存策略变更。

裁决前**不得**改动生产编译器、CLI 缓存、语言规范或输出契约；本线也没有这么做。
Image v1 已覆盖 §2.2 的三段实现，Windows 单窗关窗已按 §2.1 验证。跨平台和多窗口验收，以及 §2.3 的范围裁决仍待完成，整卡保持部分完成。
