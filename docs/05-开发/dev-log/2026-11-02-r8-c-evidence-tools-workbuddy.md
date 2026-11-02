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
