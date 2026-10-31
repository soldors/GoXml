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
