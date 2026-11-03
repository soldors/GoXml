# 2026-11-02 R8-C EVIDENCE-TOOLS（WorkBuddy）

树 `C:/Users/admin/.codex/worktrees/night-r8-workbuddy-20261102/Goxml`，分支 `codex/night-r8-workbuddy-20261102`，
基线 `e72c478e6d59b6636b98005c73d580d6f4f7b9f0`（开工时 HEAD/分支/clean 三项核验通过，`status --porcelain` 计数 0）。

## 1. 做了什么

把"开发命令留证"变成随仓工具，针对 R7 复盘第 3 条"证据质量欠账"（首败 stdout 被覆盖、
JSONL 写坏、崩溃后"未运行/已运行/未知"混记）。**不重写** R6 已合入并复核的 Windows Job
进程树与 proclock，不改 PS 注册/示例/CLI/Codegen，不引入依赖，不增加第六个示例。

| 文件 | 职责 |
|---|---|
| `scripts/evidence_schema.py` | 台账 schema 与校验的**唯一**定义（写入方与校验方共用，防字段漂移） |
| `scripts/record-command.py` | 命令记录入口（`run` / `recover`） |
| `scripts/verify-evidence.py` | 只读证据校验器 |
| `scripts/test-record-command.py` | 真实闭环自检 |
| `scripts/test-verify-evidence.py` | 校验器分支毒化 + 健康对照 |
| `scripts/mutate-evidence-tools.py` | 变异取证（8 个变异必须翻红） |

复用关系：`record-command.py` 直接调用 `verify_examples_proc.run_capture`（shell=False、
真实 argv 数组、局部环境、原始双通道字节 + SHA、PID + 创建时间、单调区间、真实 rc/timeout/cleanup），
锁走 `verify-examples-lock.py` 的 `Lock`（原子排他、只按自己的 token 释放、从不按 mtime 删锁）。

## 2. 提交

| # | SHA | 内容 |
|---|---|---|
| 1 | `7b971949c22e1b47750fa5446ee7cdb186a86695` | feat(scripts)：六个新脚本（工具 + 测试 + 变异取证） |
| 2 | `57031e1b3482a9783cb635951e70d4e701d9f102` | fix(scripts)：rc 越界毒化用例改用负数 |

全部 Git 写带 `GOXML_AUTO_PUSH=0` + 临时空 `core.hooksPath`；**精确 add**（无 `-A`/`.`）；
未 push、未 merge 主仓、未 reset/clean/stash。

## 3. 三个切片的判据与实测

**切片 1（记录入口）**：`phase` 分 `prepared/completed/recovered/unknown`——
`prepared` 只写 `<run-dir>/planned.json`，**不得据此声称零启动**；
`unknown`（硬终止后）的 `started`/`rc`/`rcSource` 一律 `None` 且 `recordOk=False`（不把未知记成 0）。
退出码 0/2/3/4/5/6/7/8/9 各自独立含义；**记录成功与业务成功分开判定**：rc=7 时退出码 4 但
`recordOk` 仍为 True；超时 rc=8 标 `rcSource=forced-on-timeout`，不冒充自然退出码。
锁必须显式 `--lock-mode acquire|external`；他人持锁退出 8 且**不发起任何子进程**；
声明 external 却没锁文件 → 退出 2（不暗中绕锁）。

**切片 2（只读校验器）**：逐行严格 parse；缺失/错类型/重复 id/rc 来源不自洽/时间倒序/
raw 与磁盘不符 → FAIL；未启动/超时/收口未知/`recordOk=False`/`elapsedSec` 未知 → INCOMPLETE（不是通过）；
历史 schema 标 `unsupported`。**只读**：不修复、不吞坏行、不归一化字节后再判。

**切片 3（自检与闭环）**实测（每个数字都带命令与全量口径）：

- `python scripts/test-record-command.py --work <空目录>` → **126 项 / 失败 0 / 跳过 0**
  （跑 2 遍：开工自检一遍 + 交付门禁一遍）。真超时、真"父退孙活"、真硬 kill 后 recover、
  见证进程（不在被测 job 内）全程存活且其收口在有界 finally 中、PS5.1 + PS7 各跑通
  `--args-file` 参数保真（空串/空格/中文/引号/尾反斜杠逐字回读）。
- `python scripts/test-verify-evidence.py --work <空目录>` → **96 项 / 失败 0**
  （跑 3 遍；第 2 遍真实翻红暴露一处用例失效，见下第 4 节第 4 条）。
- `python scripts/mutate-evidence-tools.py --only M1,M2,M3,M7,M8` → 5/5 翻红；
  `--only M4,M5,M6` → 3/3 翻红；**合计 8/8**，运行前后 9 个源文件 SHA-256 全部未变。
- `python scripts/verify-evidence.py --jsonl <R7 作者台账>` → **28 行 = 6 行 bad-json（FAIL，逐行点名）
  + 22 行 unsupported**；原件未改（只读复核，sha256 `fe5e14b36d2db9e7fbe87b9d511c0bba08ce5a6035de5b70d8406f1d7c3b5e64`）。
- 自用闭环：交付台账 `night-product-r8/workbuddy/commands.jsonl` 由本入口记录 6 条真实门禁命令
  （含一条**失败**的 `rc=1` 记录，如实保留），再由校验器验证 → **PASS（6/6，fail=0，CR=0）**。

## 4. 自伤更正（按发现顺序，全部带出处）

1. **Windows `os.open` 默认文本模式**：台账行尾被写成 `\r\n`（实测 `CR=1`）→ 加 `O_BINARY`，
   现在交付台账 `CR=0`。这是字节级保真问题，与 R6 raw 捕获同一口径。
2. **recordOk 写行后才算**：台账行恒为 `False`（台账不可覆写）→ 改为**落盘前自证**（先序列化
   再 parse + 结构/自洽/raw 三项），写完只做回读复核；回读不符时判 7，不回头改行。
3. **rc 范围用了 POSIX 的 0..255**：Windows 被 Ctrl+Break 终止的退出码是 32 位 → 放宽到
   `0..0xFFFFFFFF`；连带后果见第 4 条。
4. **复跑才暴露的失效用例**：放宽范围后，`rc-out-of-range` 毒化用 999 已不再越界
   （复跑真实翻红：`codes=['business-pass-rc-mismatch']`）→ 提交 `57031e1` 改用 `-1`。
   这条是"再跑一遍"才发现的，首遍全绿并不能证明稳定。
5. **退出码互相盖住**：`timedOut` 无条件短路成 5，导致已声明 `--expect-timeout` 的调用方看不见
   收口问题 → 改为"未声明期望超时才短路"，声明了就落到 6。
6. **锁误报 9**：`decide_exit` 在释放锁之前判定，`released=None` 被当成失败 → 改为只有
   明确 `released is False` 才报 9。
7. **校验器把历史台账全判 FAIL**：R7 的 22 行手写台账被判 `missing-schema` → 加历史形态识别
   （`format` 字段命中 `r6c-2`/`r6c-selcase-1`，或 `seq`+`cmd` 形状命中 R7 台账）标 `unsupported`，
   且**不做模糊推测**（防止被篡改的记录靠"长得像历史"躲过判定）。
8. **未启动时误报收口问题**：`not-contained`/`active-*-unknown` 对"根本没启动"的用例是误导 →
   收口类只在 `started is True` 时判定，未启动只报 `not-started`。
9. **恢复时把 prepared 当结果**：硬终止后 `result.json` 停留在 `phase='prepared'` 版，
   原实现会凭它造出"已跑过" → 加 `is_final_result()`（要求无 `phase` 键）判别。
10. **中断用例曾不可判定**：首版依赖信号落地，实测信号没接住（子命令跑满 30s 后 rc=0 正常结束）
    → 拆成两条：软中断只断言"有界退出 + 必有可定位结果"（第二次实测信号确实被接住：
    `signal=21, count=1`，6s 有界超时内落盘），硬 kill 走**可判定**路径
    （`planned.json` 在 + 无台账行 → `recover` → `unknown` 形态 → 校验器 INCOMPLETE）。
11. **PIPE 卡住**：硬 kill 后 `communicate()` 等 EOF 超时（孙进程可能继承管道句柄）→ 改 `DEVNULL`。

## 5. 证据边界（未做的事，不冒充已做）

- 未跑 Go 全仓门禁：`go build` / `go vet` / `gofmt` 在本线**不适用**（本线零 Go 代码改动，
  按公共任务"代码未变不重复检查"不重跑）；`go test ./internal/docgate` 见第 7 节。
- 本线**零原生窗口、零诊断 exe、零采样/pprof/trace、零安装、零联网补依赖**。
- 进程测试只用本仓自有 Python 哨兵 `scripts/testdata/verify-proc-sentinel.py`，不启动 GUI。
- Windows race 与 CGO0 冲突：本线不涉及 Go 构建，未测（沿用既有登记）。
- 未复跑 B 线候选测试（见交付 `audit-zcode-b.md` 第 6 节声明）。

## 6. 交叉审：对 B 线（ZCode）的独立审核

详见交付 `audit-zcode-b.md`。要点：

- 同帧三重冲突是**真实**外部写（`obj.Title` 赋值）+ `r.Queue(key.EditEvent)` + 真实 Return 同帧，
  不是"只 Return"或"直接调处理器"；观测 `XOLD` 的成因可在 Gio v0.10.3 源码复核
  （`widget/editor.go:886` `SetText` 后 `SetCaret(0,0)`，故后续 `Range{}` 从头插入）。
- 投影是**真实**夹具行为：`widget/editor.go:1010-1018` 的 `replace` 对程序化 `SetText`
  同样做 MaxLen 截断与 Filter 过滤（"letters"→空、"AAb"→"AA"）。要写清：投影是 Gio 既有行为，
  **不是候选新增能力**，候选只是把它纳入 D7 语义承诺。
- 真非幂等 Append 成立：`AppendText` = `vm.Title += s`（`internal/compiler/codegen/input_divergence_test.go:53`，
  本仓基线文件，本线未改）；四种处理器类型 + 无处理器形态齐备。
- 4 条观察项（P2，记录完整性）：`distinct-fields`/`dual-view` 缺 `matchesFixture` 自证字段；
  I1 的例外清单不含"无处理器非受控态"（scen6 实测 `editor=draft` 而 `model=""`）；
  `dual-view` 未给逐帧序列；quiet 是有界观察不等于永久稳定（作者措辞已限定）。
- **结论：技术/事实核对通过；`semanticApproved=false`，状态 PENDING，不输出 ACCEPT。**

## 7. 收尾门禁

- 交付台账自校验：`verify-evidence.py --jsonl <交付 commands.jsonl>` → `PASS`（6/6，fail=0，exit 0）。
- `go test ./internal/docgate`：本节文档已登记本线提交号 `7b97194` 与 `57031e1`（基线 `e72c478`
  作为开工引用登记）。结果见交付 `handoff.md`。
- 五态：源实现已提交 / 自测完成 / **外审未进行**（DeepSeek 审 C） / 主仓未合入 / 远端未推送。

## 8. P0 后续：对齐隔离补修 `58c5e74` + 补变异（提交 `cebb4ae`）

背景：协调者对本线 `8fceb71` 判 BLOCK，列出三类缺陷（普通错误路径锁泄漏 / recover 时 raw SHA
被按当前磁盘洗白 / 校验器接受 NaN·Infinity·重复键·无时区时间），已隔离补修 `58c5e74` 并发布。
本轮把该补修**按字节**同步回作者树，再补变异锁住这三类。

同步前提（实测，不是"照着改一遍"）：

- `git log --oneline -1 58c5e74^` = `8fceb71`＝本线原 HEAD；
- 6 个脚本的 pre 版 blob 与作者树 blob 逐字节一致（`evidence_schema` 8411aace、
  `record-command` ad170947、`verify-evidence` 652b8b67、`test-record-command` a296b6a7、
  `test-verify-evidence` 0c3258c1、`mutate-evidence-tools` 31ed9f1c）；
- 同步后 `git hash-object` 复核：c2d44a84 / a6283aa / f830d045 三个生产脚本与 `58c5e74`
  的 post blob 完全一致，0 mismatch。

新增判据（先证明能变红，再写进自检）：

- 锁错误路径归还：`--report` 指向已存在文件 → 取锁后走用法错误路径（退出码 2），
  断言锁文件已消失（`lock.errpath-released`）且已存在的报告字节未被覆盖。
- recover 哈希承诺：先正常记录一条，再往 raw 追加字节，然后 recover 到**另一份**台账；
  断言 `raw.stdout.sha256` 仍是捕获时原值、`recordOk=False`、退出码 7、校验器报
  `raw-sha-mismatch`。用独立日志目录，避免污染主台账的收尾复核。
- 严格输入：在**落盘字节**上把 `"elapsedSec":1.0` 改成 `NaN` / `Infinity`、行尾追加重复键、
  `startedUtc/endedUtc` 去掉时区；四条都必须判红（不是改内存里的对象）。

变异：M9 摘掉 `try/finally`（锁只走成功尾部）、M10 `raw_block` 按当前磁盘重算 bytes/SHA、
M11 `json.loads(text)`（放弃 `parse_constant` 与 `object_pairs_hook`）、M12 时区检查改 `if False:`。
12/12 翻红，源文件 SHA 前后一致。

实测（各连跑两遍）：`test-record-command.py` **141 项**全绿；`test-verify-evidence.py`
**107 项**全绿（含 R7 原件只读复核，skipped=0）。

自我更正：

- 原计划"补 3 个变异"，实际拆成 4 个（M9–M12）：时区与严格 JSON 分属两个检查点，
  塞进一条变异会让"哪些断言该翻红"变模糊，判据强度反而下降。
- M11 期望断言由 6 条收到 4 条：NaN/Infinity 被宽松 parse 进来后仍会被 `nonfinite-elapsed`
  兜底判红，退出码仍是 1，所以 `.exit` 两条对**这个**变异不敏感——不把它们算进红判据，
  但留在测试里当第二层（非有限值落在数值字段时也判红）。

未做／边界：不 push、不动主仓、未改 Job/锁助手生产实现、未增加第六个示例；
`58c5e74` 之外的改动只有测试与变异两份文件。

- P0 五态：源实现已提交（`cebb4ae`） / 自测完成 / **外审未进行** / 主仓未合入 / 远端未推送。

## 9. P1：external 锁归属确认 + 单调差口径（提交 `5936174`）；非 Windows 降级的结论

补登记：P0 的文档提交 **`75766ee`** 一并登记在此（当时只登记了代码提交 `cebb4ae`，
漏了它，被 `go test ./internal/docgate` 抓出——见本节 9.4 第 3 条）。
提交 `91a7a70` 为 P1 的文档提交，**`34cfb78`** 为 P2 的文档提交。

**只动测试与变异文件**——三个生产脚本（`evidence_schema.py` / `record-command.py` /
`verify-evidence.py`）本轮**未改**，与 `58c5e74` 的字节对齐保持不变。

### 9.1 external `--lock-token` 归属确认（+10 项）

- token 与锁文件里的持有者不一致 → 退出码 2、stderr 报 `usage error`、stdout **无** summary JSON、
  不发起子进程、锁文件字节不变、不多写台账行；
- `--lock-token` 用在 `acquire` 模式 → 退出码 2 且不建锁文件（token 只对 external 有意义）；
- external 模式**不得声称自己持锁**：台账 `lock.acquired=False`、`lock.released=None`。

（用法错误路径只写 stderr、stdout 上没有 JSON，所以"未发起子进程"改用
"无 summary + 锁字节不变 + 台账行数不变"来证，不能读 `summary['recorded']`——那是 None 不是 False。）

### 9.2 单调差口径：端点不持久化（+5 项）

实测 `verify_examples_proc.py:284` `t0 = time.monotonic()`，`:308`
`elapsedSec = round(time.monotonic() - t0, 3)`。结论：

- **只有差值持久化，单调端点不持久化**——而且不该持久化：monotonic 的绝对读数跨进程、
  跨平台都不可比，存下来没有语义。墙钟时刻由 `startedUtc`/`endedUtc` 承担（必须带显式 UTC 偏移）。
- **不得**用 `endedUtc - startedUtc` 交叉核对 `elapsedSec`：系统时钟会被 NTP 调整或跳变，
  两者不一致是**正常事实**，交叉核对会把真实记录误判红。`elapsedSec` 本身只做"有限且非负"。

### 9.3 非 Windows 便携降级：结论是**维持现状，不改**

原计划"非 Windows 上也能跑、raw/SHA/rc 完整、整树收口标未证 → INCOMPLETE"。核实后不做的理由：

1. 现状是 fail-closed：`verify_examples_proc.py:103`，`auto` 模式在非 Windows 返回
   `CONTAINMENT_REFUSED`（`unsupported-platform: process-tree containment is Windows-only`），
   **启动前拒绝、不发起子进程**。
2. 要改成"能跑但收口未证"，必须改 `verify_examples_proc.py` / `verify_examples_proctree.py`
   ——这两个是**明令只读**的现有 Job/进程树生产实现；在 `record-command.py` 里另起降级执行路径
   等于重写进程树，违反任务书"复用现有实现、不重写"。
3. 即使改了，本机是 Windows，**无法真机验证**非 Windows 行为；按红线不得外推，只能标"未验证"。
4. 更根本的判断：整树收口未证的证据拿去门禁用，风险比"拿不到证据"更大——fail-closed
   本身就是更保守也更诚实的选项。

→ 建议维持现状。若确需降级，应先由协调者决定是否解除"Job 助手只读"约束，并在非 Windows 真机上实测。

### 9.4 一处自我更正 + 一处一手发现

- **自我更正**：M13 的 `clock.no-wallclock-code` 原本写"不含 bad-elapsed/negative-elapsed/…"
  这类**已知 code 黑名单**——实测对**新加的**过度校验不敏感（漏掉 `elapsed-wallclock-mismatch`），
  变异只翻红 2/3。改成要求健康记录**零 issue** 后 3/3。黑名单式判据天生对新缺陷免疫，不能用。
- **一手发现**：M15 原设想"external 模式去释放他人的锁"**不可达**——
  `verify-examples-lock.py` 的 `release()` 有两道独立守卫（未 `acquire` → 直接 False；
  token 不匹配 → 拒绝删除）。external 从未 `acquire`、`self.token` 为 None，
  物理上删不掉他人锁文件。已改为测真正的归属声称（把自己记成 `acquired=True`）。
- **自我更正**：漏登记 P0 的文档提交 `75766ee`（只登记了代码提交 `cebb4ae`），docgate 判红。
  门禁的"维护提交豁免"只覆盖**仅动 dev-log/CHANGELOG** 的提交；同时改了使用说明的文档提交
  不算维护提交，必须登记。教训：文档提交也要登记号，且补登记要放在**只动 dev-log** 的提交里。

实测：`test-record-command.py` **151 项**、`test-verify-evidence.py` **112 项**，各连跑两遍全绿；
变异 **15/15** 翻红，源文件 SHA 前后一致。

- P1 五态：源实现已提交（`5936174`） / 自测完成 / **外审未进行** / 主仓未合入 / 远端未推送。

## 10. P2：多份 JSONL 聚合（修一处真缺陷）+ 接入现有自检入口（提交 `69b58f1`）

### 10.1 聚合：一处真缺陷（实测发现并已修）

现象：`verify-evidence.py --jsonl a --jsonl a`（同一份台账传两次）→ 文本输出
`duplicate-id agg-1 -> a:1, a:1`，但 **`verdict=PASS exit=0`**、`totals.fail=0`，
两个 report 的 line 1 都是 `status=pass codes=[]`。

对照实验（同一 id 分布在两个**不同**文件）：b 的 line 2 `status=fail codes=['duplicate-id']`、
`counts.fail=1`、verdict FAIL。说明判定逻辑本身是对的，只有"同文件两次"这条路径漏了。

根因：`find_duplicates` 用 `(path, line)` 判断是否"先到先得"的那一条；两份相同输入的定位
**完全相同**，于是两条都被当成第一条，都不追加 FAIL。

修法：入口拒绝（退出码 2），用 `realpath + normcase` 归一，换写法（`./a.jsonl`）也识别。
**为什么选"拒绝"而不是"判红"**：判红会打出"每一行都跟自己重复"的满屏噪音且没有信息量；
而"点了名却判通过"比报错危险得多——调用方是按 exit code 判断的，会以为过了。

与 `58c5e74` 的关系（可审计）：`git diff 58c5e74 -- scripts/verify-evidence.py` 是
**纯追加 13 行**，全部落在 `main()` 的参数校验处，其三处修复（锁 / recover / 严格 JSON）
一字未动。

判据（+13 项）：跨文件重复 id 判红且定位点名**两个文件**（路径含盘符冒号，只能从右侧切行号）；
每份文件独立报告（路径/字节/SHA/计数）与 `totals` 跨文件累加；同一份传两次 → 退出码 2 且
**不写**报告；换写法传同一份 → 同样拒绝。

### 10.2 接入现有自检入口：零改动 + 归属口径

接入方式：不改 `selfcheck-verify-examples-proc.py` 任何一行，用记录入口在它外面包一层
（该入口 48 项、约 8.5s，实测全绿）。判据 11 项：`argv` 指向该入口、`tool.name` 是
`record-command.py`、raw 含 `"checks"` 且 SHA 自洽、报告文件已写出、校验器判 PASS。

归属口径（已写进使用说明 5b，这里记下为什么）：台账行的 `argv` 是**被记录的命令**，
`tool.name` 是**记录器**，两者不可互相顶替。这一行证明的是"记录器完整可信地捕获了
那个入口的一次执行"，**不是**"那个入口自己产出了这份证据"。要判断入口本身过没过，
看 `business.pass`（它的退出码）与该入口自己的报告。

### 10.3 实测

`test-record-command.py` **162 项**、`test-verify-evidence.py` **125 项**，各连跑两遍全绿；
变异 **17/17** 翻红（新增 M16 / M17），源文件 SHA 前后一致。

- P2 五态：源实现已提交（`69b58f1`） / 自测完成 / **外审未进行** / 主仓未合入 / 远端未推送。

### 10.4 一处遗漏的自我更正：P0 同步漏掉的两个文件

P0 的字节同步只核对了"6 个脚本的 pre blob 与作者树一致"，**没有核对 58c5e74 是否新增了文件**。
本轮用 `git diff --stat 58c5e74 -- scripts/`（不是只看那 6 个文件）才发现：隔离补修还
**新增**了 `scripts/test-evidence-review.py`（112 行）与 `scripts/test-record-command-review.py`
（150 行）——两个独立对抗性测试，我的树上一直缺。已按字节补回（`git cat-file blob` 取原始
字节 + SHA-256 复核），提交 `cd7fa87`。

实测：`python -m unittest test-evidence-review` **7 项全绿**；
`python test-record-command-review.py --out <dir>` **32 项全绿**。
后一个是**脚本式 runner**，用 `python -m unittest` 跑会得到 `Ran 0 tests` 的**空判据**——
差点把它当成"通过"。它的覆盖点（`report-no-lock-leak`、`recovery-keeps-historical-hash`、
`external-no-proof-refused`、`unsafe-id-*` 等）与本线 P0/P1 补的判据交叉验证，结论一致。

教训：**对齐不能只比对"两边都有的文件"，必须先看对方新增/删除了什么**
（`git diff --stat <ref> -- <path>` 而不是逐个 `git show <ref>:<file>`）。

## 11. 收尾复核（2026-11-03）：三处更正

复核方式：不凭记忆，回到事实（git / 实测）重新核对本线自己写下的每条结论。

1. **B 线语义结论已过期**。第 6 节原写"`semanticApproved=false` → PENDING"。实测主仓
   `3c969f0`（2026-11-02 23:15，`feat(input): adopt approved user-edit priority and silent
   synchronization`）**已采用经批准的用户编辑优先与静默同步策略** → Input 语义已获批准。
   本线"不输出 ACCEPT"的判断维持（语义批准不是本线权限），但理由改为"语义批准已由他方完成"。
   `audit-zcode-b.md` 正文**保留当时判断原文**不改写，只在 handoff 加更正说明——
   改写历史结论的原文，读者就无从判断"当时到底看到了什么"。
2. **主仓 WIP 数字过期**。handoff 写"93 份用户 WIP"，复核实测
   `git -C D:/Codes/Goxml status --porcelain` = **63** 条（本线未动）。
   数字来自本线开工前的记录；主仓状态在并行会话中持续变动，引用必须带**复核时间**。
3. **补回两个测试文件后没有复跑**。`cd7fa87` 改了 `mutate-evidence-tools.py` 的 FILES
   （把两个评审测试纳入字节比对），我却直接交了——**声称的"17/17 翻红 + sourcesUnchanged"
   最后一次实测发生在改这个脚本之前**。已在最终树（`3952560`）复跑：
   **125 / 162 项全绿，17/17 翻红，sourcesUnchanged=true**（报告 `out/final-mutate-full.json`）。

教训：**改了取证工具本身，之前用它得出的结论就必须重跑**；交付文档里的每个"实测值"
都要能对上复核时刻，不能沿用旧值。


---

## 12. R9-C EVIDENCE-CLOSEOUT（2026-11-03）

基线 `4fe52f8de3a37051109545f64b13ec49b849e9af`，分支 `codex/r9-evidence-workbuddy-20261103`，
新树 `C:/Users/admin/.codex/worktrees/r9-evidence-workbuddy-20261103/Goxml`。
提交 `0746d942acfe4a8a89f8f517ee22e49543daa4df`（实现 + 使用说明）。

### 12.1 迁移方式：先看完整文件集，再迁净差异

`git diff --name-status 4fe52f8 680747a` 全树 30 项，其中 scripts 只有 4 项，且
`--stat` 显示 **376 插 / 0 删**——旧树这 4 个文件是基线的严格超集，所以按 blob
字节迁移（`git cat-file blob` + `git hash-object` 校验）即可，不重放旧 14 提交链。

`evidence_schema.py`（`c2d44a84`）、`record-command.py`（`a6283aa`）两侧同 blob，
两个评审测试也已在基线 —— 与任务书"58c5e74 和两评审测试已合入"一致。

**使用说明不能这么干**：那一份是 61 插 / 23 删，删掉的 23 行里有主仓后加的
`--no-disk`、签名认证、"锁释放须另核"、126/96 历史口径等段落。所以做法是
**以主仓基线版本为底、往上加**，迁移后逐条 grep 复核那 23 行仍在（实测全部在）。

### 12.2 四项 review 修复

1. **公开台账 token**：`public_lock_block()` 返回**副本**，去 token 键，
   写 `tokenFingerprint = "sha256:" + 完整 hexdigest`；`planned.json` 保留真 token；
   释放仍走 `Lock` 对象自己的 token；recover 读旧 planned 走同一路径。
   判据 = `redact.*` 一组（私有计划保留 / 台账与 report 不含真 token / 指纹完整且稳定 /
   恢复同样脱敏 / 原 planned 与 raw 字节不回改），M18 锁死。
2. **direct API 重复定位**：`find_duplicates` 改按 (report 索引, 行号)。
   CLI 拒绝"同一份传两次"**不能顶替**库这一层——`build_report([a, a])` 绕不过 CLI。
   另有对照：两份不同台账不得误报重复（否则那条红就是恒真空判据）。M19 锁死。
3. **`lastLineUnterminated`** 初始化为 False，四种情形（正常/空/缺尾换行/半行坏 JSON）
   逐项断言**真实 bool 类型与取值**，不用 `.get(默认)` 隐去未知。
4. **selcheck 双层路径**：只传 `sc_base`；并按**真实 argv** 与**产物路径**分别断言
   （`cases/` 存在、`cases/cases` 不存在、`--work` 实参等于 sc_base）。

### 12.3 自查发现并修掉的一处真缺陷（不在四项 review 里）

两个 runner 都把**所有用例包在同一个 try** 里。M8 变异实测 hit=0/2 "未翻红"，
查报告才看清：新加的用例在断言前先 `duplicateIds[0]` **索引越界抛异常**，
异常中断了后续 `test_file_level`，`dup.exit` / `dup.code` **根本没跑到**——
看起来像"没翻红"，其实是"没执行"。

改两处：（a）缺数据时断言缺失、不崩；（b）runner 逐个用例包一层，异常记为
`crash.<用例名>` 判红并继续跑后面的用例。**异常不是判据，也不该吃掉别的判据。**

### 12.4 实测结果（本机，Windows，Python 3.11.15）

| 项 | 结果 |
|---|---|
| `test-record-command.py` | **185** 项全绿（原 162 + 23 新增） |
| `test-verify-evidence.py` | **152** 项全绿（原 125 + 27 新增；§12.7 又补了一条，此前实测为 151） |
| `test-evidence-review.py`（unittest） | **7** 项 OK |
| `test-record-command-review.py --out <dir>` | **32** 项全过 |
| `mutate-evidence-tools.py` | **19/19** 翻红，`sourcesUnchanged=true` |

M18 4/4、M19 5/5、M17（换锚点后）2/2。

### 12.5 本轮自伤一次：清理删掉了上一次运行的 raw

自用闭环时把 `rm -rf <work>/closure` 和重新录制写在**同一条命令**里，
第二次运行把第一次那 5 行的 case 目录（含 stdout.raw/stderr.raw）删了。
结果 `commands.jsonl` 里 5 行引用了不存在的 raw，校验器会报 `raw-file-missing`。

处理：**不删行、不补时间、不重算哈希**（台账不可覆写、首败独占保留），
另起 `commands-final.jsonl` 跑干净闭环，并在交付材料里原样记录这次事故——
包括"第一次运行的 rc=1 原因现已不可考（raw 已被我自己删掉），不推测"。

### 12.6 边界与未做

- 非 Windows **维持 fail-closed**，不加便携降级；Job/raw/锁生产助手**只读**未改。
- 公共锁 `heavy.lock` 本轮由 zcode 持有（R9-B-probes，token `c3c44168…`），
  本线**未获取、未删除、未修改**；本轮也没有 Go 重负载（本线只改 scripts/ 与 docs）。
- 主仓 WIP：默认 `git status --porcelain` 63 条状态条目，`--untracked-files=all` 展开
  为 93 个文件——两者计数对象不同，**不能用 63 推断 93 个文件消失**（此前写"93 变 63"是错的）。
- 主仓合并与推送仍由协调者；本线不抢全局索引与 Bug 编号。

### 12.7 第二次自查发现：非空 work 目录下会读到上一次的报告

自用闭环重跑时 `test-verify-evidence.py` 报 `agg.cross-file-dup-exit got=2 want=1`。
根因不是聚合逻辑，是**用例本身假定 work 目录是空的**（文档也这么写），但没人强制：

- `test_aggregate` 的 `--json` 路径 `agg.json` 已存在 → 校验器按设计拒绝（退出码 2）。
  这条还只是"红得莫名其妙"。
- `half-report.json` 更隐蔽：拒绝之后 `open(path)` 读到的是**上一次**的报告，
  `bytes.half-line.unterminated` 于是拿**陈旧文件**当本次证据，看着还是绿的。

这正是本轮要修的那类毛病（判据必须来自本次真实产物），只不过发生在测试自己身上。
修法：新增 `fresh_path()`（只删本用例自己上一轮写的那几个报告文件），
并补 `bytes.half-line.report-fresh` 断言本次调用确实写出了报告（退出码 1）。
该修复提交为 `6e96448497366fe461e8c8301be2ba39399c704b`（只改
`scripts/test-verify-evidence.py`，所以 docgate 要求它出现在本文件里）。

判别力：同一 work 目录**连跑两次**，两次都 152 项全绿；修复前第二次必红。
这条判据是"重跑型"的——单次绿证明不了它，必须至少跑两遍同一个目录。

顺带记一次事故（§12.5）：第一次自用闭环（沙箱内运行）5 行的 case 目录被我自己的
`rm -rf` 删掉，`commands.jsonl` 因此引用不到 raw。第二次（沙箱放行后重跑）
`f2-selftest-verify` 又因上面这个缺陷 rc=1。两次失败都**原样保留在台账里**，
没有删行、没有补时间、没有重算哈希；收口结论以重新录制的干净台账为准。
