# 路由图库工程模板（Image + Router）

一个可以**真的复制出去、真的生成、真的编译、真的跑测试**的最小工程：静态图片由
Goxml 在编译期 `//go:embed` 进二进制，两个包的页面在**同一个 Router** 上互相切换，
并且共用**同一份模型**——切页之后状态还在，关窗时 Router 与页面 Context 一起被取消。

它示范的是「Image 资产 + Router 导航 + 关窗接线」这套组合的接线方式，不是新的 GXML 语法：

```
                wire.Build(homeCtor, detailCtor)
                          │
        model.NewGallery()│  ← 只调用一次
                          ▼
   ┌──────────────────────────────────────────┐
   │  router.Router                           │
   │   "/gallery/home"   → home.NewGoxml…(m)  │──┐
   │   "/gallery/detail" → detail.NewGoxml…(m)│  │ 同一个 *model.Gallery
   └──────────────────────────────────────────┘  │
                          │                      │
        app.WithOnDestroy(r.View(), r.Shutdown)  │
                          ▼                      │
                 app.NewApp(...).Run()  ←────────┘
```

## 目录图

```
routed-gallery/
├── README.md                       本文件（不参与构建）
├── go.mod.txt                      → go.mod            模块定义（占位符 __MODULE__ / __GOXML_DIR__）
├── model/
│   └── gallery.go.txt              → model/gallery.go         唯一手写的业务类型 + 导航意图
├── wire/
│   └── wire.go.txt                 → wire/wire.go             组装：一份模型 / 一个 Router / 关窗包装
├── view/
│   ├── home/
│   │   ├── doc.go.txt              → view/home/doc.go         先有包，才能生成（见下）
│   │   ├── home.gxml               → view/home/home_gen.go    首页源（静态图 + 共享计数 + 跳转）
│   │   ├── assets/badge.png        24x24 PNG，跟产物同包（embed 的匹配范围由包决定）
│   │   └── route_test.go.txt       → view/home/route_test.go  跨页离屏测试（真实 Router + 真实组装）
│   └── detail/
│       ├── doc.go.txt              → view/detail/doc.go
│       ├── detail.gxml             → view/detail/detail_gen.go 详情页源（静态图 + 加计数 + 返回）
│       ├── assets/cover.png        32x16 PNG
│       └── detail_test.go.txt      → view/detail/detail_test.go 页内离屏测试
└── cmd/
    └── gallery/
        └── main.go.txt             → cmd/gallery/main.go      窗口入口（只构建，不运行）
```

`*_gen.go` 不在模板里——它们是**生成结果**。

为什么是 `*.go.txt` / `go.mod.txt`：如果它们叫 `.go` / `go.mod`，这些目录就会成为
Goxml 仓库根模块里的普通包，跟着一起被构建、被 vet。改名后它们只是**模板文本**。

## 一条命令：复制 + 填占位符 + 生成 + 构建 + 离屏测试

```bash
python scripts/verify-projects.py routed-gallery \
    --out-dir "D:/work/我的图库工程" \
    --lock-path "D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock" \
    --check
```

阶段与产物落点：

| 阶段 | 干什么 | 产物/证据落点 |
|---|---|---|
| `copy` | 复制模板、`*.go.txt`→`*.go`、替换占位符、补 `go.sum`（PNG 原样字节复制） | 工程目录；模板源码不改 |
| `build-cli` | `go build ./cmd/goxml` | `<工程>/.verify-projects/goxml.exe` |
| `generate:view` | 原生 CLI **一次目录生成**覆盖两页 | `view/home/home_gen.go`、`view/detail/detail_gen.go` |
| `build-entry` | **只构建**窗口入口 | `<工程>/.verify-projects/gallery.exe` |
| `test` | 离屏测试：10 个用例，零 SKIP、零 FAIL | 每阶段一个证据目录 |

`--out-dir` 必须不存在（本工具只写本轮独占创建的目录）；目录名可以含空格与全角括号。
每个阶段的真实 argv / cwd / 退出码 / 收口结论 / stdout·stderr 原始字节与 sha256 都在
`<工程>/.verify-projects/<阶段>/`，汇总在 `report.json`。

退出码：`0` 全过 · `2` 用法错误 · `3` 拒绝 · `4` 业务失败 · `5` 超时 ·
`6` 进程树收口未证实 · `7` 记录不完整 · `8` 锁忙 · `9` 锁未按自有 token 释放。

### 一条命令跑完整验收（缓存 / 资产 / 变异）

上面那条命令只证明「复制 → 生成 → 构建 → 离屏测试」能过。**验收**还要回答四个更难的问题
（再生成是不是真命中、换图会不会只重验一页、坏图会不会拒绝并保旧、两条变异是不是真的翻红），
这些判据现在都在仓库里，**不依赖任何交接目录**：

```bash
python scripts/verify-gallery-accept.py \
    --run-dir "D:/work/r22 图库 验收" \
    --lock-path "D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock"
```

`--run-dir` 必须是本轮独占创建的新目录（已存在就拒绝，零新增）。跑完它下面有：

| 落点 | 内容 |
|---|---|
| `project/` | 真实工程 + `.verify-projects/` 里每个阶段的 argv / rc / 收口 / 原始字节 |
| `main.json` | 主验收报告（`verify-projects.py` 原样落盘的那一份） |
| `raw/` | 矩阵每条命令的 `stdout.raw` / `stderr.raw` / `result.json` |
| `report.json` | 判据清单 + 命令台账 + 每一步前后的字节/mtime 快照（条数以报告 `counts` 为准；R22-C 实测 76 条） |

成功打印 `GALLERY-ACCEPT-OK <runId>`；失败逐条打印 `[判据 id] 名称: 详情`。
同一份判据的**低成本自检**（14 条反例，不跑 Go、秒级）在
`scripts/test-verify-gallery-accept.py`：重复目录、旧报告、锁忙、缺用例、SKIP/FAIL、
完成标记缺项、mtime-only 改动、变异锚点不唯一，各有一条。

三段持锁，**同一时刻只有一层**（没有嵌套）：

| 段 | 谁持锁 | 干什么 |
|---|---|---|
| 预检 | 自己取、立刻按自有 token 放 | 只为一件事：锁忙时**零新增**，连运行目录都不创建 |
| 主验收 | `verify-projects.py` 子进程 | 复制 → 一次目录 `generate --check` → 只构建入口 → 10 个离屏用例 |
| 矩阵 | 本进程自己 | 缓存真命中、换图只重验首页、坏图/删图拒绝保旧、两条变异 + 健康对照 |

为什么每条命令都固定 `GOFLAGS=-mod=readonly` 等环境：见下面「缓存与资产」的三条误判。
矩阵里唯一的例外是 C8——它**故意**改这一项，用来证明「上下文变化」长什么样。

### 手动步骤（排障时看）

```bash
# 0) 准备 CLI（只做一次）
go build -o /tmp/goxml ./cmd/goxml

# 1) 复制模板（目标目录名可以含空格与中文）
cp -r <repo>/examples/gxml/testprojects/routed-gallery "/d/work/我的图库工程"
cd "/d/work/我的图库工程"

# 2) 模板文本改名为真源文件（含 go.mod.txt）
find . -name '*.go.txt' -exec sh -c 'mv "$1" "${1%.txt}"' _ {} \;
mv go.mod.txt go.mod

# 3) 复制依赖校验文件：离线构建必需
cp <repo>/go.sum .

# 4) 填两个占位符
sed -i 's|__MODULE__|example.com/my-gallery|g' go.mod model/gallery.go wire/wire.go \
    view/home/route_test.go view/detail/detail_test.go cmd/gallery/main.go
sed -i 's|__GOXML_DIR__|D:/Codes/Goxml|g' go.mod

# 5) 一次目录生成两页（发现根=输出根=view）
/tmp/goxml generate view --out-dir view --check

# 6) 离线构建整个工程（只构建，不运行）
CGO_ENABLED=0 GOFLAGS=-mod=readonly go build ./...

# 7) 跑离屏测试（不需要窗口、不需要输入法）
CGO_ENABLED=0 GOFLAGS=-mod=readonly go test ./view/...
```

跑起来（**会创建原生窗口**，本仓库的自动测试从不执行这一步）：

```bash
go run ./cmd/gallery
```

## 各包职责

| 包 | 来源 | 职责 |
|---|---|---|
| `model` | 手写 | 唯一状态所有者：共享计数 `Visits`/`VisitsText`、导航意图 `OpenDetail`/`Back`/`Bump`、注入的导航函数 |
| `view/home` | 生成 | 静态图标 + 绑定 `{VisitsText}` + 「打开详情」 |
| `view/detail` | 生成 | 静态封面 + 绑定 `{VisitsText}` + 「记一次访问」/「返回首页」 |
| `wire` | 手写 | 组装：一份模型、一个 Router、两次注册、一次 `app.WithOnDestroy` |
| `cmd/gallery` | 手写 | 主题 + 窗口句柄 + 失效入口 + 首发导航 + 事件循环 |

两个 `.gxml` 都写 `ViewModel="model.Gallery"`（**相对模块根**的包目录 + 类型名），
所以生成产物的 `package` 子句是各自目录的包名，而类型定义留在 `model`。

## 路由：谁改状态，谁只发意图

- **模型不导入 `pkg/runtime/router`。** `OpenDetail()` / `Back()` 只记录意图并调用
  注入的导航函数（`wire.Build` 通过 `SetNavigator` 注入 `r.Navigate`）。
  于是模型可以被单测直接驱动，也不需要知道 Router 的线程模型。
- **`Navigate` 可从任意 goroutine 调用**，从事件处理器里调用是合法的；它只把路径记成
  **待定导航**，由 UI 线程在 Router 帧首的导航阶段应用。
- 页面点击发生在第 N 帧的子视图 Layout 内，已经过了本帧的导航阶段。
  `Router.Current()` 当帧仍是旧路径，第 N+1 帧才变成新路径。测试逐帧断言这两步
  （`TestRouteClickOpensDetail`）。
- 未注册路径是**同步**错误（`router.ErrPathNotRegistered`），不会进入待定队列。
- 页面构造失败时本次导航整体放弃、旧页继续运行、`NavError()` 记录原因；本模板的两个
  工厂会把生成构造函数的错误原样交给 Router，例如内嵌图片解码失败。

## 关窗接线：`app.WithOnDestroy(r.View(), r.Shutdown)`

`app` 的事件循环收到 `DestroyEvent` 时调用一次回调，然后返回。把 `r.Shutdown` 挂上去，
窗口关闭时 Router 与页面 Context 一起被取消，待处理队列被丢弃。

```go
view, err := app.WithOnDestroy(r.View(), r.Shutdown)   // wire.Build 里
```

- `WithOnDestroy` 对**已经包过**的视图返回错误（「destroy callback is already set」），
  这条错误本身就是一个可用的判据：生产组合再包一次必须失败，裸 `r.View()` 包一次必须成功。
  测试 `TestRouteDestroyWiringPresent` 就靠这一对正反例，**不用反射、不用认识内部类型**。
- 图库测试验证根视图已包装，并单独验证 `Shutdown` 可重复调用。
  回调目标 `r.Shutdown` 由上面的生产接线指定。图库入口在自动验收中只构建。

这条接线有**两层**证据，层次不同、不能互相替代，验收入口把它们分开记：

| 层 | 证明什么 | 谁在跑 | 判据在报告里 |
|---|---|---|---|
| 源码接线 | `wire.go` 里那条 `app.WithOnDestroy(r.View(), r.Shutdown)` 存在，且对已包装视图再包一次会被拒 | 图库的 `TestRouteDestroyWiringPresent`；变异 M1 把它拆掉后必须翻红；未变异工程上的同名用例必须绿（健康对照） | `m1-no-destroy-wrap.*` / `main.test.ledger` |
| 受控事件覆盖 | 两个事件循环（`app-loop`、`integration-loop`）收到 `DestroyEvent` 后**真的调用**了已注册的回调 | 仓库里的 `pkg/runtime/app` 的 `TestDestroyEventShutsDownWrappedRouter`，**不经图库** | `destroy.event.pass` / `destroy.event.loops` |

两层都没有覆盖的：原生窗口真的关掉时，`DestroyEvent` 从系统事件队列到达 Gio 的这条路径。
那属于人工/原生验收项，自动验收不声称验证过它。

## 生成过程的两条硬要求

1. **先有包，再生成页面。** 第一次生成之前目录里只有 `.gxml` 与 `assets/`，`go list`
   看不到非测试 Go 源文件，CLI 会以「没有可定名的非测试 Go 源文件」退出码 2 拒绝。
   `doc.go` 就是那个包。
2. **资产跟产物同包。** `//go:embed "assets/badge.png"` 的匹配范围由**生成文件所在的
   Go 包目录**决定，不是 `.gxml` 所在目录；`--out-dir` 与源目录不同时以产物为准。
   路径只接受包内相对路径：绝对路径、卷名/UNC、`..`、隐藏/下划线段与控制字符都被拒绝。

## Image：已批准的属性与预算

| 项 | v1 规则 |
|---|---|
| `Src` | **静态字面量**；绑定被拒绝（`CODEGEN-ASSET-001`），因为运行时解析资源违反「运行时不读文件」 |
| `Fit` | `Contain`（缺省）/ `Cover` / `Unscaled`，映射到 Gio `widget.Fit` 常量；绑定与非法值在生成期拒绝 |
| 绘制位置 | 固定居中 |
| 体积预算 | 编码文件 ≤ 1 MiB，解码像素 ≤ 4,194,304 |
| 格式 | PNG / JPEG（`image/png`、`image/jpeg` 是纯 Go，Windows 仍 `CGO_ENABLED=0`） |
| 解码时机 | 构造期 `ReadFile` + `image.Decode` 一次，缓存 `paint.ImageOp`；`Layout` 只回放 |

本模板的两张图（24x24 与 32x16，用来让「哪个包内嵌了哪张图」在测试里可区分）是**版本化
字节**，直接进版本控制；生成物里只写路径，不写像素：换图不改源码字节。所以验收不需要
任何资产生成器——要一张「另一张合法 PNG」时，用另一页那张就够了（验收入口就是这么做的）。

本工程的绑定页构造函数通过 `error` 报告内嵌图片解码失败。Pipeline 在生成前用
`DecodeConfig` 和完整 `Decode` 验证资产。请在可信构建目录操作，并在校验到编译期间保持资产树稳定。

## 缓存与资产：怎么验证它真的生效

下面 C1–C8 的每一条都由 `scripts/verify-gallery-accept.py` 自动执行并留下原始字节；
手工复核时按这个顺序在工程根做（顺序有意义：C3 依赖「第一次」换图）。

| 步 | 命令（工程根） | 实测结果 |
|---|---|---|
| C1 | `goxml generate view --out-dir view --check` | 两页都未命中（`cache-missing`）；`提交前检查通过：2 个目标包` |
| C2 | 同一条命令**再跑一次** | `跳过 …（缓存命中）`×2、`命中 2，生成 0`；字节、mtime、Manifest **三者零变化** |
| C3 | 再跑第三次 | 同上（命中稳定，不是偶然） |
| C4 | 换掉 `view/home/assets/badge.png` 为**另一张合法 PNG** | 只有首页重验（`命中 1，生成 1`），详情页命中；首页产物 mtime 变、字节不变 |
| C5 | 把原资产字节换回去 | `命中 2，生成 0`——原键的记录仍然有效，说明键跟**内容**走，不是跟「最近一次」走 |
| C6 | 把 `badge.png` 写成非图片字节 | rc=2；`CODEGEN-ASSET-002: asset "assets/badge.png": image: unknown format`；`批次全部放弃，磁盘无任何变更`；两页产物与 Manifest 字节/mtime 全部保旧 |
| C7 | 删除 `badge.png` | rc=2；`CODEGEN-ASSET-002`（底层是系统缺文件错误）；产物与 Manifest 仍保旧 |
| C8 | 只改构建上下文（去掉 `GOFLAGS`） | **两页同时**未命中，原因都是 `package-sources-changed`；产物与 Manifest 被真的重写（mtime 变）；回到固定环境又是 `命中 2，生成 0` |

C8 是**反例**，它存在的意义是把两件事分开：

| 变化的是 | 未命中的范围 | 原因词 |
|---|---|---|
| 资产内容（C4） | 只有引用它的那一页 | `package-sources-changed` |
| 构建上下文（C8） | 同一批里的**所有**页 | `package-sources-changed` |

原因词一样，只有范围能区分。所以「看到 `package-sources-changed` 就以为资产变了」是错的，
反过来「以为环境变了」也可能是错的——要看**几页**重新生成。

三件容易误判的事：

1. **`GOFLAGS` 是缓存键的一项，改变构建上下文会使缓存失效。** 同一个工程，用
   `GOFLAGS=-mod=readonly`（`scripts/verify-projects.py` 的固定环境）跑是
   `命中 2，生成 0`；换成裸 shell（无 `GOFLAGS`）跑立刻变成
   `缓存未命中 …：package-sources-changed`。这不是资产变了、也不是源码变了，
   而是构建上下文变了。所以「验证缓存」必须在**同一套环境变量**下跑，
   否则会把一次环境差异误读成缓存失效。
2. **资产内容变化没有专门的原因词**：资产确实进了缓存键（C4 只重验首页就是证据），
   但标签统一是 `package-sources-changed`，与 C8 的上下文变化同词。诊断可读性有限——
   这是真实的观感缺口，登记给 CLI 线，不在本模板里绕；验收入口因此把「范围」
   （几页未命中）单独记成一条判据，作为唯一的区分手段。
3. **Manifest 与缓存记录不是一回事**：C6/C7 保旧断言同时看产物字节、产物 mtime 与
   Manifest 字节。只看字节会把「重写了一遍相同内容」误判成没动。

## 两条退化变异（可编译，且对应断言会翻红）

```bash
# M1：拆掉关窗包装
#   wire/wire.go: view, err := app.WithOnDestroy(r.View(), r.Shutdown) → view := app.View(r.View())
#   go build ./... 通过；go test -run ^TestRouteDestroyWiringPresent$ ./view/... 失败
#   失败文案：生产组合未带关闭包装：对已包装视图再包一次本应被拒绝

# M2：两页各建模型
#   wire/wire.go: home(m) → home(model.NewGallery())，detail(m) → detail(model.NewGallery())
#   go build ./... 通过（m 仍被 SetNavigator 使用）；
#   go test -run ^TestRouteSharedModelSurvivesRoundTrip$ ./view/... 失败
#   失败文案：在扫描范围内未命中「打开详情」；测试观察的是共享模型上的 LastAction
```

第二个变异先红在「点击没有落在共享模型上」这条断言（它读的是共享模型的 `LastAction`），
而不是后面那条「回首页该看到新计数」——两处指向同一个事实，先到的那条先红。
`scripts/verify-gallery-accept.py` 会把**实际失败行**（`route_test.go:<行号>: …`）逐字写进
`report.json` 的 `mutations[]`，所以「红在哪一条断言」不需要人去翻终端。

两条都只改 `wire` 的接线、不改生成物，所以「能编译」是有意义的：红的不是编译错误，
而是那条接线上本该成立的事实。验收矩阵逐条跑
`go build ./...`、变异后的单个用例、以及**未变异**工程上的同名用例作为对照。

## 离屏测试在测什么（10 个用例）

`view/home/route_test.go`（跨页，package `home`）：

- 起始状态：无当前页；导航后**同一帧**应用并渲染首页；只构造首页。
- 真实点击「打开详情」：点击当帧仍是首页，下一帧切到详情页并构造实例。
- 跨页共享：详情页加一次 → 返回首页 → **首页渲染出来的**计数必须是新值。
- 静态图真进了二进制：`img0.Size()` 等于 PNG 像素尺寸（24x24），不是 gxml 里的 64dp。
- 关窗接线：已包装视图再包一次必须报错、裸视图包一次必须成功；`Shutdown` 后导航被拒绝且可重入。
- 未注册路径是同步错误，不改变当前页。
- 两次组装互不串状态（模型、Router、页面实例都独立）。

`view/detail/detail_test.go`（页内，package `detail`）：加计数后**渲染串**同步变化、
返回只发意图（路径逐字、次数恰好一次、不改业务状态）、封面解码尺寸 32x16。

为什么分成两个文件：渲染槽位（`slots` / `img0`）是**包内**字段，跨包只能看导出符号。
首页的跨页测试因此只能断言首页渲染出来的文本；详情页自己的渲染证据留在它自己的包里。

扫描纪律：按钮坐标一律**实测扫描**（不硬编码像素），每个候选点开新 harness，避免
上一次点击的残留把下一次判据变成空判据；每例帧数有硬上限；文案断言用 `==` 不用 `Contains`。

## 本模板不做什么

- 不引入新 GXML 语法、不新增 CLI 选项、不改 `Image` / `Router` / `app` 的生产代码。
- 不声称有**转场动画**：本工程覆盖注册、导航与关闭接线。
- 不自动化原生窗口：`cmd/gallery` 只构建、不运行；`DestroyEvent` 触发路径与 IME 属于
  人工/原生验收项，本工程的自动测试只覆盖离屏语义。
- 不注册成第六个示例：`examples/gxml/testdata/` 下的五个示例保持不变，本目录在
  `testprojects/` 下，不参与 `-Example` 选择器。
- 不把「两页共用模型」写成文档声明了事：它是 `TestRouteSharedModelSurvivesRoundTrip`
  逐字断言的渲染串，且有一条刻意让它翻红的变异。
