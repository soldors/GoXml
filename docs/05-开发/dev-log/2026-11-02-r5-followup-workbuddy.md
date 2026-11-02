# R5-FOLLOWUP-WORKBUDDY 开发日志（2026-11-02）

> 本轮任务书：`D:/Codes/Goxml-agent-rounds/2026-11-02/night-product-r5/followup-r1/workbuddy-task.md`
> 隔离新树：`C:\Users\admin\.codex\worktrees\r5-followup-workbuddy\Goxml`，分支 `codex/r5-followup-workbuddy`
> 基线：`1e210c47287a5ed888bd6aa9c8b9591b6bb1b109`（R5-C 末提交，创建时 `git status --porcelain` 为 0 行）
> 产品写入范围：只有 `examples/gxml/*`、`scripts/*` 与本文件；未改 CLI / Codegen / runtime / contracts。

## 0. 开工前核验（不核验不写代码）

复核草稿来自 `checkpoints/workbuddy/snapshot.json`，状态 `UNCOMMITTED_CHECKPOINT_NOT_REVIEW_APPROVED`。按快照逐个比对**字节**后才采用：

| 文件 | 快照字节 | 拷入新树后实测 SHA-256 | 是否一致 |
|---|---|---|---|
| `examples/gxml/workbench_project_test.go` | 35196 | `2a8b84a90f829be2984a735c3b0bcd2d500f68a31c49a4bae89f95a23f34fec2` | 一致 |
| `examples/gxml/workbench_review_test.go` | 2616 | `e879bc50330b3a7e8c9cff323280261422bec8d054ae8f7d8c25f1389530460c` | 一致 |

与基线 diff 只有一行（`write("workbench_sem_test.go", []byte(workbenchSemSource))` → `… + workbenchReviewSemSource`），确认草稿没有夹带其它改动。

快照状态本身就意味着**它不是审核结论**：本轮按"接受增量复核"处理（C4 按新 SHA 接受，不拆独立教学工程），草稿走的是"先跑通再扩写"，不是"直接采纳"。

## 1. 片 1：删除→新增→选择→编辑→保存/回车 真实离屏流程

### 1.1 做了什么

草稿只有 1 条用例（`DeleteAddSaveCurrentGeometry`）。本轮扩到 5 条，并补了两个此前缺失的判据：

1. **删除后按当前几何重扫** —— 删除会给页面底部加一行状态提示，页级按钮整体上移。用例刻意保持"先删后增"的真实顺序，删除后**重新扫描**拿新坐标；同时保留一条反向观察：拿删除前的坐标去点，`动作=""`（没命中任何事件）。这条反向观察就是"为什么必须重扫"的证据，而不是靠调换顺序把位移绕过去。
2. **删除选中项要清草稿与编辑器** —— `SelectedID==0`、`Draft==""`、编辑器文本为空、计数回到"共 2 项"、不脏，五项逐项相等。
3. **ID 不复用 + 行 Key 跟随数据** —— `[1 2 3]` → 删#2 `[1 3]` → 增 `[1 3 4]` → 删#1 → 增 `[3 4 5]`。
4. **两实例隔离（当前几何）** —— A 选中 `#1` 时 B 的 `SelectedID=0`、`Draft=""`、编辑器为空。
5. **`DirtyText` 两态逐字相等** —— 四态文案必须逐字等于 `（无未保存修改）` 或 `（草稿有未保存的修改）`，不接受子串判据。

判据口径沿用上一轮：所有按钮坐标靠网格早停扫描实测，断言落到**不可被覆盖的计数器**的增量（外部值应用本身会带来一次 `ChangeEvent`，会把"最近一次动作"重写成 `change`）；没有任何一条靠 `vm.Select(1)` 之类直调冒充事件验收。

### 1.2 证据（全部在公共锁内真实运行）

| 命令 id | rc | 耗时 | 原始输出 |
|---|---|---|---|
| `f1-01-build-cli` | 0 | 3.65s | `raw/f1-01-build-cli.out`（0 B） |
| `f1-02-generate` | 0 | 1.27s | `raw/f1-02-generate.out`（27 B，真实 CLI `--no-cache`） |
| `f1-03-sem-review`（草稿轮） | 0 | 10.53s | **已被覆盖，见 1.4 自伤 1** |
| `f1-03-sem-review`（扩展轮，`-ext`） | 0 | 25.98s | `raw/f1-03-sem-review.out` = `raw/f1-03-sem-review-ext.out`（1435 B，5/5 PASS，内部 `ok … 22.916s`） |
| `f1-05-sem-review-rerun` | 0 | 28.80s | `raw/f1-05-sem-review-rerun.out`（5/5 PASS，内部 `ok … 25.479s`） |

`f1-05` 是**独立第二次**跑同 5 条，不是把第一份复制一遍：两次的每个用例耗时都不同（8.68 / 5.18 / 5.28 / 2.36 / 3.91 vs 7.71 / 4.28 / 5.08 / 2.37 / 3.43），总耗时 25.479s vs 22.916s。

同源证明：`work/extract_sem2.py` 按 `workbenchSemSource` + `workbenchReviewSemSource` **顺序**从交付的两个文件重新抽取，得到 35962 B，SHA-256 `e47e6b16c4283149fa350b1c7b39f9d2acae43e9a003617725eccf0a6199a29d`，与探针目录里被真正 `go test` 的 `workbench_sem_test.go` **逐字节相同** —— 证据跑的就是交付的常量，不是另写一份。

### 1.3 R-10 空判据的独立复算

上一轮自审的 R-10 是：`strings.Contains(DirtyText, "未保存")` 在两态里都成立，判据永远不红。本轮不引用上轮结论，**独立重做变异**：

- 变异：把探针 `vm.go` 的脏分支改成输出干净态文案；
- 只跑 `-run TestWorkbenchSemReviewDirtyTextExactBothWays` → rc **1**，`输入后文案应逐字为「（草稿有未保存的修改）」，实际 "（无未保存修改）"`（`raw/f1-04-mutation-r10.out`，302 B）；
- 同一次变异下打印"变异串里含 `未保存`"= `True` —— 说明**子串判据在这种变异下仍然是绿的**，这正是必须用逐字判据的理由；
- 恢复判据用**字节 SHA-256**（不是 `git diff` 为空）：`vm.go` 两侧均为 `126aad73c0b4a73dc1ee6244a3502cf35e64c0cd09dfb01d547ff3e652e0af3d`，`grep -c MUTATION` = 0。

### 1.4 自伤（如实登记）

1. **日志文件名冲突，草稿轮原始输出被覆盖**：两次运行都用了 `f1-03-sem-review` 作 id，扩展轮把草稿轮的 175 B 输出覆盖成 1435 B（`commands.jsonl` 里两行的 `stdoutBytes` 仍是当时真实的 175 / 1435，可作旁证，**但不能替代原始字节**）。违反了"日志文件名唯一、保留首次失败"的纪律。补救：加跑 `f1-05` 作为独立第二次证据；`raw/f1-03-sem-review.out` 与 `-ext.out` 是**同一份字节的两份拷贝，不是两次运行**，不得计成两个证据。
2. **README 遗留残片**：常见错误表里"输入一次却看到处理器被调用两次"这行的原因列，混进了一段 `**未保存/未渲染:**`（编辑事故残留）。全仓 `grep -rn "未保存/未渲染"` 只有这一处，已改写成完整句子并顺带标注证据边界（只测到"调用次数多于按键次数"，IME 与真实按键分帧未测）。

### 1.5 README 改了什么（表格格式 + 证据边界）

- 修掉上述残片；
- 新增独立小节「删除后再新增（复核补充，独立 5 条）」，含用例表与三条**证据边界**：① 与 12 条并列、不重复计数（本轮没有把旧的 13 条重跑一遍当新成果）；② 观察全部来自离屏测试，不含窗口显示 / IME / 光标 / 性能，人工清单状态仍是**未通过（未执行）**；③ 作者自测 ≠ 非作者复核，外部复核状态仍是**待独立复核**。

### 1.6 提交

`1b58f2e`（`test(examples/gxml): 复核补充删除→新增→选择→编辑→保存真实离屏流程`），三个文件：
`examples/gxml/workbench_project_test.go`（+1/-1）、`examples/gxml/workbench_review_test.go`（新增 174 行）、`examples/gxml/README.md`（+23/-1）。

`git ls-tree` 已确认两个测试文件都在该 SHA 的树里（blob `c1e1bee7…` / `fec678c7…`）；提交后 `git status --porcelain` 为 0 行。

## 2. 片 2：D-3 输入同步有界实验（只观察，不改生产语义）

完整观察记录另立一份：`docs/05-开发/2026-11-02-D3-输入同步有界实验观察.md`。本节只记结论与自伤。

### 2.1 做了什么

新增 `examples/gxml/input_sync_probe_test.go`（**唯一**产品改动）：

- fixture（页面 + ViewModel）全部是文件内常量，不新增 `testdata/`，不碰 CLI / Codegen / runtime / contracts；
- 外层测试走**真实** `cli.Run(["generate", …, "--no-cache"])`，并校验生成物里确实含 `applied0` / `ed.SetText(cur)` / `case widget.ChangeEvent:`——跑的是生成代码，不是手抄的同步块；
- 内层实验写入临时 module，用真实 `input.Router` 逐帧驱动，上限 **8 帧**，记录四元组 `editor / vm / applied / changes`；
- 三种回写：`verbatim`（原样）、`upper`（转大写）、`append`（每次追加 `!`，**故意非幂等**）；
- 另有机制探针：复刻那段同步块，只对照"回调后是否把 `applied` 重贴到 `vm`"。

### 2.2 结果（真实生成物，`raw/d3-01-explore-trace.out` / `raw/d3-04-outer-test-obs.out`）

| 回写 | 逐帧 | 收敛 | 末帧 |
|---|---|---|---|
| `verbatim` | f1 `"abc"/"abc"/"abc"/1` | 第 2 帧静默 | `editor="abc" vm="abc" applied="abc" changes=1` |
| `upper` | f1 `"abc"/"ABC"/"ABC"/1` | 第 2 帧静默 | `editor="abc" vm="ABC" applied="ABC" changes=1` |
| `append`（非幂等） | f1 `"abc"/"abc!"/"abc!"/1` | 第 2 帧静默 | `editor="abc" vm="abc!" applied="abc!" changes=1` |

一次按键：`editor="z" vm="z" applied="z" changes=1`（真实指针点击取得焦点，再 `key.EditEvent`）。

机制对照（8 帧上限）：重贴 `applied` → `changes=1`；去掉重贴 → `changes=8`、`vm` 每帧加一个 `!`。

### 2.3 三个问题分开回答

1. **事件机制**（已复现）：外部改值 → 帧内 `SetText` → **一次** `ChangeEvent` → **一次** `ValueChanged`。预热两帧 `changes=0`，说明不是每帧都发。
2. **可复现往复**（生成代码路径**未复现**）：三种回写都在第 2 帧静默。往复只在"去掉重贴行"的反事实变体里出现，且到第 8 帧硬停。
3. **典型产品流影响**（可观察后果，交裁决）：① 多余回调（`Changes` ≠ 按键数）；② 非幂等回写导致编辑器/模型分歧（`upper` 编辑器停在 `abc`、模型是 `ABC`；`append` 停在 `abc`、模型是 `abc!`）。

**没有报框架缺陷**：真实链路没复现出往复；只复现出上面两条后果。候选方案（保持契约 / 抑制程序化同步事件 / 应用后回读 / 契约层禁止非幂等）的影响比较写在观察文档 §7，**仅候选**，`ValueChanged` 公开语义未改。

### 2.4 自伤（本片 3 条）

3. **编译一次红（两个错误同时报）**：先用了 `widget.Editor.Focused()`（本版没有该方法），又给 `key.EditEvent` 写了 `Tag` 字段（本版只有 `Range`/`Text`，靠焦点路由）。rc 1，`.err` 271 字节保留在 `raw/d3-01-explore-trace.err`。**但这一轮的 stdout（53 字节）被随后的成功轮覆盖成 4701 字节**——`d3-01` 这个 id 又被用了两次，仍是同名覆盖（与 1.4 自伤 1 同一类）。
4. **外层绿色运行看不到内层观察**：`CombinedOutput` 只有在失败时才打印，第一次绿色运行的日志只剩一句 PASS。补了"绿色也把 `OBS`/`MECH` 行原样转写进本层日志"，但第一版过滤条件写成 `HasPrefix("OBS ")`，实际行首是 `文件:行号:`，于是只转写了 MECH（`raw/d3-03` 可见）。改成 `Contains` 后才是 `raw/d3-04`。
5. **变异三次被锁挡在门外**：公共锁被其它 lane 持有（`acquire: busy`，rc 70）。其中三次是在"已变异、未运行"的状态下退出的；每次都用 `work/mutate_d3.py --restore` 按**字节 SHA-256** 恢复到基线（`546ec31b…`，`MUTATION` 计数 0）后才继续写别的，没有把变异态留在交付目录里。`mutate_d3.py --apply` 自己也校验"当前必须是基线 SHA"，防止在已变异状态上二次变异。

## 3. 片 3：教程与组合验收准备

### 3.1 示例名单一来源检查（新增测试）

`examples/gxml/selector_single_source_test.go`（`TestExamplesSelectorSingleSource`，rc 0）。它只查**静态一致性**，
替代不了真跑脚本的行为测试，但能挡住最常见的漂移。判据共 5 条，全部落在可数字节或集合比较上：

1. `scripts/verify-examples.ps1` 必须有 UTF-8 BOM、**零 CR**（PS 5.1 读中文注释 + `.gitattributes` 要求 LF）；
2. `$known = @(...)` 定义**恰好 1 处**，解析出的名字序列必须正好是 `starter,tasks,forms,workbench`（顺序也固定）；
3. 每个示例名在脚本里**只出现 1 次**（就是 `$known` 那次）——再抄一份列表，`all` 与 `-Example` 就会各自漂移；
4. `testdata/` 的目录集合与注册表**互为全集**（多一个没注册的目录会红）；
5. 每个示例目录三个文件都在且非空，`.gxml` 恰好 1 个，且 `starter` 的页面名必须是 `home.gxml`（不能从目录名推导）。

### 3.2 组合树路径（不捏造组合 SHA）

两个选择器脚本都吃 `--repo`，所以组合树**不需要改脚本**：

```text
python scripts/test-verify-examples-workbench.py --repo <组合树> --out <目录> --lock-path <公共锁>
```

已核对：两个脚本里没有写死的本机绝对路径（`grep -E "C:\\\\Users|r5-|night-r5"` 无命中）。本轮**没有**产出任何
组合 SHA，也没有把本树 SHA 说成组合结果。

### 3.3 G2/G3/G4 与 Padding 的复验责任（写进 README 表）

README 新增「组合验收（本树结果不能当组合通过）」一节，逐项写明：G2（命中字节 + mtime 不变）、G3（`--no-cache` 强制生成）、
G4（失败保留旧产物与 Manifest）在本树通过，但 B 线改的就是 `internal/cli/` 缓存键，**必须由协调者在真实 B→A→C 组合后复验**；
`Padding` 在本基线仍被 Codegen 忽略（TASK-CODEGEN-001-21），所以本树的留白/溢出**不代表**组合树，也不能当组合通过。
本节没有拿任何作者侧 Background 候选做实验组合。

### 3.4 小画布记录（`raw/f3-02-canvas-report.out`）

用真实生成物 + 真实 router：先在不约束最大尺寸下量内容高度，再在三套画布上做网格扫描，看哪些动作**真的能点到**。
探针文件是 `work/wb-probe/canvas_report_test.go`（**临时记录用，不入仓库**；随交付 work/ 一起给出，可复现）。

| 画布 | 自然高 | 溢出 | 可达动作 | 缺 |
|---|---|---|---|---|
| 360×240 | 382 | 是 | 6/7 | `add:4`（「新增」） |
| 480×360 | 382 | 是 | 7/7 | — |
| 720×480 | 382 | 否 | 7/7 | — |

结论只到这里：**360×240 下「新增」点不到**，页面没有滚动能力（本基线也没有滚动 API），因此
"裁剪了但还能滚"这种说法不成立，本记录**不批准**新增滚动或任何新 API。另外自然宽度不可用——
`layout.Flex` 会把最大宽度吃满（记录里是 4000），只有高度 382 有意义。

## 4. 收口（本线自测，不是外审）

| 项 | 命令 | 结果 |
|---|---|---|
| 受影响包 | `go test -mod=readonly -p 2 -count=1 -timeout 30m ./examples/gxml` | rc **0**，84.7s（`raw/f4-01-examples-pkg.out`） |
| docgate | `go test -count=1 ./internal/docgate` | rc **0**，2.3s（`raw/f4-02-docgate.out`） |
| workbench 选择器 | `scripts/test-verify-examples-workbench.py --repo . …` | W1–W5 **5/5 PASS**，rc 0，50s（`out/workbench-selector-followup.json`） |
| forms 选择器 | `scripts/test-verify-examples-forms.py --repo . …` | F1–F5 **5/5 PASS**，rc 0，50s（`out/forms-selector-followup.json`） |
| gofmt | `gofmt -l examples/gxml` | 空 |
| vet | `go vet ./examples/gxml` | rc 0 |

两个选择器脚本**自己持公共锁**（`--lock-path`，内部 acquire → 跑 → release，日志回读 `afterRelease: absent`），
所以它们是直接调用的，没有经过 `run_r5_gate.py` 的批量通道，也就没有进 `commands.jsonl`；
它们的证据是各自的 `out/*.json`（含每个用例的 rc）与本文这张表，起止时间未采集（不补造）。

入口构建：`TestWorkbenchProject` 在临时 module 里 `go build ./cmd/workbench` 并检查产物非空——
**只构建、不执行**，本线全程没有开过一次原生窗口，也没有跑诊断程序 / 采样 / pprof / trace。

## 状态口径（本文件每一节都适用）

| 维度 | 片 1 | 片 2 | 片 3 |
|---|---|---|---|
| 源码实现 | `1b58f2e` | `61c2549` | `446e511` |
| 自测 | 5/5，两次独立运行；变异 rc 1 | 外层 1 条 + 内层 6 条子用例 rc 0；反事实变异 rc 1 | 单源检查 rc 0；小画布为**记录**，非通过项 |
| 非作者复核 | 未开始 | 未开始 | 未开始 |
| 主仓 | 未合入 | 未合入 | 未合入 |
| 远端 | 未 push（全程 `GOXML_AUTO_PUSH=0`） | 同 | 同 |
