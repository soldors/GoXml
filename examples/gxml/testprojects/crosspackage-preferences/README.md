# 跨包偏好设置工程模板

一个可以**真的复制出去、真的生成、真的编译、真的跑测试**的最小工程：状态在一个独立的
`model` 包，两个视图由 Goxml 生成到**另外两个包**，窗口入口用现有 Go 代码把两个视图
组合起来，并且**共享同一个模型指针**。

它示范的是「生成输出与 ViewModel 分属不同包」这条接线，不是新的 GXML 语法：

```
                   model.NewPreferences()   ← 只调用一次
                            │  *model.Preferences
              ┌─────────────┴─────────────┐
              ▼                           ▼
  edit.NewGoxmlEditView(m)      summary.NewGoxmlSummaryView(m)
   （package edit，可编辑）        （package summary，只读）
              └──────── 同一个指针 ────────┘
```

## 目录图

```
crosspackage-preferences/
├── README.md                       本文件（不参与构建）
├── go.mod.txt                      → go.mod            模块定义（占位符 __MODULE__ / __GOXML_DIR__）
├── model/
│   └── preferences.go.txt          → model/preferences.go            唯一手写的业务类型
├── view/
│   ├── edit/
│   │   ├── doc.go.txt              → view/edit/doc.go                先有包，才能生成（见下）
│   │   ├── edit.gxml               → view/edit/edit_gen.go           编辑视图源
│   │   └── sem_test.go.txt         → view/edit/sem_test.go           编辑视图离屏语义测试
│   └── summary/
│       ├── doc.go.txt              → view/summary/doc.go
│       ├── summary.gxml            → view/summary/summary_gen.go     只读摘要视图源
│       └── sem_test.go.txt         → view/summary/sem_test.go        跨视图语义测试
└── cmd/
    └── preferences/
        └── main.go.txt             → cmd/preferences/main.go         窗口入口（只构建，不运行）
```

`*_gen.go` 不在模板里——它们是**生成结果**，由你在自己的目录里生成。

为什么是 `*.go.txt` / `go.mod.txt`：如果它们叫 `.go` / `go.mod`，这些目录就会成为
Goxml 仓库根模块里的普通包，跟着一起被构建、被 vet；改名后它们只是**模板文本**。
本仓库的工程级测试（`examples/gxml/crosspackage_project_test.go`）读的就是这份材料，
测试与教程不各写一份。

## 一条命令：复制 + 填占位符 + 生成 + 构建 + 可选离屏测试

不用再手抄下面那串 `find`/`mv`/`sed`：仓库自带入口脚本会按顺序做完，并逐阶段留证据。

```bash
python scripts/verify-projects.py crosspackage-preferences \
    --out-dir "/d/work/我的偏好工程" \
    --lock-path "D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock"
```

- 目标目录**必须不存在**：本工具只写「本轮独占创建」的新目录。已有目录（含空目录、
  别人的标记文件、符号链接）一律拒绝，退出码 3，**一个字节都不动**。
- 目录名可以含空格与 Unicode（全角括号也行）；`go.mod` 里的仓库路径写成**原生**
  绝对路径并按 Go 规则转义（`"C:\\Users\\..."`），不需要你再把反斜杠改成正斜杠。
- 公共锁必须显式传入（`--lock-path`）：他人持锁时直接退 8，**连输出目录都不创建、
  一个 Go 进程都不启动**。

阶段与产物落点：

| 阶段 | 干什么 | 产物/证据落点 |
|---|---|---|
| `copy` | 复制模板、`*.go.txt`→`*.go`、替换占位符、补 `go.sum` | 工程目录；模板源码不改 |
| `build-cli` | `go build ./cmd/goxml` | `<工程>/.verify-projects/goxml.exe` |
| `generate:<dir>` | 原生 CLI **一次目录生成**覆盖本批全部 View | `view/edit/edit_gen.go`、`view/summary/summary_gen.go` |
| `build-entry` | **只构建**窗口入口 | `<工程>/.verify-projects/preferences.exe` |
| `test` | 可选离屏语义测试（`--skip-tests` 可跳过） | 每阶段一个证据目录 |

每个阶段的实际 argv / cwd / 退出码 / 收口结论 / stdout·stderr 原始字节与 sha256
都在 `<工程>/.verify-projects/<阶段>/`，汇总在 `<工程>/.verify-projects/report.json`。

退出码：`0` 全过 · `2` 用法错误 · `3` 拒绝（目录/模板/模板被改）· `4` 业务失败
（某阶段退出码非 0）· `5` 超时 · `6` 进程树收口未证实 · `7` 记录不完整 · `8` 锁忙 ·
`9` 锁未按自有 token 释放。

边界（说清楚，不夸大）：

- **窗口入口只构建、不运行**——执行它才会创建原生窗口；自动流程从不执行。
- 原生窗口与输入法**没有**被自动验证；离屏测试只覆盖 View 的语义与状态流转。
- `--check` 转发给 `generate`，在提交候选生成物之前执行目标包编译检查，成功后仍正常写盘。
  缓存全命中也会检查；它不是 dry-run。报告只有在每个生成阶段实际成功且带检查成功标记时
  才记 `passed=true`；兼容旧 CLI 时缺标记记 `null`，不能把已转发当作已检查。
- 两个 View 由**一次目录 generate** 覆盖（`generate view --out-dir view`），不是逐页两条
  命令：批次的检查在写盘前统一执行，任一目标包编不过时两页的旧产物、Manifest、成功缓存
  都保持原样；逐页调用会留下一页新、一页旧的混合状态。报告里 `generate:<dir>` 阶段的
  `checkTargets` 是这次检查覆盖的目标包数（本项目为 2）。
- `--log-dir` 如显式指定，必须不存在；输出目录拒绝后不会写入他人的日志目录。
  `--json` 不覆盖已有报告或重解析目标，失败时只更新本轮确实拥有的证据文件。
- 入口脚本是 Python（标准库），只在 Windows + 本机 Go 工具链上实测过，**不宣称**
  PowerShell / Linux / macOS 全覆盖。

### 手动步骤（排障时看）

一条命令跑不通时，下面这几步就是它内部做的事，可以逐条手工复现、定位卡在哪。
命令以 Git Bash 为例（shell 参数可用 `/c/...` 路径）；`<repo>` 是本仓库 checkout 的
根目录，`goxml` 是构建出来的 CLI：

```bash
# 0) 准备 CLI（只做一次）
go build -o /tmp/goxml ./cmd/goxml

# 1) 复制模板（目标目录名可以含空格与中文）
cp -r <repo>/examples/gxml/testprojects/crosspackage-preferences "/d/work/我的偏好工程"
cd "/d/work/我的偏好工程"

# 2) 把模板文本改名为真源文件（含 go.mod.txt）
find . -name '*.go.txt' -exec sh -c 'mv "$1" "${1%.txt}"' _ {} \;
mv go.mod.txt go.mod

# 3) 复制依赖校验文件：离线构建必需，不联网、不 go mod tidy
cp <repo>/go.sum .

# 4) 填两个占位符（__GOXML_DIR__ 用 `/` 分隔的绝对路径；手工改成 / 最省事，
#    入口脚本写的是转义后的原生路径 "C:\\...\\Goxml"，两者等价）
sed -i 's|__MODULE__|example.com/my-prefs|g' go.mod cmd/preferences/main.go \
    view/edit/sem_test.go view/summary/sem_test.go
sed -i 's|__GOXML_DIR__|D:/Codes/Goxml|g' go.mod

# 5) 一次目录生成两个视图（发现根=输出根=view，产物仍落在 view/edit 与 view/summary）
#    目录模式按「输出根 + 相对路径」映射，所以 --out-dir 要与输入目录一致；
#    写成 `--out-dir .` 会把产物放到 ./edit/edit_gen.go（不是模板里的位置）。
/tmp/goxml generate view --out-dir view

# 6) 离线构建整个工程（只构建，不运行）
CGO_ENABLED=0 GOFLAGS=-mod=readonly go build ./...

# 7) 跑模板自带的离屏测试（不需要窗口、不需要输入法）
CGO_ENABLED=0 GOFLAGS=-mod=readonly go test ./view/...
```

写入 `go.mod` 的路径必须是 Go 能识别的原生绝对路径（Windows 示例为
`D:/Codes/Goxml`），不是 `/d/Codes/Goxml`：Git Bash 不会转换文件里的路径。
仓库路径含空格时，`replace` 右侧使用 Go 模块文件支持的双引号路径。

生成成功的输出形如：

```
缓存未命中 view/edit/edit_gen.go：cache-missing
已生成 view/edit/edit_gen.go
缓存未命中 view/summary/summary_gen.go：cache-missing
已生成 view/summary/summary_gen.go
```

跑起来（**会创建原生窗口**，本仓库的自动测试从不执行这一步）：

```bash
go run ./cmd/preferences
```

## 为什么每个视图目录里都要有一个 `doc.go`

生成之前，`view/edit/` 里只有 `.gxml`。这时 `go list` 在该目录看不到任何非测试
Go 源文件，CLI 无法为目标包定名，会直接拒绝：

```
goxml generate: .../view/edit/edit.gxml: failed to load target package:
目录 ...\view\edit 没有可定名的非测试 Go 源文件: package directory is uninitialized
```

退出码 2，磁盘零变更。这是 `代码生成输出契约.md` §3.2 的既有要求：**产物目录必须能
解析为明确的 Go package**。所以顺序是「先有包，再生成页面」——`doc.go` 就是那个包。

反向的坑也有一条：目标目录里**多出第二个 package** 时同样 fail-closed，而不是抓一个
算一个：

```
failed to load target package: 目录 ...\view\edit 存在多个非测试 package 或非法包声明:
found packages other (conflict.go) and edit (doc.go) in ...\view\edit
```

## 三个包各管什么

| 包 | 来源 | 职责 |
|---|---|---|
| `model` | 手写（`model/preferences.go.txt`） | 唯一状态所有者：草稿 `Name`/`Port`、已保存值 `SavedName`/`SavedPort`、派生显示串 `SavedSummary`/`DirtyText`、计数、`Validate`/`Confirm`/`Reset`/`ClearStatus`/`OnNameSubmit` |
| `view/edit` | 生成（`edit.gxml`） | 两个 `<Input>`（`MaxLen` / `Filter`）+「确认」+「重置」+ 提示区 `If` |
| `view/summary` | 生成（`summary.gxml`） | 只读：绑定 `SavedName` / `SavedPort` / `SavedSummary`，没有任何事件与输入框 |

两个 `.gxml` 都写 `ViewModel="model.Preferences"`（**相对模块根**的包目录 + 类型名），
所以：

- 生成产物的 `package` 子句是各自目录的包名（`edit` / `summary`）；
- 产物显式导入 `.../model`。当前生成器可能以源视图包名作为别名，实际源码为
  `import edit ".../model"` / `*edit.Preferences`（摘要同理为 `summary`）；
  限定符虽不同，仍是同一个 model 包中的类型。该命名可读性问题保留待修，
  不把教程中的概念类型 `*model.Preferences` 冒充实际生成源码；
- 入口 `cmd/preferences/main.go` 调用 `model.NewPreferences()` **一次**，把同一个指针
  交给两个构造函数（`NewGoxmlEditView(m)` / `NewGoxmlSummaryView(m)`）。

`main.go` 用 `layout.Flex` 把两个 View 左右并排：**Goxml 目前没有路由**
（`pkg/runtime/router` 仍是规划项），所以这里不做页面切换。并排的好处是
「共享同一个模型」肉眼可见：左边点「确认」，右边下一帧就是新的已保存值。

## 缓存与 Manifest：怎么验证它真的生效

- 缓存命中的证据不止「输出一样」：还要看**产物 mtime 没变**（说明确实没重写）和
  `.goxml/generate-manifest.json` 的字节没变。只看字节会把「重写了一遍同样的内容」
  误判成命中。
- 缓存键包含依赖闭包：**只改 `model` 包**（哪怕只改一行注释）就会让两个视图的缓存
  失效——产物字节不变，但会被真重写。
- `--no-cache` 绕过的是缓存，不是写盘：字节相同也照写。
- 结构错误（XML 不闭合）与类型错误（模型字段消失）都必须**保留旧产物**，退出码 2，
  磁盘零变更。
- Manifest 在**模块根**的 `.goxml/generate-manifest.json`，一次生成一条记录；
  两个视图各占一条。

这些都由 `examples/gxml/crosspackage_project_test.go` 逐个判定，不靠文档声明。

## 模板自带的测试在测什么

`view/edit/sem_test.go` 与 `view/summary/sem_test.go` 是离屏语义测试：用真实
`input.Router` 投递 pointer / key 事件，不创建窗口。

- 编辑视图：改草稿不动已保存值、`MaxLen` 按 rune 截断、`Filter` 过滤非数字、
  确认写两份已保存值并重算派生串、校验失败保旧值、`Reset` 恢复并回写编辑器、
  回车只记载荷不回写草稿（端口框没接线，回车无效）、提示区 If/Then→Else 两态、
  两个实例互不串状态。
- 摘要视图：**跨视图**验证——在同一个 Router 上驱动编辑视图，断言摘要视图**渲染出来
  的文本**：草稿改了摘要不动、确认后同一帧更新、`Reset` 后不变、两个工程实例隔离。
- 焦点与按钮坐标全部**实测扫描**（不硬编码像素）；按钮按状态重扫（提示区出现会挪动
  「清除提示」）；每例帧数有硬上限。

一条与直觉相反的实测结论：`EditEvent` 由编辑器在**自身 Layout 时**消费，回写晚于同帧
的 `Text` 渲染，所以草稿上方的脏标记要**下一帧**才更新；而按钮点击在 Layout 开头被
消费，摘要视图同一帧就能看到新的已保存值。

## 生成产物的一个现状（如实记录）

当前 CLI 把 ViewModel 导入的**别名**取成 `.gxml` 所在目录的包名，而不是 ViewModel
所在包的包名。于是 `view/edit/edit_gen.go` 顶部是：

```go
import edit "example.com/my-prefs/model"   // 别名是 edit，被导入的是 model 包
```

类型是同一个类型（`*edit.Preferences` 就是 `model.Preferences`），`go build` /
`go vet` 都通过，语义正确；只是读起来容易误会。这条已作为**真实缺陷**记录在交付的
`findings/` 里交给 CLI 所属线，本模板不修改 CLI，也不靠改别名绕过去。

## 本模板不做什么

- 不引入新 GXML 语法、不新增 CLI 选项、不改 `Input` 的既有语义
  （`MaxLen` 按 rune 计数、`Filter` 过滤后不产生 `ValueChanged`、`Submit` 不回写草稿）。
- 不声称框架支持路由或滚动：两栏并排是**入口自己**用 Go 布局代码写的，
  不是 `Page Route` 生效。`Route` 只是声明，当前不驱动导航。
- 原生窗口与 IME 属于**人工验收项**：自动测试只编译 `cmd/preferences`，从不执行它，
  也不模拟输入法预编辑。真实键盘、真实输入法、真实 DPI 下的表现未在本仓库自动验证。
- 不注册成第六个示例：`examples/gxml/testdata/` 下的五个示例保持不变，
  本目录在 `testprojects/` 下，不参与 `-Example` 选择器。
