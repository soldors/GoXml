# 2026-11-05 R20-B Router B2 生产关闭接线——设计与断点（ZCode）

实验起点 `02483e78457c41d9893656d55c9fda3425e464f5`；树
`C:/Users/admin/.codex/worktrees/product-r20-zcode/Goxml`（分支 codex/product-r20-zcode，clean）。
前置输入：R18 router handoff、R19 state.json、B2 任务卡草案（均已读）。
**本轮为阶段 1（只读盘点+可审查设计）；生产改动保持等待（判定见下）。**

## 谱系与固定候选状态（实测）

02483e7 ← 966b2d2 ← 4f6a6bb ← 617814f（R17 candidateRef）← 6310573（R17 readyRef）；
即已含：R16 全部验收、F-01/05/07/08 清理、**已批准的显式关闭锁序**（shutdown 与
导航提交共用状态锁；关闭先获锁→在途工厂结果只计一次丢弃、保留当前页与最近错误；
工厂调用与失效通知在锁外）与 F-06 方案 A（app 导入仅外部测试包 + 原生标签护栏）。
本轮实测：router 全套 rc=0；默认选择器 `XTest:[host_windowless_test.go]`
（无窗口宿主用例在外部包），integration_test.go 仅显式标签纳入 ✓。

## 门槛逐项核对（G1–G4）

| 门槛 | 状态 | 证据 |
|---|---|---|
| G1 负责人批准 B2 卡 | **部分**：锁顺序+隔离关闭语义已批准（R18 协调者收口）；**公开 API 与生产 App 接线未批准**（同文件：「待 B1 门槛及后续授权」；state.json `appWiring: pending-b1-acceptance`） | product-r18/coordinator/resume.md §Router、state.json |
| G2 F-06 裁决 | **✅ 方案 A 已批准并落地**（f3cf375 护栏 + 02483e7 外部包迁移） | R18 handoff、本轮 go list 实测 |
| G3 app.go 可用 | **✅ 实质满足**：主仓 `git status -- pkg/runtime/app/` 仅 testdata/concurrencyprobe/main.go 修改，**app.go 本身干净**（93 项 WIP 中的 app.go 已并入 b261551 谱系） | 本轮只读实测 |
| G4 B1 P3 清单确认 | **实质满足**：F-01/05/07/08 已在 617814f 关闭（candidateRef 谱系），F-02/03 在 R16 关闭；正式签批记录未见 | 谱系核对 |

**阻断项（生产改动等待的依据）**：① B1 正式验收未完成——负责人明确保留原生视觉
与 race 两个条件（WorkBuddy 已在 R19 备好 router-visual.exe 与
router-native-race.test.exe 及人工步骤，**已构建、待桌面执行**）；
② B2 公开 API 与生产接线授权未下达（三处记录一致）。
按本轮任务书「B1 验收、B2 卡及公开接口裁决、App 所有权条件未齐时，交设计和
断点，生产改动保持等待」——**阶段 2/3 不开工**。

## App 文件归属盘点

- 主仓 master=b261551、索引空；`pkg/runtime/app/` 仅 testdata 探针在制（非 app.go）。
- B2-2 将改 `pkg/runtime/app/app.go`（仅两处加法）——与主仓无 WIP 冲突；与字体线
  （theme/testdata）零交集。
- 02483e7 谱系的 app.go = b261551 提交态；R19 已对其跑过无窗口 race（60 用例 pass）。

## B2-2 设计（待授权后执行；本节为可审查设计稿）

### 关闭 API 签名

- Router 侧：**导出既有已批准实现**——`func (r *Router) Shutdown()`，零新语义
  （02483e7 的 `shutdown()` 已含批准的锁序与幂等；导出=改签名可见性）。
- App 侧：`func (a *App) OnDestroy(fn func()) error`；**回调注册与重复注册策略
  （建议）**：首次设置生效、重复设置返回错误——与 `SetInvalidator` 的防换窗先例
  同族；**触发次数**：每次进程运行**恰好一次**（`sync.Once` 语义），在收到
  `app.DestroyEvent` 的那个事件循环内、退出返回之前、**锁外**调用（项目惯例：
  锁内不调用户代码）；nil fn 拒绝。

### 两个事件循环的接线位置（精确）

1. `App.loop` —— app.go:378 `case app.DestroyEvent:` 分支，在 `return` 前插入
   `a.fireDestroy()`。
2. `RunWindowWithInvalidator` 内联循环 —— app.go:558 `case app.DestroyEvent:`
   分支，在 `return nil` 前插入回调触发。

**RunWindow** 委托 RunWindowWithInvalidator（app.go:590-592）→ 单点接线即覆盖
其全部调用方。`watchWindowStop`（app.go:568-583）在 stop 信号下
`w.Perform(system.ActionClose)` → 产生**真实 DestroyEvent** → 回调经同一分支
触发（`close(stop)` 因此可作 B2-3 的受控触发器 ✓）。

**回调到达内联循环的通道（决策点 D-A，建议）**：`Config` 增 `OnDestroy func()`
（加法字段，双形态共用：NewApp 存之、RunWindowWithInvalidator 从 cfg 读之）——
零签名破坏、两个循环各一行触发。备选：新增导出变体函数（破坏面更大，不推荐）。

### 入口兼容性

- 既有调用方签名零变化（Config 加法字段；回调 nil 时行为与现状逐字节一致）。
- 事件循环退出与错误行为不变：两分支本就 return；回调 panic 无 recover
  （与项目「工厂 panic 无 recover」惯例一致，design 不引入新 recover）。
- `stop` 信号触发路径：stop → ActionClose → DestroyEvent → 回调 → 循环退出
  ——与现状唯一差异是**多了一次回调调用**，退出时序不变。

### 红→绿测试矩阵（受控 DestroyEvent）

| 用例 | 形态 | 绿（接线在） | 红（变异） | 执行条件 |
|---|---|---|---|---|
| T-G1 App.loop 形态 | NewApp+Run + 真实窗口关闭（或 cfg 探针） | root/页面 ctx 级联取消、Navigate/Dispatch 拒绝+计数、DroppedCount 递增 | 移除 app.go:378 接线 → 本用例红 | 原生标签+授权（App.loop 无 stop 参数，自动化触发 DestroyEvent 需 D-B 决策：cfg 内部探针或人工关闭） |
| T-G2 RunWindowWithInvalidator 形态 | close(stop) → ActionClose → DestroyEvent | 同上 + 在途工厂不发布（关闭先获锁语义）+ 重复 Shutdown 幂等 + 最近错误保留 | 移除 app.go:558 接线 → 本用例红 | 原生标签+授权 |
| T-G3 双实例 | 两个窗口/两个 Router | 互不串写 | — | 原生标签+授权 |

**受控 DestroyEvent 反例设计（阶段 3，待授权执行）**：T-G1/T-G2 各配「移除本
循环接线」的编译变异——任一事件循环漏接线，其对应用例在 DestroyEvent 后断言
rootCtx 未取消而翻红；两个用例分别锚定两条接线，缺一即红。无窗口等价物不存在
（App.loop/内联循环均消费真实 `w.Event()`，Gio 无注入事件源）——这是本矩阵
必须原生标签+授权的原因，明确标注。

### 断点（恢复条件）

授权齐备（① 负责人执行 r19 两个桌面验收程序并记录；② 下达 B2 公开 API+生产
接线授权；③ D-A/D-B 两决策点裁定）后，从本树 `codex/product-r20-zcode`
（02483e7）开工：B2-2 最小实现 → T-G1/T-G2/T-G3 原生红绿 → 无窗口 race 复跑
（Router+App）→ readyRef 冻结。预计改动面：router.go 导出 + app.go 两处一行 +
Config 一字段 + 外部测试包一个原生测试文件。

## race 与原生状态（继承 R19）

- 无窗口 race：R19 已用 zig/MinGW 工具链跑过 Router 与 App（state.json：两线
  pass，60 用例）——本轮未重跑（代码未变）。
- 原生 race 与视觉：二进制已构建、**awaiting-desktop-run**（r19
  router-manual-acceptance.md 给出精确 PowerShell 命令）；本轮 PENDING。
