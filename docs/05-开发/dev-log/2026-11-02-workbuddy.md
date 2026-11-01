# 2026-11-02 R4-C 开发日志（WorkBuddy：可运行 Input 表单入门工程）

工作树 `C:\Users\admin\.codex\worktrees\dev-20261102-workbuddy\Goxml`，分支
`codex/dev-20261102-workbuddy`，基底 `a934ebe18f857467be17bc1d40f602413422d321`
（clean；已含 portable 锁/marker 修复，本次**不重做**）。

## 切片 1：forms 工程 + 离屏输入事件验收

新增材料 `examples/gxml/testdata/forms/`：`forms.gxml`、`vm.go`、`main.go.txt`
（教程 / 自动测试 / PowerShell 验证共用同一份）。只用已批准控件
`Input`/`Text`/`Button`/`If`；`<Input>` 在 `<For>` 之外；显示串在事件处理器
准备（`previewOf` / `OnSubmit`），不在 Layout 拼接。

新增 `examples/gxml/forms_project_test.go`：真实 CLI 生成（`cli.Run`，含缓存命中
mtime 不变、合法修改产物变化、非法绑定 `GXML-BIND-001` 且旧产物保留），
独立 module 构建（复制本树 `go.mod`/`go.sum` + `replace`，`GOPROXY=off` 不联网
tidy），离屏语义测试作为源码写入临时 module 后 `go test` 实跑。

离屏验收走**输入事件链路**，不是直接调用 ViewModel 方法：

- 焦点用真实指针点击获得（网格早停扫描，缓存坐标后复用）；
- 文本用 `key.EditEvent` 经 `input.Router` 队列投递，与回车**不同帧**；
- 回车用 `key.Event{NameReturn}` Press/Release 成对投递。

判据与实测（全部 rc=0，raw 在交付目录 `raw/`）：

| 用例 | 实测 |
|---|---|
| 输入回写 | `editor="dabc初始文本"`、`vm.Text` 与编辑器一致、`applied0` 跟随（路径 2） |
| 回车提交 | 载荷 `"hi初始文本"`、`SubmitCount=1`、`Status` 由处理器准备 |
| 无回车负对照 | 后续 3 帧 `SubmitCount` 不变 |
| 清空往返 | `editor/vm.Text/applied0` 全部回到 `"初始文本"`，`SubmitCount` 不变（不重复提交） |
| 条件提示 | 未提交时全网格扫不到「隐藏提示」；`HasStatus=true` 命中于 (0,184)；点掉后同坐标不再命中 |
| 两实例隔离 | A 输入后 B 的 `Changes` 增量 0、编辑器与 `applied0` 均不变 |
| 编译期负例 | `<For>` 内 `<Input>` → rc=2 且 `GXML-SCOPE-005`，不留产物 |

## 实测发现（记录，不改冻结契约）

1. **外部值应用会额外带来一次 `ChangeEvent`**。Gio `widget.Editor.SetText` 内部
   `replace` 置位缓冲区 Changed 标记，`Update` 的 `processKey` 首行即
   `if e.text.Changed() { return ChangeEvent{}, true }`（`gioui.org@v0.10.3`
   `widget/editor.go:366`）。因此按《Input 输出契约》§5.3 的固定顺序
   （②SetText → ③Update → ⑤消费），**每一次外部应用都会调用一次
   `ValueChanged`**，首帧同步也不例外。本次用例的 `Changes` 判据一律取**增量**，
   并把该行为写进 `vm.go` 注释。若处理器的回写会改变文本（如大小写规整），
   存在同步—回写往复的风险：**本次未构造复现，未裁决，交协调者**。
2. **G4「合法修改产物变化」的补丁必须命中会进产物的位置**：`forms.gxml` 的 XML
   注释里也有「清空」二字，只替换裸词会改到注释（注释不进产物），判据空转。
   已改为精确替换 `Text="清空"`（自伤一次，已修）。
3. **`Padding` 仍被 Codegen 忽略**（已知缺口，`size_runtime_test.go:522` 已注明属
   TASK-CODEGEN-001-21 范围）。forms 材料保留 `Padding="24"` 与既有示例一致；
   离屏扫描从 (0,0) 起，不依赖 padding。
4. **反引号会截断 Go 原始字符串字面量**：`formsSemSource` 的注释里写了
   `` `if e.text.Changed()...` `` 导致 `expected ';', found 'if'`（自伤一次，已修）。

## 切片 2：接入一键验证脚本与文档

`scripts/verify-examples.ps1`：示例名收敛为**一处** `$known = @('starter','tasks','forms')`
（原先「all 列表」与「单示例白名单」是两份字面量，加示例只改一边就会出现
`-Example X` 可用、`-Example all` 却漏掉的漂移）；未知示例的提示文案由 `$known`
拼出。锁助手与通用启动机制**未改动**。

新增 `scripts/test-verify-examples-forms.py`（专用选择测试，持公共锁后以
`-SkipLock` 调用脚本）：F1 `-Example forms` 只出现 forms 子目录（rc 0，
`forms_gen.go` 7212 字节、入口 11976704 字节）；F2 `-Example all` 三个子目录齐全；
F3 `-Example bogus` rc 2 且提示"未知示例 … starter / tasks / forms / all"；
F4 源材料缺 `vm.go` rc 7；F5 `-Example all` + `-SourceDir` rc 2（既有规则未破坏）。
五项全 PASS。

双宿主各跑一次 `-Example forms`（显式 `-LockPath` 公共锁，日志回读
"已取得锁: D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock"）：
PowerShell `5.1.26100.9711` Desktop 与 `7.6.9` Core 均 rc 0，
`forms_gen.go` 7212 字节、入口 11976704 字节；未重跑旧全矩阵。

`examples/gxml/README.md`：新增「进阶：受控表单（`testdata/forms/`）」整节
（命令、每一步在保证什么、用户流程验证、自动验证、**运行入口（人工）**、
常见错误），并把一键脚本的说明从"两个入门工程"改为三个；宿主支持范围补
2026-11-02 的 forms 双宿主实测与混合编码提示。

自伤一次：选择测试的判据最初假设入口产物在示例子目录内、且生成物名为
`<示例>_gen.go`，两者都不对（入口在输出根层；starter 的页面是 `home.gxml`
→ `home_gen.go`）。改的是判据，不是工具。

## 提交

- 切片 1：`4e5adb30d7657978ec3410b6dcea87187663e471`（本树，未 push）
- 切片 2：`c3a3ecf74d1738272ed8032514badeb1ee0c2c58`（本树，未 push）

两次提交均 `GOXML_AUTO_PUSH=0` + 单次 `-c core.hooksPath=<空钩子目录>` +
逐个显式 `git add`（不用 `add .` / `-A`）。

## 回归与边界

- 旧 `TestStarterProject` / `TestTasksProject` 各跑一次：PASS（未重跑旧全矩阵）。
- 窗口入口只 `go build`（`forms-entry.exe`），**不执行**；0 原生窗口。
- IME / 物理焦点是**人工验收项**（契约 §9 场景 15），不写成自动通过。
- 所有 Go 负载持公共锁 `D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock`
  （锁助手经 importlib 从本仓 `scripts/verify-examples-lock.py` 加载），
  acquire → 跑 → `finally` 实测释放（raw `commands.jsonl` 有 token 与释放结果）。
- 编译器 / CLI / 运行时生产代码**只读**，本次未改。

## 非作者复核更正（2026-11-01，提前执行R4）

此前 OBS-1 提及“同步—回写往复风险”仅为未复现推测。独立读生成器发现处理器返回后立即令 A=重新求值V，下一帧 V==A 不会 SetText，故不能按原链路推导往复。README 已删“不是bug”的预先定性，保留外部应用通知的实测事实；Clear恢复初始值、不清除提交记录。未改Input契约。

选择器宿主补收口：原脚本取锁后若实验异常会越过释放，且释放失败仍可能返回0。此次finally覆盖整个判据区，锁释放/不存在均成为成功条件；独立注入launcher异常实证锁被释放，模拟release拒绝实证返回1。另加“输入但从未回车”的SubmitCount==0断言。
