# ADR-0015：`<Image>` 控件与编译期资产嵌入

- 状态：**已批准**（2026-11-04，隔离分支验收）
- 日期：2026-10-31
- 关联：[`GXML语言规范.md`](../GXML语言规范.md) §3.2.2、
  [`代码生成输出契约.md`](../代码生成输出契约.md) §4.8、
  [`诊断码目录.md`](../诊断码目录.md) §2.9、
  `TASK-IMAGE-001`

## 1. 背景

`<Image>` 从 v1 起就在语言规范的基础控件表中，Schema 也注册了 `Src`
（`internal/compiler/expander/schema.go:166`），Parser 将它列为内置控件
（`internal/compiler/parser/source.go:40`）。但 Codegen 的标签分派长期没有
`Image` 分支，写 `<Image>` 会在生成阶段报 `not supported`。

这不是「再加一个控件」那么简单：它第一次把**外部文件**带进编译输入。
项目同时有两条硬约束：

1. **运行时不读文件/不解析资源**（`AGENT.md` §2.2 的精神）；
2. **Codegen 不读磁盘**、只返回 `[]byte`，写盘归 pipeline
   （`docs/03-架构/代码生成输出契约.md` §4.4、`AGENT.md` §3）。

因此必须先裁决：资产在哪个阶段读取、以什么形式进入生成代码、缺失/越界/
损坏在哪一阶段报错、CLI 缓存是否要感知资产内容。

## 2. 决策

### 2.1 静态 `Src` + 包内相对路径

- v1 只接受静态字面量 `Src`；绑定 `Src` 拒绝（`CODEGEN-ASSET-001`）。
- 路径基准是**生成文件所在的 Go 包目录**，不是 `.gxml` 源文件目录。
  理由：`//go:embed` 的匹配范围由生成文件所在包决定；`--out-dir` 下两者不同，
  以生成文件为准才能保证 embed 指令成立。
- 拒绝绝对路径、卷名/UNC、`..`、隐藏/下划线段与控制字符。读取之前检查资产
  路径的组件与实际拼写，拒绝识别到的符号链接、junction 和其他重解析点。
- 资产目录须由可信构建方控制。普通文件检查可接受硬链接，无法证明其原始位置。
  并发替换检查与读取之间的路径或文件未验证，构建期间须保持资产树稳定。

### 2.2 用 `//go:embed`，不把字节 Base64 进源码

生成文件只写：

```go
//go:embed "assets/logo.png"
var goxmlAssetsDashboard embed.FS
```

构造期 `ReadFile` + `image.Decode` 一次，缓存 `paint.ImageOp`；`Layout` 只回放。

选择 embed 的理由：

- 生成文件只含路径。CLI 缓存身份仍记录资产内容和实际拼写，保证改动、缺失
  和不安全路径触发重验。Go 构建按 embed 文件内容重新编译；
- 源码体积不随资产膨胀；
- `embed.FS.ReadFile` 读的是编译期数据，不违反「运行时不读文件」。

代价：`--out-dir` 使用者必须把资产放进输出包目录；这是路径语义的直接结果，
不是隐藏行为，写入规范与诊断消息。

### 2.3 资产门禁在 pipeline，Codegen 不读磁盘

- 路径收集只遍历展开后的页面。Pipeline 在 Analyzer 加载 Go 类型之前读取并验证
  资产。支持 PNG/JPEG，编码文件至多 1 MiB，解码像素至多 4,194,304。
- 同时存在资产错误与 Go 加载错误时先报告资产错误。资产正常时，独立的 Go
  加载错误保留原诊断和底层原因。
- 失败发射 `CODEGEN-ASSET-001..003`，不进入生成、不写文件；
- Codegen 只对路径形状做防御性校验，保证绕过 pipeline 的直接调用也不会生成
  非法 embed 指令。

预算取 1 MiB / 4M 像素：首版以图标与中小图片为目标，避免 embed 进二进制的
资产无界增长；大图应走后续资产管线版本，而不是在本 ADR 里放开口子。

### 2.4 生成形态

- 静态页构造函数签名保持不变（`NewXxxView() *View`）。资产字节已由 pipeline
  完整 `image.Decode` 校验（先 `DecodeConfig` 查尺寸，再完整解码查损坏），
  正常链路解码失败不可达；静态构造期 panic 是「生成文件被篡改/损坏」的
  不变式护栏，不是用户输入错误路径。
- 绑定页构造函数仍返回 `(*View, error)`，解码失败走 error。
- 同一 `Src` 只嵌入/解码一次，多个节点共享 `img<n>` 字段。
- 绘制位置固定居中；`Fit` 由可选静态属性决定：缺省 `Contain`（等比缩入不裁剪），
  另支持 `Cover`（等比覆盖并裁剪）与 `Unscaled`（不缩放），映射到 Gio
  `widget.Fit` 的对应常量。绑定 `Fit` 与非枚举值在生成期拒绝。

### 2.5 命名预检

新增包级符号 `goxmlAssets<Source>`。`goxml generate` 的预检在页面含静态 Image
时把它计入（`NamingTarget.HasAssets`），否则同名用户声明会拖到 `go build`
才以 `redeclared` 暴露。

## 3. 备选方案与放弃理由

| 方案 | 结论 | 理由 |
|---|---|---|
| Base64/字节字面量内联 | 放弃 | 生成文件随资产膨胀；资产变化改变源码但不改变语义，且会逼迫缓存键感知资产 |
| 运行时读取文件路径 | 拒绝 | 违反「运行时不读文件」；打包与工作目录问题不可控 |
| pipeline 复制资产到输出目录 + embed | 首版不做 | 需要新增产物/manifest 语义，与 CLI 清理解析在制线重叠；当前路径语义已可用 |
| 编译期完整解码并生成像素数据 | 放弃 | 实现复杂、体积更大；`image.Decode` 一次的成本可接受 |

## 4. 后果

- 正向：框架首次具备图片/图标能力；资产生命周期与二进制一致；
  Windows 仍可 `CGO_ENABLED=0`（`image/png`、`image/jpeg` 为纯 Go）。
- 负向/限制：
  - 资产必须位于生成包目录（或 `--out-dir` 的输出包目录）；
  - 首版不支持绑定 Src/Fit、平铺、SVG、动图；`Fit` 仅 Contain/Cover/Unscaled；
  - 编译期会完整验证解码（受 1 MiB / 4M 像素预算限制），
    生成时间随资产数量/像素增长；未做大规模资产基准；
  - 静态构造期解码会同步计入启动耗时；1 MiB / 4M 像素预算限制其上限，
    但本轮未做启动性能测量；
  - 静态构造期解码失败会 panic；
  - CLI 命中前检查路径并读取资产用于指纹。资产内容、拼写或可读性变化会触发
    重验；校验失败时保留已有产物、Manifest 和成功缓存记录。
- 诊断：新增 `CODEGEN-ASSET-001..003`，由 `diagdoc` 门禁守护。

## 5. 证据

- 规范已有 Image/Src：`GXML语言规范.md` §3.2、§5.3；
- Schema 已注册：`internal/compiler/expander/schema.go:166`；
- Parser 内置控件表：`internal/compiler/parser/source.go:40`；
- Codegen 原缺口：历史 `nodeExpr` 分派无 `Image`，默认报 `not supported`
  （本次实现后新增 `imageExpr`）；
- Gio API：`gioui.org@v0.10.3/widget/image.go` 的 `Image{Fit, Position}`、
  `paint.NewImageOp`；
- 运行时验证：
  - `TestGeneratedImageCompilesAndLaysOut`：临时模块真实 PNG 编译 + 布局；
  - `TestGeneratedImageDrawsPixels`：headless 截图确认实际绘制非零像素；
  - `TestGeneratedBindingPageWithImage` / `TestGeneratedImageInsideIf`：
    绑定页与条件分支；
  - `TestImageFitMapping` / `TestImageFitRejectsInvalidAndBinding`：
    Contain/Cover/Unscaled 映射与拒绝路径；
- 资产门禁：`TestRunImageAssetGate`（缺失/损坏/超限）、
  `TestRunImageAssetRejectsTraversal`、CLI `TestGenerateImage*`；
- 资产路径/预算/完整解码实现见 `internal/compiler/codegen/assets.go` 与
  `internal/compiler/pipeline/assets.go`。
