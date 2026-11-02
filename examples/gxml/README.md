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

# 进阶：可编辑列表与表单联动（`testdata/workbench/`）

`tasks` 的列表行只有按钮、没有编辑；`forms` 只有一个孤立输入框、没有列表。当页面需要"一份**带稳定 ID** 的列表，点中某一行后在**列表之外**编辑它、保存写回列表，还能新增与删除"时，用 `testdata/workbench/`：

- `vm.go` —— 手写 ViewModel：`Item{ID, Title}` 列表、`SelectedID`（`0` 表示未选中）、草稿 `Draft`、选中行文案 `Selection`、结果提示 `Status`/`HasStatus`、空态开关 `HasItems`，以及事件处理器 `Select(id int)`、`OnDraftChanged(value string)`、`SaveSubmit(text string)`、`Save()`、`Add()`、`Remove(id int)`、`ClearStatus()`。
- `workbench.gxml` —— 只用已批准控件：`<For>` + 行内 `<Button>`（**传 `item.ID`**）、`<Text>`、`<If>/<Then>/<Else>`，以及**在 `<For>` 之外**的唯一 `<Input>`（草稿编辑器）。
- `main.go.txt` —— 窗口入口模板（复制到 `cmd/workbench/main.go`）。

教程、自动测试与一键脚本**共用这三个文件**，不存在"教程里的示例"与"测试里的示例"两份副本。

## 命令

```powershell
$env:CGO_ENABLED = '0'
go build -o ./bin/goxml.exe ./cmd/goxml
New-Item -ItemType Directory -Force ./tmp-workbench | Out-Null
Copy-Item ./examples/gxml/testdata/workbench/vm.go ./tmp-workbench/vm.go
Copy-Item ./examples/gxml/testdata/workbench/workbench.gxml ./tmp-workbench/workbench.gxml
./bin/goxml.exe generate ./tmp-workbench/workbench.gxml --no-cache
New-Item -ItemType Directory -Force ./tmp-workbench/cmd/workbench | Out-Null
Copy-Item ./examples/gxml/testdata/workbench/main.go.txt ./tmp-workbench/cmd/workbench/main.go
# 模板面向独立 module；在当前 checkout 使用时改为本地包路径。
$main = Get-Content ./tmp-workbench/cmd/workbench/main.go -Raw -Encoding utf8
$main.Replace('example.com/goxml-workbench', 'github.com/goxml/goxml/tmp-workbench') |
    Set-Content -Encoding utf8 ./tmp-workbench/cmd/workbench/main.go
go build -o ./tmp-workbench/workbench-entry.exe ./tmp-workbench/cmd/workbench   # 只构建窗口入口
```

## 每一步在保证什么

| 写法 | 约束 | 来源 |
|---|---|---|
| `<For … Key="{item.ID}">` | 行状态按**稳定业务键**复用；`<For>` 子树里的 `<Button>` 必须有 `Key`，否则 Parser 报 `GXML-SCOPE-003` | `GXML语言规范.md` / Parser 诊断 |
| `Click="Select(item.ID)"` | 行事件传 **ID 而不是下标**：删除中间一行后下标会变，ID 不会 | 本例实测（删除他行后选中仍为 `#1`，剩余 ID 1 / 3） |
| `<Input>` 必须在 `<For>` 之外 | `<For>` 子树内出现 `<Input>` → Parser 报 `GXML-SCOPE-005`。所以草稿编辑器只有一份、放在列表下方 | `Input输出契约.md` §7 |
| `ValueChanged="OnDraftChanged"` 只写草稿 | **输入不改列表**：只有 `Save()` / `Submit` 才把 `Draft` 写进 `Items[i].Title` 并递增 `Saves` | 本例 `save()` 负面对照 |
| 保存使用**重新求值的 `Draft`** | `SaveSubmit(text)` / `Save()` 都不使用事件实参，而是按 `Input输出契约.md` §5.3 路径 2 在处理器返回后重新求值 V | `Input输出契约.md` §5.3 |
| 校验失败只写 `Status` | 未选中 → "未选择任何行，未保存"；草稿仅空白 → "草稿为空，未保存 #N"；两者都**不动列表、不递增 `Saves`** | 本例实测 |
| 空态走 `HasItems` 的 Else | 列表为空时显示"列表为空，先点新增"，不让草稿区看起来"可以保存" | 本例实测 |
| 计数是**派生量**，不是计数器 | `CountText = "共 N 项"` 在 `refresh()` 里由 `len(Items)` 重算，不随事件累加 —— 不存在"某条路径忘了 +1" | 本例实测（初始 3 → 新增 4 → 删除 3 → 空 0） |
| 脏标记也是**派生量** | `Dirty = 已选择 && 草稿 != 该行标题`，`DirtyText` 是它的显示串。多一个独立变量就多一处忘记清空的路径 | 本例实测（选中不脏 → 输入脏 → 保存不脏 → 删除选中项不脏） |

生成的 `workbench_gen.go` 里可以看到（**不要手改**）：行按钮的两份状态池 `rowClicks0`（选择）/`rowClicks1`（删除）与按 key 的回收逻辑、`input0` + `applied0`（唯一 Input 的"最近一次外部应用"）、`ed.Submit = true`（声明了 `Submit` 事件）、以及三处分支守卫 `HasItems` / `HasSelection` / `HasStatus`。

## 用户流程验证（自动）

```powershell
go test -v -run '^TestWorkbenchProject$' ./examples/gxml
```

生成侧沿用与 forms 相同的判据（首次生成出现产物、缓存命中 mtime 不变、合法修改产物变化、非法绑定 `GXML-BIND-001` 保留旧产物、`<Input>` 放进 `<For>` → `GXML-SCOPE-005` 且不留产物）。

此外把一份**离屏**语义测试写进临时 module 并 `go test` 实跑（用 `input.Router` 的 pointer 事件、`key.EditEvent` 与分帧 `key.NameReturn`，不创建原生窗口），共 12 条：一条完整链、五条负面对照，以及空态恢复、两实例隔离、计数、脏标记、多画布定位与重复执行：

1. **全链路**：真实点击某行的「选择」→ 焦点由真实指针点击获得 → `EditEvent` 输入 → 回车（Press/Release 分帧）→ 标题写回列表 → 再点该行「删除」→ 列表少一项、**编辑器被清空**（选中项已不存在）。
2. **保存按钮链路**：不按回车、改点「保存」，同样写回列表。
3. **不保存不改列表**（负对照）：只输入不回车/不点保存，列表标题与 `Saves` 都不变。
4. **空草稿被拒**（负对照）：草稿仅空白时保存 → `Status="草稿为空，未保存 #2"`、`Saves=0`。
5. **删除他行 ID 不串**（负对照）：删掉另一行后，当前选中仍是 `#1`，剩余 ID 为 1 / 3。
6. **删除后行状态池清理**：池大小 3→2，被删 key 已回收（不泄漏、不复用错位）。
7. **空列表 → 新增**：删空后 Else 分支生效；点「新增」得到新 ID=4、池=1。
8. **两实例隔离**：A 选中 `#1` 后，B 的 `SelectedID` 仍为 0、B 的编辑器文本为空。
9. **计数跟随真实点击**：初始"共 3 项" → 点「新增」"共 4 项" → 点「删除」"共 3 项" → 空列表"共 0 项"。
10. **脏标记生命周期**：未选择不脏 → 刚选中不脏（草稿==标题）→ 真实输入后脏 → 回车保存后不脏 → 删除选中项后不脏。
11. **换画布尺寸仍能定位**：720×480 / 480×360 / 1024×768 三套尺寸各自全网格扫描，六个行事件（`select:1..3`、`remove:1..3`）在**三套尺寸下都命中**，且命中坐标互不相同（`remove:3` 的 x 分别为 672 / 432 / 976）——证明用例测的是布局语义，不是几个魔数。
12. **完整链重复 5 次**：同一条"选择 → 输入 → 回车保存"跑 5 遍，每遍草稿、写回结果、行数都一致。

    这条**证明的不是"不 flaky"**：该链路没有并发、随机或时间依赖，是完全确定性的，重复多少次都不会翻出新的失败模式。它真正覆盖的是**跨轮之间共享缓存不污染**——扫描命中点（`baseHits`）与焦点坐标（`focusPos`）是包级缓存，一轮跑完再跑一轮，若某处把状态写进了共享变量就会在这里暴露。需要证明"不 flaky"的用例必须靠 N 次重跑，但不要把确定性用例的重复说成稳定性证据。

判据取**增量**而不是绝对值：所有按钮定位都用网格早停扫描实测坐标（不硬编码像素），且断言落到**不可被覆盖的计数器**（`Selects/LastSelectID`、`Removes/LastRemoveID`、`Saves`、`Adds/LastAddID`、`StatusClears`）——外部值应用本身也会带来一次 `ChangeEvent`，会把"最近一次动作"这类字段重写成 `change`（见 forms 的常见错误）。

**直接调用 VM 方法不算事件验收**：上面每一条都经 router 队列投递真实事件，没有任何一条是靠 `vm.Select(1)` 之类直调达成的。

## 删除后再新增（复核补充，独立 5 条）

上一节 12 条走的是"选择 → 编辑 → 保存"与单点删除。复核补充另外固化了 5 条，专门走**删除 → 新增 → 选择 → 编辑 → 保存/回车**这条顺序——先删后增，不是把顺序调成"先增后删"来回避布局位移：

```powershell
go test -v -run '^TestWorkbenchSemReview' ./examples/gxml
```

| 用例 | 判据 | 本条实测观察 |
|---|---|---|
| 删除后按**当前**几何重扫，再新增/选择/编辑/保存 | 删除后底部多出一行状态提示、页级按钮整体上移，删除前缓存的坐标不再命中——必须重扫，不能靠调换顺序绕开 | 用删除前的坐标点 `add:4` 命中空串（`动作=""`，未触发任何事件），重扫后才命中 |
| 删除选中项清空草稿与编辑器 | `SelectedID==0`、`Draft==""`、编辑器文本为空、计数回到"共 2 项"、不脏 | 五项逐项相等 |
| ID 不复用、行 Key 跟随数据 | 删除后新增拿到的是**新** ID（不回收旧号），行 Key 集合始终等于 ID 集合 | `[1 2 3]` → 删#2 `[1 3]` → 增 `[1 3 4]` → 删#1 → 增 `[3 4 5]` |
| 两实例隔离（当前几何） | A 选中 `#1` 后，B 的 `SelectedID==0`、`Draft==""`、B 的编辑器为空 | 逐项相等 |
| `DirtyText` 两态**逐字**相等 | 四态文案必须逐字等于 `（无未保存修改）` 或 `（草稿有未保存的修改）`，不接受子串判据 | `无 → 无 → 有 → 无` |

**证据边界（三条都要连着读）**：

- 这 5 条与上面 12 条**并列、不重复计数**：12 条是上一轮已交付的成果，本轮只在改动文件上窄测 `-run '^TestWorkbenchSemReview'`，没有把 12 条重跑一遍再当作新结论。
- 本节观察全部来自**离屏**测试（真实生成物 + 真实 router 事件 + 分帧 `key.NameReturn`），**不含**窗口显示、IME 组合输入、光标与任何性能数据；那些仍在"运行入口（人工）"的清单里，状态是**未通过（未执行）**。
- 作者自测**不等于**非作者复核：用例与观察由本 lane 产出，外部复核状态仍是**待独立复核**，不要读成"已复核通过"。

## 运行入口（人工）

```powershell
go build -o ./tmp-workbench/workbench-entry.exe ./tmp-workbench/cmd/workbench
./tmp-workbench/workbench-entry.exe        # 只在你想真的看窗口时才跑
```

人工验收清单（自动化**没有**覆盖，不要当成已通过）：窗口能显示；点某一行的「选择」后草稿区出现该行标题、并真的获得焦点且光标闪烁；中文输入法组合输入过程中不被打断、上屏后草稿包含完整文本；回车后列表标题更新且提示串出现；删除后列表立刻少一行、草稿区回到未选中；窗口缩放后行布局不重叠。

## 常见错误

| 现象 | 原因 | 处理 |
|---|---|---|
| `<Input>` 放进 `<For>` 报 `GXML-SCOPE-005` | 契约禁止（每个迭代一份编辑器，状态无法按 key 收敛） | 草稿编辑器放到 `<For>` 之外，用"选中行 + 一份编辑器"模型 |
| `<For>` 里的 `<Button>` 报 `GXML-SCOPE-003` | 行内交互控件缺 `Key` | 加 `Key="{item.ID}"` |
| 删掉中间一行后"保存到了别的行" | 行事件传下标，下标在删除后串位 | 传 `item.ID`，按 ID 定位 |
| 输入了但列表没变 | 设计如此：`ValueChanged` 只写草稿，写回列表只发生在 `Save()` / `Submit` | 回车或点「保存」 |
| 提示"草稿为空，未保存" | 草稿去除空白后为空；这是校验，不是失败 | 输入内容再保存 |
| 输入一次却看到处理器被调用两次 | 同 forms：外部值同步调用 `SetText` 后 Gio 的 Changed 标记可使 `Update` 产出 `ChangeEvent`；首帧同步与用户输入可能分别通知。本例只测到"调用次数多于按键次数"这一现象，IME 与真实按键的分帧细节未测 | 处理器要幂等；不要用"调用次数"当作"用户敲了几个字" |
| 页面四周没有内边距 | `Padding` 目前被 Codegen 忽略（已知缺口，属 TASK-CODEGEN-001-21 范围） | 等该任务卡落地；不要据此认为 `Padding` 已生效 |
| "未保存"标记点了保存还不消失 | 多半是把脏标记做成了**独立变量**，在保存/删除/选择某条路径里忘了清空 | 改成派生量：`已选择 && 草稿 != 该行标题`，一处重算即可 |
| 自己写离屏测试时点击"没反应" | 缓存的命中坐标只在**缓存时那套布局**下有效：本页删除后底部会多出一行状态提示，页级按钮整体上移，旧坐标就不再命中（本轮自伤 7） | 布局变了就重扫；或把用例顺序安排成不触发布局位移 |
| 提示"有未保存的修改"，点保存却说"草稿为空" | 草稿里只有空格时确实与列表不同（所以判脏），但保存按"去除空白后为空"拒绝——是刻意的：不让一行被写成空白标题 | 输入可见内容再保存 |
| 用 `strings.Contains(文案, "未保存")` 判断脏标记 | 两态文案是"（草稿**有**未保存的修改）"与"（**无**未保存修改）"，"未保存"三字在两态里都有，这个判据**永远不红** | 逐字断言两态文案，或直接断言 `Dirty` 布尔值 |

输入处理器的幂等性不能单独保证编辑器与模型一致：例如转大写是幂等的，但当前同步算法仍可使编辑器保留 `abc`、模型变为 `ABC`。[有界实验记录](../../docs/05-开发/2026-11-02-D3-输入同步有界实验观察.md)区分了程序化通知、显示/模型分歧和未复现的往复；本工作台采用原样草稿回写，不据此修改公开输入语义。

# 进阶：设置编辑 → 校验 → 审阅 → 提交/撤销（`testdata/preferences/`）

starter 只有一个按钮、tasks 是列表、forms 是孤立输入框、workbench 是列表 + 一份草稿。当页面需要"**两份互不干扰的草稿**、先**校验**再**审阅**、最后确认才写入已保存值，还能一键撤销"时，用 `testdata/preferences/` 这份完整工程：

- `vm.go` —— 手写 ViewModel：草稿 `Name` / `Port`、已保存值 `SavedName` / `SavedPort`、派生脏标记 `Dirty` / `DirtyText`、只读确认串 `ConfirmText`、审阅信息 `ReviewText`、提示 `Status` / `HasStatus`、编辑审阅开关 `InReview`，以及事件处理器 `OnNameChanged(value string)`、`OnPortChanged(value string)`、`OnConfirmChanged(value string)`、`OnNameSubmit(text string)`、`BeginReview()`、`Commit()`、`CancelReview()`、`Reset()`、`ClearStatus()`。
- `preferences.gxml` —— 只使用已批准控件：三个 `<Input>`（名称 / 端口 / **只读确认**）、`<Text>`、`<Button>`、`<If>/<Then>/<Else>`；没有 `<For>`、没有 `Image` / `Router` / `ThemeRef` / `Expanded`。
- `main.go.txt` —— 窗口入口模板（复制到 `cmd/preferences/main.go`）。

教程、自动测试与一键脚本**共用这三个文件**，不存在"教程里的示例"与"测试里的示例"两份副本。

## 命令

```powershell
$env:CGO_ENABLED = '0'
go build -o ./bin/goxml.exe ./cmd/goxml
New-Item -ItemType Directory -Force ./tmp-preferences | Out-Null
Copy-Item ./examples/gxml/testdata/preferences/vm.go ./tmp-preferences/vm.go
Copy-Item ./examples/gxml/testdata/preferences/preferences.gxml ./tmp-preferences/preferences.gxml
./bin/goxml.exe generate ./tmp-preferences/preferences.gxml --no-cache
New-Item -ItemType Directory -Force ./tmp-preferences/cmd/preferences | Out-Null
Copy-Item ./examples/gxml/testdata/preferences/main.go.txt ./tmp-preferences/cmd/preferences/main.go
# 模板面向独立 module；在当前 checkout 使用时改为本地包路径。
$main = Get-Content ./tmp-preferences/cmd/preferences/main.go -Raw -Encoding utf8
$main.Replace('example.com/goxml-preferences', 'github.com/goxml/goxml/tmp-preferences') |
    Set-Content -Encoding utf8 ./tmp-preferences/cmd/preferences/main.go
go build -o ./tmp-preferences/preferences-entry.exe ./tmp-preferences/cmd/preferences   # 只构建窗口入口
```

> 生成器需要真实 Go package，因此**先复制 ViewModel 再生成**；窗口入口反过来要在生成**之后**安装。

与前面几份一样：**只生成和构建，不打开窗口**。

## 每一步在保证什么

| 写法 | 约束 | 来源 |
|---|---|---|
| 两个 `<Input>` 各自 `Value` + 各自 `ValueChanged` | 方向是 **vm → Editor** 单向；两份草稿互不串写 | `Input输出契约.md` §3.1/§3.2 |
| `MaxLen="12"` | Gio 内置按 **rune** 截断（中文与 ASCII 同口径）；被截断的字符**不产生** `ValueChanged` | `Input输出契约.md` §9 场景 7；Gio `Editor.MaxLen` |
| `Filter="0123456789"` | Gio 内置过滤；被过滤掉的字符**不产生** `ValueChanged` | `Input输出契约.md` §9 场景 8；Gio `Editor.Filter` |
| `ReadOnly="true"` 的确认框 | 只读框**也接了** `ValueChanged`，但它只计数不改状态；真实 `EditEvent` 在只读框上被丢弃 | Gio `Editor.Update` 的 `key.EditEvent` 分支 |
| 名称框 `Submit="OnNameSubmit"`、端口框**不声明** | 回车提交是**事件声明的派生效果**；事件接线按控件各自独立 | `Input输出契约.md` §2.3/§4.3 |
| `Submit` 的处理器与 `Click` 的处理器**分成两个方法** | `Submit` 带一个 text 载荷、`Click` 不带，同签名无法两用（实测 `GXML-EVENT-004`） | `事件处理输出契约.md` |
| 脏标记是**派生量** `Name != SavedName \|\| Port != SavedPort` | 多一个独立变量就多一处忘记清空的路径 | 与 workbench 同源的实测结论 |
| 显示串在 `refresh()` 里算好 | 不在 Layout 热路径每帧拼接 | `性能预算与优化策略.md`、`AGENT.md` §2.5 |
| 回写是**原样回写**（不 Trim / 不转大写） | 规范化本身幂等，但当前同步算法下会让编辑器显示与模型分歧；不把未验证的规范化写法藏进示例 | `docs/05-开发/2026-11-02-D3-输入同步有界实验观察.md` |
| 背景只用叶子静态色 `Background="#RRGGBB"` | 容器/Page 背景与 `ThemeRef` **不在**已实现范围 | `GXML语言规范.md` §Background 覆盖范围（2026-11-02 裁决 B） |

生成的 `preferences_gen.go`（**不要手改**）里有四处关键产物：`input0/1/2` 与 `applied0/1/2`（三个 Input 各自的"最近一次外部应用"）、`ed.MaxLen = 12` / `ed.Filter = "0123456789"` / `ed.ReadOnly = true`（静态属性透传）、`ed.Submit = true`（**只有**名称框声明了 `Submit`），以及两处分支守卫 `InReview` / `HasStatus`。

## 用户流程验证（自动）

```powershell
go test -v -run '^TestPreferencesProject$' ./examples/gxml
```

生成侧沿用与 forms / workbench 相同的判据：首次 `--no-cache` 出现产物与 Manifest、**缓存命中 mtime 不变**、`--no-cache` 同内容重写、合法修改产物变化、非法绑定 `{ReviewText}`→`{Missing}` 报 `GXML-BIND-001` 且保留旧产物与 Manifest；另加一条编译期负例 `<Input MaxLen="-1">` 被拒（`non-negative integer`）。

离屏语义测试写进临时 module 后 `go test` 实跑（真实 `input.Router` 的 pointer 事件、`key.EditEvent` 与分帧回车，不创建原生窗口），共 13 条：

1. **两字段独立**：改名称不动端口（`Port` 与 `PortChanges` 都不变），确认串随之重算并写回 `applied2`。
2. **MaxLen 按 rune 截断**：插入 16 个 ASCII 后草稿恰好 12 个 rune，编辑器与模型一致。
3. **Filter 过滤**：插入 `a` 被丢弃且**不产生** `ChangeEvent`；再插入 `9` 才回写（端口达到 5 位上限）。
4. **只读不改值**：只读确认框对真实 `EditEvent` 不改值（`ConfirmChanges` 基线 ≥ 1 证明接线存在，不是"压根没接"）。
5. **校验失败不误提交**：端口被推到 98080 后点「提交审阅」→ 停在 `review-reject`，`Reviews=0`、`Commits=0`、`InReview=false`、已保存值不变。
6. **全链路**：编辑 → 提交审阅 → **按当前几何重扫** → 确认提交 → 写回已保存值、退出审阅、不再脏。
7. **Reset 往返**：真实点击「重置」把两份草稿恢复成已保存值，编辑器随之同步（不是停在中间态）。
8. **脏标记四态逐字**：`无 → 有 → 无 → 有`，两态文案逐字比（都含"未保存"三字，子串判据永远不红）。
9. **回车提交**：载荷 = 回车瞬间的编辑器文本、次数取 `Submits`；**无回车**跑 3 帧的负对照。
10. **端口框回车无效果**：端口框未声明 `Submit`，`Editor.Submit` 为假，回车不进审阅。
11. **两实例隔离**：A 输入后 B 的编辑器文本、外部值与 `applied0` 都不变。
12. **nil ViewModel 拒绝**：构造期报错。
13. **小画布可达性（记录）**：640×520 与 480×360 上「提交审阅」「重置」都可达；360×300 只可达「提交审阅」；360×240 两者都不可达。**本工程没有滚动**，画布装不下就是点不到。

三条纪律：**坐标全部实测扫描**（不硬编码像素，状态变化后重扫）、**判据取增量**（外部值应用本身会带一次 `ChangeEvent`，调用次数 ≠ 按键次数）、**每个交互用例的主链路 ≤ 8 帧**（实测 3–7 帧）。

另有三条**可编译变异红**（只改 `vm.go` 副本，原字节按 SHA-256 复核未变）：`Reset` 不恢复名称 / 脏标记恒为不脏 / 提交不写 `SavedName`，对应的三条业务判据分别变红。

**本测试不证明**：原生窗口真的显示、真实键盘/鼠标链路、**IME 组合输入与物理焦点**（`Input输出契约.md` §9 场景 15 列为人工验收门槛），以及任何性能数据。窗口入口只 `go build`，从不执行。

## 运行入口（人工）

```powershell
go build -o ./tmp-preferences/preferences-entry.exe ./tmp-preferences/cmd/preferences
./tmp-preferences/preferences-entry.exe        # 只在你想真的看窗口时才跑
```

人工验收清单（自动化**没有**覆盖，不要当成已通过）：窗口能显示；点名称框真的获得焦点、光标闪烁；中文输入法组合输入过程中不被打断、上屏后名称包含完整文本（名称上限 12 个**字符**，中文按字符计数）；端口框里敲字母确实敲不进去；只读确认框点上去有光标但敲字无效；回车后进入审阅、点「确认提交」后提示串出现、「重置」后草稿回到已保存值。

## 常见错误

| 现象 | 原因 | 处理 |
|---|---|---|
| 敲一个字却看到处理器被调用两次（或基线不是 0） | 外部值应用调用 `SetText` 后 Gio 的 Changed 标记会让紧随的 `Update` 再产出一次 `ChangeEvent` | 处理器要幂等；断言取**增量**；不要用"调用次数"当作"用户敲了几个字" |
| 输入框里追加的字符出现在**开头** | 程序化 `SetText` 会把光标移到行首（`Editor.SetText` 行为），后续 `EditEvent` 从行首插入 | 这是既有行为记录；真实用户点击后光标位置由点击决定，本例未验证真实点击后的光标落点 |
| 端口框敲字母没反应 | `Filter="0123456789"` 生效，非法字符连 `ChangeEvent` 都不产生 | 符合预期；需要别的字符集就改 `Filter` |
| 输入 16 个字符只留下 12 个 | `MaxLen=12` 按 rune 截断 | 符合预期 |
| 「提交审阅」点了没进审阅 | 校验没过（名称空 / 端口空 / 端口含非数字 / 端口不在 1-65535） | 看提示串里的具体原因 |
| 提示"有未保存的修改"，点「重置」后消失了，但保存值没变 | 设计如此：只有「确认提交」才写已保存值，「重置」把草稿**拉回**已保存值 | 想写入就走 提交审阅 → 确认提交 |
| 用 `strings.Contains(文案, "未保存")` 判断脏标记 | 两态文案都含"未保存"三字，这个判据**永远不红** | 逐字断言两态文案，或直接断言 `Dirty` 布尔值 |
| `Submit` 与 `Click` 想共用一个方法，报 `GXML-EVENT-004` | `Submit` 带一个 text 载荷、`Click` 不带，签名不同 | 拆成两个方法，共用一段内部实现 |
| 页面四周没有内边距 / 容器背景没生效 | 容器/Page 背景与 `ThemeRef` 不在本轮已实现范围 | 叶子（`Text`/`Button`/`Input`）的 `Padding` 与静态 `Background` 已生效 |

## 一键验证脚本（用户直接运行）

上面每一步都要手敲命令，容易漏掉"先复制 ViewModel 再生成"这类前置条件。`scripts/verify-examples.ps1` 把五个入门工程（starter、tasks、forms、workbench、preferences）的完整流程固化成一条命令：从任意当前目录都能调用，脚本按自身位置定位仓库根。示例名只在脚本里**注册一处**（`$known`），`all` 的列表与 `-Example` 的白名单共用它，不会出现"-Example X 可用但 all 漏掉"。

```powershell
# 默认：starter + tasks + forms + workbench，隔离模块，输出到 <仓库>/tmp/verify-examples/<时间戳>
pwsh -File scripts/verify-examples.ps1

# 单个示例 + 指定输出目录（支持空格与中文路径）
scripts\verify-examples.ps1 -Example tasks -OutputRoot "D:\我的 构建\验证"

# 只验证表单工程（本夜各宿主都显式传 -LockPath 共用公共锁）
scripts\verify-examples.ps1 -Example forms -LockPath "D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock"

# 只验证列表工作台工程
scripts\verify-examples.ps1 -Example workbench -LockPath "D:\Codes\Goxml-night-reports\2026-10-27\heavy.lock"

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

> 测试**自己重定向**子进程输出时会遇到**混合编码**：脚本自身的 `Write-Host` 是控制台代码页（简体中文环境 cp936），而 `go` / `goxml` 子进程的原样输出是 UTF-8，同一个缓冲区里两种编码并存。脚本正常运行时**不重定向**（输出直通控制台），因此这不是脚本的行为差异；写测试时请按行容错解码，不要假设单一编码。若判据要检索日志里的**中文串**（如"已取得锁"），请在**字节层面**比对两种编码的目标串——按某一种编码 decode 后再检索会漏判（本夜 workbench 双宿主测量就踩过一次，判据修正后按已留档的原始字节重算，未重跑构建）。

### workbench 示例的双宿主实测（2026-11-02）

`-Example workbench` 在两个宿主上**各跑一次**真实生成 + 入口构建（不重跑旧全矩阵），均显式 `-LockPath` 指向公共 heavy.lock：

| 宿主 | 实测版本 | rc | `workbench_gen.go` | 窗口入口 |
|---|---|---|---|---|
| Windows PowerShell 5.1 | `5.1.26100.9711` Desktop | 0 | 11541 字节 | `workbench-entry.exe` 12005376 字节 |
| PowerShell 7 | `7.6.9` Core | 0 | 11541 字节 | `workbench-entry.exe` 12005376 字节 |

（切片 C4 给页面加了计数与脏标记两个派生字段后**重测**过一遍：产物从 11056 / 12001792 变为 11541 / 12005376。材料一变就重测，避免文档里的数字失真。）

两次运行的日志里都能读到"已取得锁: `D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock`，run id …"与"已释放自己的锁（token …）"，两个 run id 不同、token 不同。

选择器专项（`scripts/test-verify-examples-workbench.py`，持公共锁 + `-SkipLock`，判据见脚本头）5/5 PASS：`-Example workbench` 只出现 workbench 子目录；`-Example all` 四个示例齐全且各有生成物与入口产物（`forms_gen.go` 7212 字节与上一轮 forms 实测一致，可作跨轮一致性旁证）；`-Example bogus` rc 2 且列出 workbench；源材料缺 `vm.go` → rc 7；`-Example all` 与 `-SourceDir` 同用 → rc 2（既有规则未被破坏）。

### preferences 示例的双宿主实测（2026-11-02）

`-Example preferences` 在两个宿主上**各跑一次**（不重跑旧全矩阵），均显式 `-LockPath` 指向公共 heavy.lock：

| 宿主 | 实测版本 | rc | `preferences_gen.go` | 窗口入口 |
|---|---|---|---|---|
| Windows PowerShell 5.1 | `5.1.26100.9711` Desktop | 0 | 17889 字节 | `preferences-entry.exe` 12024320 字节 |
| PowerShell 7 | `7.6.9` Core | 0 | 17889 字节 | `preferences-entry.exe` 12024320 字节 |

选择器专项（`scripts/test-verify-examples-preferences.py`，持公共锁 + `-SkipLock`，判据见脚本头）5/5 PASS：`-Example preferences` 只出现 preferences 子目录；`-Example all` 五个子目录齐全且各有生成物与入口产物；`-Example bogus` rc 2 且提示"未知示例 … starter / tasks / forms / workbench / preferences / all"；源材料缺 `vm.go` → rc 7；`-Example all` 与 `-SourceDir` 同用 → rc 2（既有规则未被破坏）。

`all` 五示例**只在最终组合阶段、在 PS 5.1 上验证一次**（不在两宿主重复全矩阵），产物字节：`home_gen.go` 5719、`tasks_gen.go` 10136、`forms_gen.go` 7429、`workbench_gen.go` 11807、`preferences_gen.go` 17889。

> 同轮测到的 `forms_gen.go` 7429 / `workbench_gen.go` 11807 与本页前文记录的 7212 / 11541 **不同**：前文数字是**另一棵更早的树**上的测量，跨树比较字节没有意义；看差异要先对齐基线。

## C线冻结时的组合验收清单（历史记录）

上面的数字都是**在本树**（基线 `1e210c4` + 本线提交）上实测的，不能直接当成合并树（B→A→C）的结论：

| 项 | 本树状态 | 组合后谁复验 |
|---|---|---|
| G2 缓存命中（字节相同**且 mtime 不变**） | 通过（`TestWorkbenchProject` 的 G2） | B 线改的是缓存键（`internal/cli/`），合入后必须重跑 |
| G3 `--no-cache` 强制生成（字节相同） | 通过（G3） | 同上 |
| G5 生成失败保留旧产物与 Manifest 字节 | 通过（G5 用例） | 同上 |
| `Padding` 真的生效 | **本基线不生效**（Codegen 忽略，`size_runtime_test.go` 已注明属 TASK-CODEGEN-001-21 范围） | A 线落地后重跑；在那之前本树的留白与溢出**不代表**组合树 |
| 四示例选择器 | W1–W5 / F1–F5 通过（本树） | 用 `--repo <组合树>` 指向组合树重跑，判据不改 |
| 小画布可达控件 / 溢出 | 仅**记录**（见 `docs/05-开发/dev-log/2026-11-02-r5-followup-workbuddy.md` 片 3），不是通过项 | 同上 |

选择器测试接受任意树路径，因此组合树**不需要改脚本**：

```text
python scripts/test-verify-examples-workbench.py --repo <组合树> --out <目录> --lock-path <公共锁>
```

原C线交付时不填写组合SHA；随后协调者出具的组合身份见下方R5组合说明。本线只提供"能指向任意树"的入口，
以及在本树上实测过的判据。示例名的单一来源是 `.ps1` 里的 `$known`，`TestExamplesSelectorSingleSource`
会把"注册表 ↔ `testdata/` 目录"的集合差和"示例名在脚本里重复出现"都判红。

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

### 2026-11-02 R5 组合后的说明

本页C线历史测量基线尚未包含Padding实现；接纳组合已接通容器和叶子Padding，旧画布高度/坐标只保留为当时记录。最终组合用实际layout重新测量、复验事件流和缓存G2/G3/G4；具体结果见[R5组合验证](../../docs/06-测试/2026-11-02-R5组合验证.md)。360×240等小画布仍可能溢出，本工程没有新增滚动，不能把能编译或部分控件可点读成所有画布可用。Background仍隔离，Input回调语义未改。

编号以TestWorkbenchProject源码为准：G2缓存命中（含mtime）、G3强制重生成、G4合法输入变更、G5非法绑定失败保留旧生成物和Manifest。原清单把最后一项标题记为G4，已更正；组合包四项均实际执行。

组合实测补充：同探针内容高度430；360×240可达4/7（缺新增与第3行选择/删除）、480×360可达6/7（缺新增）、720×480可达7/7。无滚动，建议先使用能容纳页面的画布。两个选择器在组合树均5/5，all包含四示例，只编译入口。原始测量条件与边界见上述R5组合验证。
