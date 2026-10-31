# 2026-10-31 R2 批次 · WorkBuddy 线开发日志

- 作者：WorkBuddy（示例 / 文档 / 事实核对）
- 工作树：`C:\Users\admin\.codex\worktrees\product-r2-workbuddy\Goxml`，分支 `codex/product-r2-workbuddy-20261031`
- 基底：`557346e2e81494b6ff888382fb596fef9dc3bad5`（开工时 clean，实测一致）
- 外部交付：`D:\Codes\Goxml-agent-rounds\2026-10-31\product-batch-r2\workbuddy`

本线范围：`examples/gxml/**` 与 `docs/**`。**未**修改生产源码、全局 AGENT、依赖；**未**动其它三线作者的 dev-log。

---

## 切片 1（已提交 `473d8f5bf47a2adb60508d76ae99e3718b3c1633`）

**做了什么**：新增 `examples/gxml/testdata/tasks/`（vm.go / tasks.gxml / main.go.txt）与 `examples/gxml/tasks_test.go`，并把教程写进 `examples/gxml/README.md`。三者共用同一份源材料。

覆盖的已批准语法：`<For Each In Index Key>`、`<If>/<Then>/<Else>`（页级与行内各一处）、带参行事件（`Toggle(task.ID)` 用行内绑定、`RemoveAt(i)` 用 Index 变量）。

### 实测命令与 rc

| 命令（cwd = 工作树） | rc | 日志 |
|---|---:|---|
| `go build -o <wb>/bin/goxml.exe ./cmd/goxml` | 0 | `raw/build-cli.stdout.meta.json`（4.177s） |
| `go test -mod=readonly -p 2 -count=1 -v -run '^TestTasksProject$' ./examples/gxml` | 0 | `raw/tasks-test.stdout`（PASS 16.11s） |
| `go test -mod=readonly -p 2 -count=1 -timeout 10m ./examples/gxml` | 0 | `raw/examples-pkg.stdout`（ok 22.230s，含既有 starter） |

重负载均经共享 `heavy.lock`（只持自己 token，`lockReleased: true`）。一次因进程被中断导致**自己的**锁泄漏，已按 token 核验后释放（见 R2-WORKBUDDY-02）。

### 用户流程验证（真实 CLI，非模拟）

`out/flow-probe.json`：

| 步骤 | 结果 |
|---|---|
| G1 首次生成 `--no-cache` | rc 0，产物 9909 B |
| G2 缓存命中（输入未变） | rc 0，**字节相同且 mtime 相同** —— 确实没重写文件 |
| G3 显式 `--no-cache` | rc 0，字节相同，mtime 变化（这条把"跳过"与"重写但同字节"区分开） |
| G4 合法修改（按钮文案 新增→添加） | rc 0，字节**变化**；还原后与首次逐字节一致 |
| G5 非法绑定 `{Summary}`→`{Missing}` | rc 2，`GXML-BIND-001`，旧 `tasks_gen.go` 与 Manifest 字节不变 |

### 离屏语义断言（不创建原生窗口）

`out/geom-probe.json` 给出实测命中区（560×400、`PxPerDp=1`、扫描步长 x10/y5）：

- `remove:1` y 84–120；`toggle:2` y 140–168 / x 456–492；`remove:2` y 136–172 / x 516–540；`toggle:3` y 188–220；`remove:3` y 188–224
- 空列表扫描不到任何行事件；**不存在** `toggle:1`（已完成行渲染文本而非"完成"按钮）

断言刻意**不硬编码像素坐标**：字体/主题会改变按钮尺寸，测试用实测网格定位命中区，再断言区间顺序与左右关系。

### 本切片不证明

原生窗口真的显示、真实鼠标点击链路、任何性能数据。含 `<For>` 的页面按 `For列表输出契约.md` §7 不享有零分配保证。

### 观察（交编译器线判断，未定性为缺陷）

生成的 `GoxmlTasksView` 含 `click1`、`click2` 两个 `widget.Clickable` 字段**未被任何代码引用**（实际使用 `click0`、`click3`），疑似普通按钮槽位计数器被行内按钮占用。不影响编译与行为（未使用的结构体字段合法），但会让"槽位数"口径失真。编号：`R2-WORKBUDDY-OBS-01`。

---

## 本线踩坑（R2 临时编号，不占全局号）

| ID | 标题 | 落点 |
|---|---|---|
| R2-WORKBUDDY-01 | shell 变量命名为 `TMP` 会污染环境变量，使 Go 忽略该目录的 go.mod | `docs/08-踩坑/踩坑-R2-WORKBUDDY-01-环境变量TMP被同名shell变量污染.md` |
| R2-WORKBUDDY-02 | 每次点击都重建 `material.NewTheme()` 使扫描超时并被中断，连带泄漏自己的锁 | `docs/08-踩坑/踩坑-R2-WORKBUDDY-02-重循环内重建主题导致超时与锁泄漏.md` |

## 与旧交付的关系

- 未重跑、未追写 R2 之前的任何 DONE 或旧日志。
- Image 候选（`C:/Users/admin/.codex/worktrees/parallel-cli/Goxml`）本批只读、不合入、不复制；对 `TASK-IMAGE-001` 旧声明只出**外部修正稿**（见交付目录 `candidate-docs/`），不修改冻结候选、不把 ADR-0015 追认为批准。
- 正式性能预算、Router B1/B2、真实 M1 均按既有证据维持状态，未升级验收。
