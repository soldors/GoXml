# 跨包任务板工程模板

`model` 包持有**唯一一份**任务状态；`view/list` 用 `<For>` + 稳定 `Key` 渲染行，
每行两个按钮（切换完成态 / 删除）；`view/summary` 只读显示总数与完成数。
入口 `cmd/taskboard/main.go` **只调用一次** `model.NewBoard()`，把同一个指针交给
两个生成视图——列表里的改动下一帧就反映到摘要，不需要事件总线、不需要通知回调。

这是 `crosspackage-preferences` 的姊妹工程：那边演示双字段输入与校验，这边演示
**列表行身份**（重排 / 删除后的事件归属）与**跨包共享状态**。两个工程互不依赖，
`examples/gxml/testdata` 下的五个内置示例注册项也一个都没动。

## 目录图

```
crosspackage-taskboard/
├── README.md                    本教程（复制时不改写）
├── go.mod.txt                   模块模板（__MODULE__ / __GOXML_DIR__ 两个占位符）
├── model/board.go.txt           唯一状态所有者：Board / Task + Toggle/Remove/Reorder
├── view/list/
│   ├── doc.go.txt               生成前先给目录定包名（否则 CLI 拒绝生成）
│   ├── list.gxml                For + Key + 两个带参行事件 + 重排按钮
│   └── sem_test.go.txt          离屏：重排命中、删除不串位、行状态池、实例隔离
├── view/summary/
│   ├── doc.go.txt               同上
│   ├── summary.gxml             只绑定 TotalText / DoneText / SummaryText
│   └── sem_test.go.txt          离屏：渲染文本跟随共享模型、实例隔离
└── cmd/taskboard/main.go.txt    窗口入口：只构建、从不执行
```

复制时 `*.go.txt → *.go`、`go.mod.txt → go.mod`，仓库根 `go.sum` 一并复制，
离线构建不联网、不 `go mod tidy`。

## 一条命令

```bash
python scripts/verify-projects.py crosspackage-taskboard \
  --out-dir "<空目录>" --repo <仓库根> \
  --lock-path D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock --check
```

阶段顺序固定：`copy` → `build-cli` → `generate:view`（**一次**真实目录 generate
覆盖两页）→ `build-entry`（只构建）→ `test`（离屏 `./view/... -run ^TestTaskboard`）。
`--check` 原样转发给 generate；公共锁自持 token、在 `finally` 释放；原生窗口从不启动。

## 手动步骤（排障时看）

```bash
# 0) 准备 CLI（只做一次）
go build -o /tmp/goxml ./cmd/goxml

# 1) 复制模板、改名、替换 __MODULE__ / __GOXML_DIR__、补 go.sum
#    （或用 --copy-only 一步完成，材料与本教程同源）
python scripts/verify-projects.py crosspackage-taskboard \
  --out-dir /tmp/taskboard --repo <仓库根> --copy-only

# 2) 一次目录生成两个视图：发现根 = 输出根 = view
#    写成 --out-dir . 会把产物放到 ./list / ./summary（错误位置，且 rc 仍为 0）
/tmp/goxml generate view --out-dir view --check

# 3) 只构建窗口入口（不运行）
go build -o /tmp/taskboard.exe ./cmd/taskboard

# 4) 离屏语义测试（真实 input.Router 事件，无原生窗口）
go test -count=1 -v -timeout 20m ./view/... -run ^TestTaskboard
```

## 已批准语法（不引入新语法）

| 写法 | 约束 |
|---|---|
| `<For Each="task" In="{Items}" Key="{task.ID}">` | `Key` 必须存在且稳定可比较；行内 `<Button>` 缺 Key 会被 Parser 以 `GXML-SCOPE-003` 拒绝 |
| `Click="Toggle(task.ID)"` / `Click="Remove(task.ID)"` | 实参取**业务键**，不取行索引；重排后位置失效但 ID 不失效 |
| `<Text Value="{task.Status}" .../>` | 派生显示串由 `refresh()` 在事件处理器里算好，不在 Layout 里逐帧拼接 |
| `ViewModel="model.Board"` | 两页都跨包引用同一个 model 类型；产物分别落在 `package list` / `package summary` |
| `<Button Text="重排" Click="Reorder"/>` | 无参事件，用于打乱行序（[1,2,3] → [3,2,1]） |

本模板**不用** `<If>`、`<Input>`、`<Image>`、路由、`ThemeRef`：列表不需要条件分支，
也不需要行内编辑器。`<Input>` 放进 `<For>` 会被 `GXML-SCOPE-005` 拒绝。

## 为什么行状态按 Key 走

生成产物 `view/list/list_gen.go` 里有两个行状态池：

```go
rowClicks0 map[int]*widget.Clickable // 「切换」
rowClicks1 map[int]*widget.Clickable // 「删除」
```

键就是 `task.ID`。顶部轮询与渲染**同源同序**（For 列表输出契约 §5），
所以点击落在哪一行，由「当前帧的 ID → 池里的 Clickable」决定，而不是由「第几个按钮」决定。
离屏测试直接读这两个池的键集合，把它们当作**生成产物状态**来断言：

- 重排 [1,2,3] → [3,2,1] 后：键集合仍是 `{1,2,3}`（复用，不是重建三份状态）；
- 删除 ID 1 后：键集合变 `{2,3}`（被删键回收），且再也扫不到 `toggle:1` / `remove:1`。

## 离屏验收：按当前几何重新定位

控件尺寸随字体与主题变化，坐标一律**实测扫描**，不硬编码像素。
更关键的是：重排或删除会改变行高位置，所以每个状态各自扫一遍接线坐标，
不复用旧坐标。扫描时每个点都开一份全新 harness（模型 + 两个视图 + 一个
`input.Router`），避免上一次点击的残留污染判据。

| 用例 | 真实事件链 | 判据 |
|---|---|---|
| `TestTaskboardListReorderHitsCurrentRow` | 点「重排」→ 在当前几何下点首行「切换」 | `LastAction == "toggle:3"`、`LastTappedID == 3`、只改 ID 3、`rowClicks*` 仍为 `{1,2,3}`、坐标与初始态不同 |
| `TestTaskboardListDeleteKeepsIdentity` | 点 ID 1「删除」→ 在当前几何下点剩余第二行「切换」 | 剩余 IDs `[2,3]`、`LastAction == "toggle:3"`、ID 2 不变、`rowClicks*` 变 `{2,3}`、扫不到已删行接线 |
| `TestTaskboardListSummaryFollowsSharedModel` | 点 ID 2「切换」 | 共享模型 `Done` 1→2，摘要 `slots[].text` 出现「完成：2」且不再有「完成：1」；并断言 `summaryView.vm == board` |
| `TestTaskboardListInstancesIsolated` / `TestTaskboardSummaryInstancesIsolated` | 两个 harness 各自点按钮 | 一边的 Done / IDs / 摘要文本变了，另一边逐字不变 |

`TASKBOARD-DONE <用例名>` 是每条用例最后的完成标记；一键入口用它判「真的跑完了」，
拒绝 `SKIP`、`no test files` 或少了任意一条用例的假绿。

## 缓存、失败保旧与变异

- 同一批输入再跑一次 `generate view --out-dir view --check`：两页都命中，产物字节与
  mtime 不变、Manifest 不变，但 `--check` 仍执行（`提交前检查通过：2 个目标包`）。
- 把 `summary.gxml` 里的 `{DoneText}` 改成 `{MissingField}`：`GXML-BIND-001`，rc=2，
  两页旧产物、Manifest 与缓存**逐字保留**；恢复后重新命中。
- 同源验收带两条**可编译**变异：① `Toggle` 固定切换 `Items[0]`（事件归属破坏）→
  「重排后命中当前行」变红；② `Toggle` 去掉 `refresh()`（共享状态派生串不更新）→
  「列表修改同步反映到摘要」变红。两条都必须 `go build` rc=0，编译失败不算行为红。
- 入口源码里 `model.NewBoard()` 只出现一次；「改成两份模型」也是一条可编译变异，
  由同源验收的结构判据翻红。

## 边界

- 窗口入口**只构建不运行**，不创建原生窗口、不做采样、不安装依赖、不联网。
- 零像素级比对：用同帧真实文本的相等断言，不引入截图或单像素容差。
- `<For>` 子树不做静态子树固化，也不享有零堆分配保证（ADR-0006、性能预算 INV-PERF-2
  限静态页面）。
- 后台写入需要调用方自行同步：`Invalidate` 的并发安全不等于 `Board` 字段读写安全。
- Windows 之外未测；原生 IME、物理焦点、race 未测。
