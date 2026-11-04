# 2026-11-04 R15-C CROSS-PACKAGE-TASKBOARD（WorkBuddy）dev-log

基线 `90f62dc0df611598b67dc6c7896f99fde00e0d0d`；树 `product-r15-workbuddy`，分支 `codex/product-r15-workbuddy-20261104`。
本轮只新增一个 testprojects 工程、它的同源验收与教程，外加 `scripts/verify-projects.py` 的必要接入；
未改 CLI/Codegen/锁助手，未改五个内置示例与 `examples/gxml/testdata`，未新增内置示例注册项。

三个代码提交（本地，未 push）+ 本 dev-log 自身为第四个提交：

| # | 提交 | 内容 |
|---|---|---|
| 1 | `8e71dc9` | `examples/gxml/testprojects/crosspackage-taskboard/`（9 文件：model / 两视图 / 入口 / go.mod / README） |
| 2 | `a1a3bc6` | `scripts/verify-projects.py`：PROJECTS 登记 + `test_markers`/`test_verdict` 参数化 + exe 名派生 |
| 3 | `7c8ee53` | `examples/gxml/taskboard_project_test.go`：同源验收（缓存命中 / 坏绑定保旧 / 三条变异） |
| 4 | 本文件 | `docs/05-开发/dev-log/2026-11-04-r15-workbuddy-crosspackage-taskboard.md` |

## 1. 工程材料的形状（为什么必须再开一个工程）

crosspackage-preferences 已经证过「状态在独立 model 包、两个生成视图在另外两个包」。
它没证过两件事，而这两件事恰好是 `<For>` 的语义核心：

1. **行身份**：`<For Key="{task.ID}">` 生成的是按**业务键**索引的行状态池
   （`rowClicks0/1 map[int]*widget.Clickable`），重排/删除之后点同一坐标必须命中**当前**那一行，
   被删行的键必须被回收。按位置索引的实现在重排后「键集合仍是 3 个」但语义已串位，
   只看键集合大小是查不出来的。
2. **共享实例**：一份 `*model.Board` 交给两个生成视图，列表里 `Toggle` 之后摘要页
   **渲染出来的文本**下一帧就变（`slots[].text`），不靠任何事件总线或显式同步。

材料结构（`examples/gxml/testprojects/crosspackage-taskboard/`）：

```
model/board.go.txt          Task{ID,Title,Done,Status} + Board + refresh/Toggle/Remove/Reorder
view/list/list.gxml         For + Key="{task.ID}"；行内 切换/删除，实参 Click="Toggle(task.ID)"
view/summary/summary.gxml   只读，绑定 {TotalText} {DoneText} {SummaryText}
view/*/doc.go.txt           生成前必须有 package 声明（输出契约 §3.2）
view/*/sem_test.go.txt      6 条离屏用例（真实 input.Router 事件）
cmd/taskboard/main.go.txt   NewBoard() 只出现一次，两个视图共用 m
go.mod.txt                  gioui.org v0.10.3 + replace => __GOXML_DIR__
```

只用已批准语法：`Column/Row/Text/Button/For(Each,In,Key)`；不引入 `If`/`Input`
（`<Input>` 在 `<For>` 内会报 `GXML-SCOPE-005`），行内按钮缺 `Key` 会报 `GXML-SCOPE-003`。

## 2. 接入：两处硬编码，只参数化不改口径

`scripts/verify-projects.py`：

- `PROJECTS['crosspackage-taskboard']`：`entry=cmd/taskboard`、`testTargets=['./view/...']`、
  `testRun=^TestTaskboard`、`completionMarker=TASKBOARD-DONE`、6 个 `expectedTests`。
- `crosspackage-preferences` 补上同形的 `completionMarker='CROSS-DONE'` 与 11 个 `expectedTests`
  （**仅显式化**，判定口径逐字不变）。
- `test_markers(raw, completion_marker='CROSS-DONE')`：正则用 `re.escape(marker)` 构造；
- `test_verdict(markers, spec=None)`：`spec` 省略时**退回**原来的 11 个 `TestCross*` 硬编码集合，
  既有调用方零改动；
- 入口 exe 名按入口包名派生：`preferences` → `preferences.exe`（旧证据路径不变）、
  `taskboard` → `taskboard.exe`。

`PROJECTS` 白名单与 `examples/gxml/testdata` 的五个 `-Example` 注册项是**两套独立登记**，
新工程只进前者——这就是「保留现有五个内置示例」的具体落点。

## 3. 真实 CLI 批次生成 + 构建入口（原始日志 `work/proto4/.verify-projects/`）

`python scripts/verify-projects.py crosspackage-taskboard --out-dir <work/proto4> --repo <worktree> --check --json reports/proto4.json`
→ `verdict ok`，5 个阶段全 rc=0，`elapsedSec=11.695`。

`generate:view` 阶段的原始 stdout（**一次目录 generate 覆盖两个包**）：

```
缓存未命中 view/list/list_gen.go：cache-missing
缓存未命中 view/summary/summary_gen.go：cache-missing
提交前检查通过：2 个目标包
已生成 view/list/list_gen.go
已生成 view/summary/summary_gen.go
共 2 个文件生成成功（按路径排序处理）
```

阶段字段：`checkReported=True`、`checkTargets=2`、`checkFailureObserved=False`。
`test` 阶段：`pass=6 fail=0 skip=0 noTestFiles=0 completed=6`（6 个 `TASKBOARD-DONE` 全齐）。
`build-entry` rc=0；入口产物 11886080 字节，**只构建、从未执行**。

## 4. 离屏验收：四项判据 + 扫描缺陷的实证

坐标一律实测扫描（不硬编码像素），每个状态各自扫、每个探针点开全新 harness。
`accept-3` 原始日志里的三组坐标（同一台机、同一次运行）：

| 状态 | 实测接线 | 说明 |
|---|---|---|
| initial | `toggle:3=[512 208]`、`remove:1=[576 104]`、`reorder=[24 52]` | 7 个接线 |
| reordered | `toggle:3=[512 104]`、`toggle:1=[512 208]`、`reorder=[24 52]` | ID3 换到首行；断言「必须换坐标」 |
| deleted-1 | `toggle:2=[512 104]`、`toggle:3=[512 156]`、`remove:2/3=[576 104/156]` | **只剩 5 个**接线，已删行不可达 |

### 4.1 已修的假命中（这条差点让判据变成永真）

第一版扫描器在 `prepare` 之后不清 `LastAction`，而 `prepare` 本身就是真实点击
（`Remove(1)` → `LastAction="remove:1"`）。于是扫到 `y=0` 的顶部边距（点不到任何按钮）时，
模型里残留的 `remove:1` 被记成「在 (24,0) 命中」：

```
修复前 deleted-1：remove:1=[24 0]   ← y=0 是 Column 顶部边距，不可能有按钮
修复后 deleted-1：remove:1 扫不到，只剩 toggle:2/3 + remove:2/3 + reorder
```

`reordered` 状态的 `重排=[24 0]` 同样是这个假命中，修复后是真实的 `[24 52]`。
修法：每次探针点击前 `h.m.LastAction = ""`，让「有值 ⟺ 这一击真的落在接线上」，
日志也改成按实测键逐个记账（不写死 `remove:1` 之类会被零值掩盖的键）。

### 4.2 另两处判据缺陷（同一轮修掉）

- 产物断言写死 gofmt 对齐：`rowClicks0 map[int]*widget.Clickable` 实际是
  `rowClicks0    map[int]*widget.Clickable`（多空格）→ 改成断言
  `map[int]*widget.Clickable`、`if c := v.rowClicks0[rowItem.ID]; c != nil {`、
  `delete(v.rowClicks0, k)`，判据落在契约而不是格式化结果。
- 入口静态判据按字节 `Count`，把注释里那句「model.NewBoard() 只调用一次」也数了进去
  → 假红（报 2 次）。改成**只数代码行**（跳过 `//` 行），并加守卫用例
  `TestTaskboardBoardCreationsGuard` 证明「注释不算、真第二份算」。

## 5. 同源验收（`examples/gxml/taskboard_project_test.go`）

`go test -run ^TestTaskboard ./examples/gxml` → PASS，`19.65s`（`accept-3`，带公共锁）。
材料与教程同源：读的就是 `testprojects/crosspackage-taskboard`，没有第二份 fixture；
判据是同一次运行内的 7 个模板文件指纹 before/after。

| 步骤 | 观测 |
|---|---|
| 批次 generate --check | `提交前检查通过：2 个目标包`；两产物 sha256 `3ea656…`(11221B) / `32bd52…`(8801B) |
| 跨包结构 | 两产物都 `import "<module>/model"`，package 子句为 `list`/`summary`（非 model） |
| 真缓存命中 | 两视图均命中、字节与 mtime 哨兵均未变、Manifest 468 字节未变 |
| 坏绑定保旧 | `summary.gxml:11:13: GXML-BIND-001: Value: "MissingField" is neither a loop variable nor a field of model.Board`；批次全部放弃，产物/Manifest 零变更 |
| 恢复 | 重新命中，字节与首次逐字一致 |
| 入口 | 只构建（11886080 字节），结构判据 `NewBoard()` 代码行 = 1 |
| 离屏 | 6 用例 + 6 个 `TASKBOARD-DONE` |
| M1 行事件按位置归属 | `TestTaskboardListReorderHitsCurrentRow` 变红（可编译） |
| M2 切换后不重算派生串 | `TestTaskboardListSummaryFollowsSharedModel` 变红（可编译） |
| M3 入口两份模型 | 结构判据 1→2 变红，且变异副本 `go build ./cmd/taskboard` 通过 |

M1/M2 都不许出现 `[build failed]`：编译失败说明不了判据有判别力，只能说明改名改崩了。

## 6. 回归

- `python scripts/test-verify-projects.py --work-root <dir>` → 20/20 `passed`，
  含 `full-pipeline`、`test-verdict-unit`、`gomod-parses`、`lock-busy-no-go`。
- `crosspackage-preferences` 全流水线（真实 CLI+11 用例）→ `verdict ok`，
  `pass=11 skip=0 fail=0 noTestFiles=0`，11 个 `CROSS-DONE` 全齐。**接入改动没动到它。**

## 7. 边界与未验证

- 窗口入口**只构建不运行**；原生窗口、IME、真实字体渲染是人工验收项，本轮未做。
- 本轮所有耗时/坐标都在本机 Windows 上测得，**不得外推**到 Linux/macOS（`CGO_ENABLED=1` 环境未跑）。
- 未 push、未合 master；分支远端状态本轮未核对。
- `For` 列表**不享有**静态页面的零分配保证（`INV-PERF-2` 限静态页面），本工程未做分配计数。
