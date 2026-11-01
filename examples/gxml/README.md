# 从 GXML 到可构建的 Go 工程

`valid/`、`invalid/` 用于 Parser/Expander 的语法示例；它们引用的演示 ViewModel 不构成完整应用。`testdata/starter/` 则是完整 CLI 入门工程的源材料，包含真实 ViewModel、GXML 与窗口入口模板。

## 已有 GoXml checkout 中的用法

Windows 示例使用 PowerShell。先安装项目要求的 Go 1.26 工具链，进入仓库根目录；依赖需要已下载或允许首次正常获取。本项目 Windows 构建使用 `CGO_ENABLED=0`。

```powershell
$env:CGO_ENABLED = '0'
New-Item -ItemType Directory -Force ./bin | Out-Null
go build -o ./bin/goxml.exe ./cmd/goxml
New-Item -ItemType Directory -Force ./tmp/starter | Out-Null
Copy-Item ./examples/gxml/testdata/starter/vm.go ./tmp/starter/vm.go
Copy-Item ./examples/gxml/testdata/starter/home.gxml ./tmp/starter/home.gxml
./bin/goxml.exe generate ./tmp/starter/home.gxml --no-cache
go test ./tmp/starter
```

到这里只生成和编译，不打开窗口。输出为 `tmp/starter/home_gen.go`；手写源码是 `vm.go`，不要编辑生成文件。生成器需要真实 Go package，因此先复制 ViewModel 再生成。

若要手工启动窗口：

```powershell
New-Item -ItemType Directory -Force ./tmp/starter/cmd/starter | Out-Null
Copy-Item ./examples/gxml/testdata/starter/main.go.txt ./tmp/starter/cmd/starter/main.go
# 模板面向独立 module；在当前 checkout 使用时改为本地包路径。
$main = Get-Content ./tmp/starter/cmd/starter/main.go -Raw
$main.Replace('example.com/goxml-starter', 'github.com/goxml/goxml/tmp/starter') |
    Set-Content -Encoding utf8 ./tmp/starter/cmd/starter/main.go
go build -o ./bin/starter.exe ./tmp/starter/cmd/starter
./bin/starter.exe
```

最后一行是明确的交互运行，会打开原生窗口。关闭窗口结束程序。夜间自动测试只编译入口，不执行它；窗口外观、真实点击和呈现效果仍需人工验证。

## 独立 module

创建 `module example.com/goxml-starter`，添加与当前 GoXml 相容的依赖；本地开发可在 `go.mod` 中使用 `replace github.com/goxml/goxml => "绝对 checkout 路径"`。将 `vm.go`、`home.gxml` 放在 module 根目录，用已构建的 `goxml` 生成后，复制 `main.go.txt` 至 `cmd/starter/main.go` 即可。测试使用相同布局，在独立临时 module 中复用仓库的锁定依赖与 `go.sum`，并关闭网络下载验证。

## 每一步在保证什么

- `.gxml` 经 Parser、宏展开、Analyzer，再由只接受 VerifiedDoc 的 Codegen 生成 Go。应用运行时导入 `pkg/runtime/app`，不导入 `internal/compiler`，不运行 XML 解释器。
- `Title` 是导出的绑定字段；`Increment` 是无返回值事件处理器。它在处理事件时更新显示字符串，避免在 Layout 中每帧格式化。生成视图接入 App 的失效入口后，事件处理会请求后续帧。
- 本例只有 UI 线程修改 ViewModel。后台任务需要调用方同步数据并请求失效；`Invalidate()` 的并发安全不等于字段读写安全。
- `--no-cache` 绕过增量缓存；普通重复生成也可使用缓存。两种方式都必须保留确定的源码输出。
- 当前 `--check` 不等同于 `go build` 或 `go test`，独立构建步骤不可省略。退出码 0 成功、1 用法错误、2 生成/提交失败。
- 不要把整个 `valid/` 目录当完整应用生成；其中是语法展示，不是各自具备 ViewModel 的包。

## 自动验证

```powershell
go test -v -run '^TestStarterProject$' ./examples/gxml
```

测试使用同一份源材料，调用真实 CLI，检查两次生成字节一致；把绑定改为不存在的字段后要求 `GXML-BIND-001`，并核对旧生成文件和 Manifest 不变。最后用独立 Go 子进程编译实际生成代码和窗口 main，执行 nil ViewModel 拒绝与普通模型状态更新检查。它不运行 main，不证明原生窗口显示或点击链路，也不生成性能数据。

---

# 进阶：列表 + 条件显示 + 行事件（`testdata/tasks/`）

`testdata/starter/` 只有一个按钮和一个文本框。当页面需要**列表**、**按条件切换显示**和**每行各自的按钮**时，用 `testdata/tasks/` 这份完整工程：

- `vm.go` —— 手写 ViewModel：`Items []Task`、计算字段 `Summary`/`HasTasks`、`ShowDone`，以及四个事件处理器 `Toggle(id int)`、`RemoveAt(index int)`、`Add()`、`ToggleFilter()`。
- `tasks.gxml` —— 只使用已批准语法：`<For Each In Index Key>`、`<If>/<Then>/<Else>`、带参行事件。
- `main.go.txt` —— 窗口入口模板（复制到 `cmd/tasks/main.go`）。

教程与自动测试**共用这三个文件**：下文命令和 `TestTasksProject` 用的是同一份源材料，不存在"教程里的示例"与"测试里的示例"两份副本。

## 命令

```powershell
$env:CGO_ENABLED = '0'
go build -o ./bin/goxml.exe ./cmd/goxml
New-Item -ItemType Directory -Force ./tmp-tasks | Out-Null
Copy-Item ./examples/gxml/testdata/tasks/vm.go ./tmp-tasks/vm.go
Copy-Item ./examples/gxml/testdata/tasks/tasks.gxml ./tmp-tasks/tasks.gxml
./bin/goxml.exe generate ./tmp-tasks/tasks.gxml --no-cache
./bin/goxml.exe generate ./tmp-tasks/tasks.gxml          # 第二次：缓存命中
New-Item -ItemType Directory -Force ./tmp-tasks/cmd/tasks | Out-Null
Copy-Item ./examples/gxml/testdata/tasks/main.go.txt ./tmp-tasks/cmd/tasks/main.go
# 模板面向独立 module；在当前 checkout 使用时改为本地包路径。
$main = Get-Content ./tmp-tasks/cmd/tasks/main.go -Raw -Encoding utf8
$main.Replace('example.com/goxml-tasks', 'github.com/goxml/goxml/tmp-tasks') |
    Set-Content -Encoding utf8 ./tmp-tasks/cmd/tasks/main.go
go build -o ./tmp-tasks/tasks-entry.exe ./tmp-tasks/cmd/tasks   # 只构建窗口入口
```

> 生成器需要真实 Go package，因此**先复制 ViewModel 再生成**；窗口入口反过来要在生成**之后**安装，否则生成期的 package 上下文会引用尚不存在的构造函数。

与 starter 一样：**只生成和构建，不打开窗口**。要手工运行窗口才复制 `main.go.txt` 并执行产物，那属于人工交互步骤。

## 每一步在保证什么

| 写法 | 约束 | 来源 |
|---|---|---|
| `<For Each="task" In="{Items}" Index="i" Key="{task.ID}">` | `Key` 必须存在（循环体含 `<Button>`，缺 Key 由 Parser 以 `GXML-SCOPE-003` 拒绝）；`Key` 必须稳定可比较 | `GXML语言规范.md` §6.2、`For列表输出契约.md` §3 |
| `Click="Toggle(task.ID)"` | 实参来自行内绑定；点第 N 行必须只切换第 N 行 | `For列表输出契约.md` §5/§9 |
| `Click="RemoveAt(i)"` | `Index` 变量可作为事件实参（行索引） | `For列表输出契约.md` §5/§6.1 |
| `<If Condition="{task.Done}">` | 分支内控件**只有在分支被渲染时**才轮询与命中 | `If条件渲染输出契约.md` |
| 事件里改完字段再 `refresh()` | 显示串在事件处理器里准备，不在 Layout 内每帧拼接 | `性能预算与优化策略.md`、`AGENT.md` §2.5 |

生成的 `tasks_gen.go` 里可以看到三处关键产物（**不要手改**）：

- `rowClicks0 map[int]*widget.Clickable` 与惰性 `rowClick0(key)`：每行一份按钮状态，键是 `{task.ID}`；
- Layout 顶部的数据驱动轮询：`rowItems := v.vm.Items` 与渲染同源同序，命中后调 `v.vm.Toggle(rowItem.ID)` 并 `gtx.Execute(op.InvalidateCmd{})`——这就是事件到重绘的失效接线；
- 分支守卫：`if v.vm.HasTasks && !(rowItem.Done) { ... }`，已完成的行不会轮询"完成"按钮。

`Invalidate` 的并发安全**不等于** ViewModel 字段读写的并发安全；后台 goroutine 改数据仍由调用方同步（另见 `状态更新契约.md` §2）。

## 用户流程验证

| 操作 | 期望 |
|---|---|
| 首次 `--no-cache` 生成 | rc 0，出现 `tasks_gen.go` 与 `.goxml/generate-manifest.json` |
| 输入未变再生成（走缓存） | rc 0，字节相同**且 mtime 不变**（确实没重写文件） |
| 显式 `--no-cache` 再生成 | rc 0，字节相同（这一次会重写，mtime 变化属正常） |
| 改一个按钮文案（合法修改） | rc 0，产物字节**变化**——不是陈旧输出 |
| 把 `{Summary}` 改 `{Missing}` | rc 2，报 `GXML-BIND-001`，旧 `tasks_gen.go` 与 Manifest 字节不变 |

## 自动验证

```powershell
go test -v -run '^TestTasksProject$' ./examples/gxml
```

除上表的流程外，测试在临时 module 内编译真实生成物与窗口入口（`go build ./cmd/tasks`），并运行四个**离屏**语义断言（用 `input.Router` 合成指针事件，不创建原生窗口）：

1. **行身份**：自上而下扫描，删除按钮必须依次命中第 1/2/3 行，且三个命中区互不重叠、顺序递增。
2. **条件显示**：已完成的行（ID=1）在任何坐标都扫不到"完成"处理器——分支真的改变了控件集合。
3. **页级 If**：`Items` 非空但 `HasTasks=false` 时，扫描网格内没有行事件；另验证空列表无行事件。这不单独证明 Else 文本已渲染。
4. **键生命周期**：删行后行状态池 `rowClicks1` 从 3 收缩到 1（无界增长被关闭）。

扫描断言**不硬编码像素坐标**：按钮尺寸随字体/主题变化，测试用实测网格确定命中区，避免猜坐标造成的脆断言与静默失真。本机一次实测（560×400、`PxPerDp=1`）的命中区为：`remove:1` y 84–120、`toggle:2` y 140–168 x 456–492、`remove:2` y 136–172 x 516–540、`toggle:3` y 188–220、`remove:3` y 188–224。

**本测试不证明**：原生窗口真的显示出来、真实鼠标点击链路、以及任何性能数据。含 `<For>` 的页面按 `For列表输出契约.md` §7 不享有零分配保证。

---

# 进阶：受控表单（`testdata/forms/`）

starter 与 tasks 都没有**文本输入**。当页面需要让用户敲字、把输入回写到 ViewModel、并在回车时提交时，用 `testdata/forms/`：

- `vm.go` —— 手写 ViewModel：外部值 `Text`、预览串 `Preview`、提交记录 `SubmitCount`/`LastSubmitted`/`Status`、条件显示开关 `HasStatus`，以及四个事件处理器 `OnTextChanged(value string)`、`OnSubmit(text string)`、`Clear()`、`HideStatus()`。
- `forms.gxml` —— 只使用已批准控件：`<Input>`（受控外部值 + `ValueChanged` 回写 + `Submit` 回车提交）、`<Text>`、`<Button>`、`<If>/<Then>/<Else>`。
- `main.go.txt` —— 窗口入口模板（复制到 `cmd/forms/main.go`）。

教程、自动测试与一键脚本**共用这三个文件**，不存在"教程里的示例"与"测试里的示例"两份副本。

## 命令

```powershell
$env:CGO_ENABLED = '0'
go build -o ./bin/goxml.exe ./cmd/goxml
New-Item -ItemType Directory -Force ./tmp-forms | Out-Null
Copy-Item ./examples/gxml/testdata/forms/vm.go ./tmp-forms/vm.go
Copy-Item ./examples/gxml/testdata/forms/forms.gxml ./tmp-forms/forms.gxml
./bin/goxml.exe generate ./tmp-forms/forms.gxml --no-cache
New-Item -ItemType Directory -Force ./tmp-forms/cmd/forms | Out-Null
Copy-Item ./examples/gxml/testdata/forms/main.go.txt ./tmp-forms/cmd/forms/main.go
# 模板面向独立 module；在当前 checkout 使用时改为本地包路径。
$main = Get-Content ./tmp-forms/cmd/forms/main.go -Raw -Encoding utf8
$main.Replace('example.com/goxml-forms', 'github.com/goxml/goxml/tmp-forms') |
    Set-Content -Encoding utf8 ./tmp-forms/cmd/forms/main.go
go build -o ./tmp-forms/forms-entry.exe ./tmp-forms/cmd/forms   # 只构建窗口入口
```

## 每一步在保证什么

| 写法 | 约束 | 来源 |
|---|---|---|
| `Value="{Text}"` | 方向是 **vm → Editor** 的单向同步；回写只能经 `ValueChanged` / `Submit` 显式完成（v1 没有隐式双向绑定） | `Input输出契约.md` §3.1/§3.2 |
| `Submit="OnSubmit"` | `Submit` **只有事件形式**，没有 bool 属性；声明它即启用回车提交 | `Input输出契约.md` §2.3/§4.3 |
| `<Input>` 必须在 `<For>` 之外 | `<For>` 子树内出现 `<Input>` → Parser 报 `GXML-SCOPE-005` | `Input输出契约.md` §7 |
| 预览串在 `OnTextChanged` 里准备 | 显示串在事件处理器里准备，不在 Layout 内每帧拼接 | `性能预算与优化策略.md`、`AGENT.md` §2.5 |
| `Clear()` 恢复初始文本，保留提交记录，不碰编辑器 | 编辑器由下一帧的同步算法（V≠A → `SetText`）更新；工程代码里没有一处直接调用 `Editor.SetText` | `Input输出契约.md` §5.3 路径 1 |

生成的 `forms_gen.go` 里可以看到四处关键产物（**不要手改**）：`input0 widget.Editor` 与 `applied0 string`（每个 Input 一份"最近一次外部应用"）、`ed.Submit = true`（声明了 `Submit` 事件的派生效果）、同步判据 `if cur := v.vm.Text; cur != v.applied0`（与 `ed.Text()` 比较会导致用户编辑被回冲）、以及分支守卫 `if v.vm.HasStatus`（`click1` 只在 Then 分支被轮询）。

## 用户流程验证

| 操作 | 期望 |
|---|---|
| 首次 `--no-cache` 生成 | rc 0，出现 `forms_gen.go` 与 `.goxml/generate-manifest.json` |
| 输入未变再生成（走缓存） | rc 0，字节相同**且 mtime 不变** |
| 改一个按钮文案（合法修改） | rc 0，产物字节**变化** |
| 把 `{Preview}` 改 `{Missing}` | rc 2，报 `GXML-BIND-001`，旧产物与 Manifest 字节不变 |
| 把 `<Input>` 放进 `<For>` | rc 2，报 `GXML-SCOPE-005`，且不留产物 |

## 自动验证

```powershell
go test -v -run '^TestFormsProject$' ./examples/gxml
```

测试除上表流程外，还把一份**离屏**语义测试写进临时 module 并 `go test` 实跑（用 `input.Router` 与 `key.EditEvent` 合成输入，不创建原生窗口）：

1. **输入回写**：焦点由真实指针点击获得，`EditEvent` 经 router 队列投递；一次输入恰好调用一次 `ValueChanged`，载荷等于编辑器当前文本，`vm.Text`、`applied0`、预览串随之更新。
2. **回车提交**：`key.Event{NameReturn}` Press/Release 与输入**不同帧**；断言载荷、次数与处理器准备的提示串。
3. **无回车负对照**：提交后再跑 3 帧，`SubmitCount` 不变。
4. **清空往返**：真实点击「清空」→ 外部值改回 → 下一帧把编辑器、预览串、`applied0` 全部同步回来，且 `SubmitCount` **不变**（不重复提交）。
5. **条件提示**：未提交时全网格扫不到「隐藏提示」；`HasStatus=true` 时能扫到并点掉；点掉后同一坐标不再命中。
6. **两实例隔离**：A 输入后，B 的编辑器文本、外部值与 `applied0` 都不变。

判据取**增量**而不是绝对值：外部值应用本身也会带来一次 `ChangeEvent`（见下）。焦点坐标用网格早停扫描实测，不硬编码像素。

**本测试不证明**：原生窗口真的显示出来、真实键盘/鼠标链路、**IME 组合输入与物理焦点**（`Input输出契约.md` §9 场景 15 列为人工验收门槛，无法可靠自动化），以及任何性能数据。窗口入口只 `go build`，从不执行。

## 运行入口（人工）

```powershell
go build -o ./tmp-forms/forms-entry.exe ./tmp-forms/cmd/forms
./tmp-forms/forms-entry.exe        # 只在你想真的看窗口时才跑
```

人工验收清单（自动化没有覆盖）：窗口能显示；点一下输入框真的获得焦点、光标闪烁；中文输入法组合输入过程中不被打断、上屏后 `ValueChanged` 的载荷包含完整文本；回车后提示串出现、再点「隐藏提示」后消失。

## 常见错误

| 现象 | 原因 | 处理 |
|---|---|---|
| `<Input Enabled="...">` 报错 | v1 明确拒绝 `Enabled`（沿用 `<Button Enabled>` 裁决） | 删掉该属性 |
| `<Input Text="...">` 报未知属性 | `Text` 与 `Value` 语义冲突，Gio 无此概念 | 用 `Value` |
| 回车没有触发 `Submit` | 没声明 `Submit` 事件（`Editor.Submit` 不会自动为 true） | 加 `Submit="方法名"` |
| 敲一个字却看到处理器被调用两次 | 外部值同步调用 `SetText` 后，Gio 的 Changed 标记可使 `Update` 产出 `ChangeEvent`；本例首帧同步与用户输入可能分别通知。此处记录现有行为，副作用是否符合产品预期仍待评估 | 处理器要幂等；不要用"调用次数"当作"用户敲了几个字" |
| 处理器需要规范化文本（如转大写） | 处理器返回后生成器立即重新求值 V 并赋给 A，故下一帧 V==A 时不会再次 SetText；不能仅由 Changed 标记推出往复。规范化后的编辑器显示语义尚未在本例验证 | 本例只做同值回写；需要规范化时先验证模型、编辑器及光标行为 |
| 页面四周没有内边距 | `Padding` 目前被 Codegen 忽略（已知缺口，`size_runtime_test.go` 已注明属 TASK-CODEGEN-001-21 范围） | 等该任务卡落地；不要据此认为 `Padding` 已生效 |

## 一键验证脚本（用户直接运行）

上面每一步都要手敲命令，容易漏掉"先复制 ViewModel 再生成"这类前置条件。`scripts/verify-examples.ps1` 把三个入门工程（starter、tasks、forms）的完整流程固化成一条命令：从任意当前目录都能调用，脚本按自身位置定位仓库根。

```powershell
# 默认：starter + tasks + forms，隔离模块，输出到 <仓库>/tmp/verify-examples/<时间戳>
pwsh -File scripts/verify-examples.ps1

# 单个示例 + 指定输出目录（支持空格与中文路径）
scripts\verify-examples.ps1 -Example tasks -OutputRoot "D:\我的 构建\验证"

# 只验证表单工程（本夜各宿主都显式传 -LockPath 共用公共锁）
scripts\verify-examples.ps1 -Example forms -LockPath "D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock"

# checkout 形态：在仓库内子目录构建，自动把 main 模板的导入换成本地包路径
scripts\verify-examples.ps1 -Mode checkout -OutputRoot "<仓库>\tmp\verify-examples\r3-checkout"
```

脚本对每个示例做三件事，**从不执行窗口**：复制 `testdata/<示例>` 的同一份源材料 → 真实构建 CLI 并 `goxml generate --no-cache` → `go build` 窗口入口（只构建）。子进程输出**直通控制台**，不重定向、不转码，所以 `go` / `goxml` 的报错就是原文。

| 退出码 | 含义 |
|---|---|
| 0 | 成功（真实生成 + 真实构建，未运行窗口） |
| 2 | 前置条件/用法（缺 Go、缺 Python 或 Python 不可用、锁助手缺失、未知示例、`-SourceDir` 与 `-Example all` 同用、checkout 输出目录不在仓库内） |
| 3 | 生成失败（打印 `goxml` 真实 rc；或 rc=0 但产物缺失/为空） |
| 4 | Go 构建失败（打印 `go` 真实 rc；或 rc=0 但产物缺失） |
| 5 | 输出目录已存在（包括空目录、已有 marker 或重解析路径）（拒绝覆盖、不删用户文件、**不写运行日志**） |
| 6 | 锁被他人持有（不等待、不删他人锁） |
| 7 | 示例源材料缺失（源目录不存在、`.gxml` 不唯一、`vm.go`/`main.go.txt` 缺失） |
| 8 | 子进程超时（`-TimeoutSec`，默认 900 秒） |
| 99 | 未处理异常 |

失败时**保留证据目录**（不删除），并只释放自己 token 的锁；清理成功不会吞掉原始失败码。运行日志只在**本轮创建的**目录里写（marker 的 `runId` + `markerToken` 同时匹配）。

### 运行前提（不声称"全新机器零预置"）

| 前提 | 说明 | 缺失时 |
|---|---|---|
| Go（本机实测 `go1.26.1 windows/amd64`） | 真实构建 CLI 与窗口入口；离线可用依赖**模块缓存已就绪**（脚本传 `GOPROXY=off`，不联网） | rc 2，提示用 `-GoExe` 指定 |
| Python 3（锁助手用，只依赖标准库） | 只用于取/放锁；`-SkipLock` 时不需要 | rc 2，提示用 `-PythonExe` 指定；若解析到 Microsoft Store 的应用执行别名（`…\WindowsApps\…`）会在**执行前**拒绝，不弹商店、不静默绕锁 |
| 锁助手 `scripts/verify-examples-lock.py` | 与本脚本同目录，随仓库提供 | rc 2，打印缺失助手的完整路径（只单独拷贝 `.ps1` 会走到这条） |
| 仓库工作树（含 `examples/gxml/testdata/`） | 源材料与 `go.mod`/`go.sum` | rc 2 / rc 7 |

**没有做到的承诺**：不保证"全新机器无需任何预置即可离线构建"——Go 与 Python 必须是本机已装好的，Go 依赖需要模块缓存里已有 `gioui.org` 等（脚本不联网、不安装、不升级依赖）。

### 锁策略（可迁移默认 + 显式共用）

- **默认**：`<仓库根>/tmp/verify-examples/heavy.lock`，由锁助手按**自身位置**推导，随 checkout 迁移；另一台机器不需要任何历史目录。
- **共用夜间公共锁**：必须显式 `-LockPath D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock`。默认值不会替代、也不得用来绕过公共锁——**本夜所有实际重负载都显式传了 `-LockPath`**。
- 锁协议沿用夜间采集器的 `wcollector.proclock` 语义：原子排他创建（`O_CREAT|O_EXCL`）、只释放自己 token、**不按 mtime 删锁**、身份用真实 pid + 进程创建时间。仓库内实现只取协议本身，不含采集器的其它内容。

### 每次调用的 runId 与输出目录所有权

- `runId` = 时间戳 + PID + 随机段，**不只用 PID**：同一个 PowerShell 宿主连续两次调用，PID 相同但 runId 必须不同（实测两次调用 `pid` 相同、`runId` 与 `lockToken` 各自不同）。
- 输出目录的 marker 里写入本次的 `runId` 与 `markerToken`。输出根必须由本轮独占新建；已有目录（含空目录、已有 marker）与重解析路径一律拒绝。写运行日志时仍要求两者**同时匹配**，失败日志写入不阻止解锁，也不掩盖原始失败码；日志失败且此前成功时返回 99。拒绝其它目录时（rc 5，目录零新增，原 marker 字节不变）。只看"marker 文件存在"会把上一次/他人留下的同名 marker 误认成自己的。

本机实测（2026-10-31，Go `go1.26.1 windows/amd64`、PowerShell `5.1.26100.9711` Desktop）：两个示例 isolated 与 checkout 均 rc 0，`home_gen.go` 5545 字节、`tasks_gen.go` 9909 字节，窗口入口约 11.8 MB（starter 11824640、tasks 11849216）；非法绑定 `{Missing}` 判为 rc 3 并原样显示 `GXML-BIND-001`。

**本脚本不证明**：窗口真的显示、点击链路可用、任何性能数据，也不替代上文的 `go test`（离屏语义断言）。它只证明"这套源材料能被真实 CLI 生成、并且生成物能被真实 Go 构建"。

实现注意：`.ps1` 必须保存为 UTF-8 with BOM，以兼容 Windows PowerShell 5.1；子进程输出直通控制台，避免双流同步读取阻塞；starter 的页面文件名是 `home.gxml`，不能从示例目录名推导。

## 宿主支持范围（双宿主实测，2026-10-31）

`verify-examples.ps1` 在**本机已存在**的两个宿主上各跑过一遍，两个宿主都是系统自带/已装的，本轮**没有安装任何宿主或依赖**：

| 宿主 | 可执行文件 | 实测版本 | 本轮结论 |
|---|---|---|---|
| Windows PowerShell 5.1 | `powershell.exe` → `C:\windows\system32\windowspowershell\v1.0\powershell.exe` | `5.1.26100.9711`，PSEdition `Desktop`，CLR `4.0.30319.42000`（.NET Framework 4.8.10008.0） | 实测通过 |
| PowerShell 7 | `pwsh.exe` → `C:\Users\admin\AppData\Local\Microsoft\WindowsApps\pwsh.exe` | `7.6.9`，PSEdition `Core`，.NET `10.0.14`，OS `Microsoft Windows 10.0.26300` | 实测通过 |

两个宿主下分别验证的内容（均为真实运行，不是推理）：

- 成功路径 rc 0：`tasks_gen.go` **9909 字节**、窗口入口 **11849728 字节**、运行日志存在且是 UTF-8 **无 BOM**；
- 失败路径 rc **3**：非法绑定（`{Summary}`→`{Missing}`）保留 `GXML-BIND-001`，退出码 3 不被日志写入或失败清理覆盖；
- 真实子进程回读（Go 包装进程 `scripts/testdata/verify-process-probe`）：`argv` 与 `cwd` 与期望逐字一致（含空格 / 中文 / 单引号 / 结尾反斜杠的路径），子进程环境为 `CGO_ENABLED=0`、`GOMAXPROCS=2`、`GOPROXY=off`、`GOSUMDB=off`、`GOTOOLCHAIN=local`、`GOFLAGS=-mod=readonly`，且 `TMP`/`TEMP`/`HOME`/`PATH` 与**宿主自身**环境逐项相等。

复现命令（本机）：

```powershell
powershell.exe -NoLogo -NoProfile -NonInteractive -File scripts\verify-examples.ps1
pwsh.exe      -NoLogo -NoProfile -NonInteractive -File scripts\verify-examples.ps1
```

自动化双宿主验收（跑真实脚本 + 真实子进程，不模拟脚本内部函数）：

```text
python scripts/test-verify-examples-hosts.py --goxml-exe <已冻结的 goxml.exe> \
  --output-root <交付>/work --raw-dir <交付>/raw \
  --report <交付>/out/host-matrix.json --commands <交付>/commands.jsonl
```

报告里每个宿主都有 `status` 字段：`verified` / `failed` / `unverified-host-not-installed`；宿主不存在时**不会**被判成通过。

**未验证 / 不支持范围（诚实标注）**：Linux 与 macOS 上的 PowerShell 未实测，跨平台结论不外推；Windows PowerShell 4.0 及更早版本未实测；PowerShell 7 从 Microsoft Store 应用执行别名启动时会在自己的 PATH 前追加别名目录（86 字符前缀），这是宿主行为、不是脚本污染，因此环境继承判据比较的是子进程与 PowerShell 宿主自身的环境。

### forms 示例的双宿主实测（2026-11-02）

`-Example forms` 在两个宿主上**各跑一次**真实构建（不重跑旧全矩阵），均显式 `-LockPath` 指向公共 heavy.lock（日志回读：`已取得锁: D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock`）：

| 宿主 | 实测版本 | rc | `forms_gen.go` | 窗口入口 |
|---|---|---|---|---|
| Windows PowerShell 5.1 | `5.1.26100.9711` Desktop | 0 | 7212 字节 | `forms-entry.exe` 11976704 字节 |
| PowerShell 7 | `7.6.9` Core | 0 | 7212 字节 | `forms-entry.exe` 11976704 字节 |

选择器专项（`scripts/test-verify-examples-forms.py`，持公共锁 + `-SkipLock`，判据见脚本头）：`-Example forms` 只出现 forms 子目录；`-Example all` 三个子目录齐全且各有生成物与入口产物；`-Example bogus` rc 2 且提示"未知示例 … starter / tasks / forms / all"；源材料缺 `vm.go` → rc 7；`-Example all` 与 `-SourceDir` 同用 → rc 2（既有规则未被破坏）。

> 测试**自己重定向**子进程输出时会遇到**混合编码**：脚本自身的 `Write-Host` 是控制台代码页（简体中文环境 cp936），而 `go` / `goxml` 子进程的原样输出是 UTF-8，同一个缓冲区里两种编码并存。脚本正常运行时**不重定向**（输出直通控制台），因此这不是脚本的行为差异；写测试时请按行容错解码，不要假设单一编码。

## 可迁移性验证（2026-11-01）

`scripts/test-verify-examples-portable.py` 只跑与可迁移性/所有权**有关**的判据，不重跑上一批的 10 项、也不重复双宿主矩阵：

```text
python scripts/test-verify-examples-portable.py --goxml-exe <已冻结 goxml.exe> \
  --output-root <交付>/work --raw-dir <交付>/raw \
  --report <交付>/out/portable.json --commands <交付>/commands.jsonl
```

| 用例 | 判据 |
|---|---|
| 默认锁路径在仓库内 | 隔离布局（系统临时目录下、路径含空格与中文）里跑助手，默认路径必须是布局内的 `tmp/verify-examples/heavy.lock` |
| 自己的 token 可取可放 | acquire→status→release，`released=True` 且文件确实消失 |
| 错误 token 不释放 | rc 78、`released=False`、锁文件字节不变 |
| 不导入历史目录 | `PYTHONVERBOSE=1` 跑助手，import 轨迹里无历史路径；两个随仓工具文件正文无历史目录字面量 |
| 缺锁助手 / 缺 Python | rc 2 且信息明确（布局路径含空格与中文） |
| 外来 marker / 无 marker 的已有目录 | rc 5 且目录**零新增**、原文件字节不变 |
| 同宿主两次调用 | 两份 marker 的 `pid` 相同、`runId` 与 `lockToken` 各自不同 |
| 隔离布局内成功一次 | 一个示例（tasks）+ 一个宿主（PS 5.1）+ 复用已冻结 CLI，rc 0（`tasks_gen.go` 9909 字节、入口已构建），重负载显式传公共锁 `-LockPath` |

实测（runId `portable-20261101-012730-eed0cab3`）：10 项全 `verified`，`failures = []`。
