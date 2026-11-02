# 2026-11-02 R7-C PREFERENCES（WorkBuddy 通道）

- 工作树：`C:/Users/admin/.codex/worktrees/night-r7-workbuddy-20261102/Goxml`
- 分支：`codex/night-r7-workbuddy-20261102`；基线 `4617f27`（他人提交，本线只作为起点引用；
  该提交的 dev-log 由原作者线负责，本线不代写其内容）
- 本线提交：`a6bf3ed`（片1）、`a1f44b3`（片2）、`83d588c`（片3）、以及仅更新本文档的收口提交
- 交付：`D:/Codes/Goxml-agent-rounds/2026-11-02/night-product-r7/workbuddy/`
- 五态：源码已提交 / 作者自测完成 / **非作者复核未进行** / **主仓未合入** / **远端未推送**

---

## 产品目标与范围

做一份"设置编辑 → 校验 → 审阅 → 提交/撤销"的**完整可复制示例**，覆盖已实现的双字段
`<Input>`、`MaxLen` / `Filter`、`ReadOnly`、条件提示与静态背景装饰。不是重复 forms / workbench，
也不依赖未批准的 Input 候选（`9fecd88` 在 R6 复核中是 BLOCK，隔离未批准，同根修补轮次已耗尽）。

只采用主干**已实现**能力：开工前先核对了 `internal/compiler/expander/schema.go` 与
`internal/compiler/codegen/input.go`，确认 `Value` / `Hint` / `ReadOnly` / `SingleLine` /
`MaxLen` / `Filter` 全部落地、`Enabled` 明确报错；背景按 2026-11-02 裁决 B 只用于
`Text` / `Button` / `Input` 的静态颜色。

---

## 片1：源材料与注册点（`a6bf3ed`）

新增 `examples/gxml/testdata/preferences/{preferences.gxml,vm.go,main.go.txt}`，教程、自动测试与
一键脚本共用同一份，不存在两份副本。

- `vm.go`：草稿 `Name` / `Port`、已保存值 `SavedName` / `SavedPort`、派生脏标记
  `Dirty` / `DirtyText`、只读确认串 `ConfirmText`、审阅信息 `ReviewText`、提示
  `Status` / `HasStatus`、分支开关 `InReview`；所有显示串在 `refresh()` 里算好，
  不在 Layout 逐帧拼接；回写是**原样/幂等**（不 Trim、不转大写）。
- `preferences.gxml`：三个 `<Input>`（名称 / 端口 / 只读确认）、`<Text>`、`<Button>`、
  `<If>/<Then>/<Else>`；没有 `<For>`、`Image`、`Router`、`ThemeRef`、`Expanded`。
- `main.go.txt`：窗口入口模板（只构建不运行）。

**注册点同步放在片1而不是片3**：往 `testdata/` 加第五个示例会让
`TestExamplesSelectorSingleSource`（`$known` ↔ `testdata` 互为全集）变红，若按原切片划分
单独提交片1，会留下一个**已知为红**的中间提交。为避免这种状态，把三处一致性改动
（`.ps1` 的 `$known`、`selector_single_source_test.go` 的 `want`、forms/workbench 两个选择器
的 `all` 预期集合）与材料一起提交。任务书把注册归入片3，此处顺序有调整，判据未改。

片1 实测：真实 CLI `generate --no-cache` rc 0；窗口入口 `go build` rc 0（12023296 字节，未运行）；
`TestExamplesSelectorSingleSource` 通过；`gofmt` 无差异。（片1 记录的 17752 字节在片2 补
`Submit` 后**作废**，最终值见下。）

---

## 片2：真生成 + 离屏事件闭环 + 变异红（`a1f44b3`）

### 源材料修订

名称框补 `Submit="OnNameSubmit"`。端口框**不**声明 `Submit`，用来对照"事件接线按控件各自独立"。

### 新增 `examples/gxml/preferences_project_test.go`

生成侧（与 forms / workbench 同构）：缓存真命中（字节相同**且 mtime 不变**）、
`--no-cache` 同内容重写、合法修改产物变化、非法绑定 `{ReviewText}`→`{Missing}` 报
`GXML-BIND-001` 且旧产物与 Manifest 字节保留；外加编译期负例 `<Input MaxLen="-1">`
被拒（`non-negative integer`）。

离屏侧写入临时 module 后 `go test` 实跑（真实 `input.Router`，不创建原生窗口）13 条 + 1 条测量：

| # | 用例 | 关键判据 | 实测观察 |
|---|---|---|---|
| 1 | 两字段独立 | 改名称后 `Port` / `PortChanges` 都不变（取增量） | `Name="abc默认名称"`、`Port="8080"`、`NameChanges 1→2`、`PortChanges 1` |
| 2 | MaxLen 按 rune 截断 | 插入 16 个 ASCII 后恰好 12 rune，编辑器 == 模型 | `Name="ABCDEFGH默认名称"`（12 rune） |
| 3 | Filter 过滤 | 插入 `a` 不产生 `ChangeEvent`，端口不变 | `Port` 仍 `"8080"`；再插 `9` → `"98080"` |
| 4 | 只读不改值 | 只读框对真实 `EditEvent` 不改值 | `ReadOnly=true`，`ConfirmChanges` 基线 1（接线证据）→ 输入后仍 1 |
| 5 | 校验失败不误提交 | `Reviews=0`、`Commits=0`、`InReview=false`、已保存值不变 | `Port="98080"` → `Status="校验未通过，未进入审阅：端口需在 1-65535 之间"` |
| 6 | 全链路 | 编辑 → 审阅 → **重扫** → 确认提交 → 写回已保存值 | `SavedName="新名称默认名称"`、`Commits=1`、退出审阅、不再脏 |
| 7 | Reset 往返 | 两份草稿恢复已保存值，编辑器随之同步 | `Resets=1`、`Name="默认名称"`、`editor="默认名称"` |
| 8 | 脏标记四态逐字 | `无 → 有 → 无 → 有` 四态文案**逐字**相等 | 见下"为什么必须逐字" |
| 9 | 回车提交 | 载荷 = 回车瞬间编辑器文本、`Submits` 次数；无回车负对照 | 载荷 `"x默认名称"`、`Submits=1`、`Reviews=1` |
| 10 | 端口框无 Submit | 端口框回车什么都不做 | `Submits=0`、`Reviews=0`、`Editor.Submit` 为假 |
| 11 | 两实例隔离 | A 输入后 B 的编辑器 / 外部值 / `applied0` 都不变 | B `editor="默认名称"` |
| 12 | nil ViewModel | 构造期拒绝 | 通过 |
| 13 | 小画布可达性（**记录**） | 只在推荐尺寸硬断言 | 640×520 与 480×360 全可达；360×300 缺「重置」；360×240 全不可达 |

三条纪律：

1. **坐标全部实测扫描**，不硬编码像素；状态变化（进审阅）后**按当前几何重扫**——
   用例 6 显式断言"进审阅后旧坐标不再命中 `review`"。
2. **判据取增量**：首帧外部值应用本身带一次 `ChangeEvent`（`SetText` 置位缓冲区 Changed
   标记 → 紧随的 `Update` 产出该事件）。**调用次数不是按键次数**。
3. **每个交互用例主链路 ≤ 8 帧**（实测 3–7 帧，写入日志）。

实测坐标（本树，`PxPerDp=1`）：焦点 `name=(28,92)`、`port=(28,128)`、`confirm=(28,168)`；
按钮 `提交审阅=(24,204)`、`确认提交=(24,244)`、`重置=(24,260)`。

### 三条可编译变异红

只改 `vm.go` 副本，原字节按 SHA-256 复核未变：

| 变异 | 改动 | 对应判据 | 结果 |
|---|---|---|---|
| M1 | `Reset` 里去掉 `m.Name = m.SavedName` | `TestPrefSemResetRestoresSaved` | 变红（输出含 `--- FAIL`，不是 `[build failed]`） |
| M2 | `refresh` 里脏文案恒为 `DirtyTextClean` | `TestPrefSemDirtyFourStatesVerbatim` | 变红 |
| M3 | `Commit` 里去掉 `m.SavedName = m.Name` | `TestPrefSemFullChainEditReviewCommit` | 变红 |

---

## 片3：工具、教程与收口（本提交）

- 新增 `scripts/test-verify-examples-preferences.py`（P1–P5），沿用既有 helper
  （`verify_examples_selcase.py` 的 `Runner`、锁助手、raw/退出码逻辑），
  **不重写**进程 / Job / raw / 锁那一层；业务判据先算，再与 `infraOk` 合成一次。
- `.ps1` 单一 `$known` 增列 `preferences`，`all` 从 4 个变 5 个（本任务明确预期）；
  forms / workbench 选择器与 `selector_single_source_test.go` 的预期集合同步。
- 教程写入 `examples/gxml/README.md` 的"进阶：设置编辑 → 校验 → 审阅 → 提交/撤销"一节。
- 双宿主各跑 `preferences` 一次：`all` 五示例只在最终组合阶段、在 PS 5.1 上跑一次
  （不在两宿主重复全矩阵）。
- 既有选择器复跑：forms F1–F5 与 workbench W1–W5 均 **5/5 PASS**（rc 0/0/2/7/2），
  `infraOk` 全 True、锁 release ok、`afterRelease=absent`。
  首次取锁时公共锁被他人持有（`agent=zcode`、`task=R7-B-final-check`），
  按纪律**不等待、不删他人锁**，锁释放后重跑。

---

## 实测数据（本树，基线 `4617f27` + 本线提交）

| 项 | 结果 |
|---|---|
| `preferences_gen.go` | 17889 字节，sha256 `9fa4ea3a1e82f55f8c29f6da369f6e4d95c7a59eec3ac2808fdd2d1dd0f2de80` |
| 窗口入口 | 12024320 字节（只构建，未运行） |
| `TestPreferencesProject` | PASS（约 25s） |
| 选择器 P1–P5（PS 5.1 `5.1.26100.9711`） | 5/5 PASS；`infraOk` 全 True；锁 acquire/release ok、`afterRelease=absent` |
| `-Example all` 五示例（PS 5.1） | rc 0；`home_gen.go` 5719、`tasks_gen.go` 10136、`forms_gen.go` 7429、`workbench_gen.go` 11807、`preferences_gen.go` 17889 字节 |
| `-Example preferences`（PS 7 `7.6.9`） | rc 0；产物 17889 字节、入口 12024320 字节；取锁与释放自己的 token |
| 同轮产物字节与 README 前文差异 | 前文 forms 7212 / workbench 11541 是**另一棵更早的树**上的测量，与本轮不可比 |

---

## 自伤与更正（本线）

1. **`Submit="BeginReview"` 报 `GXML-EVENT-004`**：`Submit` 事件带一个 text 载荷、`Click` 不带，
   同签名无法两用。拆成 `OnNameSubmit(text)` + `BeginReview()` 共用 `beginReview()`。
2. **绝对计数断言恒红**：初版写 `NameChanges != 1`，实际首帧外部值应用已经让它等于 1。
   全部改为取增量（基线 + 1 / 基线不变）。
3. **Reset 用例顺序反了会恒绿**：先点「重置」再 `SetText("INTERMEDIATE")`，此时 `V == A` 不回写，
   编辑器停在中间态。改为**先造中间态再点重置**，并保留"外部值未变时不回冲编辑器"这条中间断言
   （契约 §9 场景 3）。
4. **片1 的 17752 字节作废**：补 `Submit` 后为 17889 字节；片2 提交说明里已更正，不以旧数字为准。
5. **docgate 红**：`go test ./internal/docgate` 提示 `4617f27` 未在 dev-log 记录。
   那是他人提交、本线只作起点引用（本文档确以基线号引用它，门禁判据是"提交号出现"），
   其内容不由本线代写——已如实登记，不用"顺手刷绿"掩盖。

---

## 证据边界（三条必须连着读）

- 全部离屏语义结论来自**真实生成物 + 真实 `input.Router` 事件**，**不含**窗口显示、IME 组合输入、
  光标与任何性能数据；那几项仍在 README 的人工验收清单里，状态是**未通过（未执行）**。
- 作者自测**不等于**非作者复核：本线用例与观察由 WorkBuddy 通道产出，外部复核状态仍是
  **待独立复核**，不要读成"已复核通过"。
- 本线**没有**改动正式 Input 算法、ADR、CLI / Codegen / Analyzer 生产源码、进程 / Job / raw / 锁
  助手；`verify-examples.ps1` 只改了 `$known` 一行与文件头注释。

---

## 审核 A：DeepSeek 新 Input 候选（`e25828e`）的独立关键事件探针

按任务书，本线完成后用**自己写的**探针审 DeepSeek 候选（分支 `codex/r7-cand-input` =
`e25828e40987679ebdfbcf2df6bd8e3248485edd`，该工作树实测 HEAD 即此 SHA、clean）。
对照基线 `4617f27`。探针材料与驱动在交付目录 `work/audit/`，原始输出 `raw/audit-probe-{base,cand}.txt`，
结论见 `audit-deepseek-candidate.md`。

| 场景 | 基线 `4617f27` | 候选 `e25828e` |
|---|---|---|
| 空投影 + 真实插入再删除 | `changes 0→1→2` | `changes 0→1→2`（不吞） |
| MaxLen=3 追加 | `"cab"`（3 rune） | `"cab"`（3 rune） |
| 同帧 `EditEvent` + 回车 | `Submits=1` | `Submits=1`（不丢） |
| 共享字段两框 | 一致 | 一致 |
| 程序化 echo（观测项） | `PresetChanges=2` | `PresetChanges=0` |

**结论**：四条关键重放在候选上全部与基线等价或更好，技术上通过；
但"程序化外部写入是否派发 `ValueChanged`"是**语义裁决**（`contract-diff.md` §3），
本审核**不批准**，状态 **PENDING（待负责人裁决）**。技术通过 ≠ 语义批准。

边界：本探针**未能直接重放 R6 的 B1' 失败模式**——B1' 出自 R6 候选的 `armed` 方案，
基线（旧 v1）压根没有门控所以也不吞；要复现 B1' 需要 R6 候选树，本轮没有，故未复现也未反证。
本用例只确认"新候选没有引入吞事件这一类回归"。

---

## 零原生窗口 / IME / 物理键鼠声明

本线全程：0 个原生窗口创建、0 次 IME 组合输入模拟、0 次物理键鼠事件、0 次采样 / pprof / trace、
0 次安装。窗口入口只 `go build`，从不执行。
