# R11-C 一键工程入口（workbuddy 线）

- 任务包：`D:/Codes/Goxml-agent-rounds/2026-11-03/product-r11/`（`common-task.md` /
  `workbuddy/task.md` / `input-manifest.json`，7 项输入逐字节核对 **ALL_MATCH**）
- 固定基线：`9dfce78fa622963bf86869bb7b6dee22df261a82`
- 工作树：`C:/Users/admin/.codex/worktrees/product-r11-workbuddy/Goxml`
  分支 `codex/product-r11-workbuddy-20261103`；不 push、不合主仓
- 提交：`2729fc5`（C1 复制与所有权）→ `de63beb`（C2 生成/构建/测试闭环）→
  `4bfffb0`（C3 两路径验收、边界、变异、README）→ 本 dev-log

## 做了什么

把 `examples/gxml/testprojects/crosspackage-preferences` 从「照教程手抄」变成
「跑一条命令」，但不新增第六个 `-Example`、不改 CLI/Compiler/runtime/正式契约：

| 文件 | 职责 |
|---|---|
| `scripts/verify-projects.py` | 仅标准库；按脚本位置定位仓库；复制模板、替换占位符、独占创建输出目录、真实构建 CLI、逐 View 生成、只构建窗口入口、可选离屏测试 |
| `scripts/test-verify-projects.py` | 19 条专属用例；含影子仓库、变异生成（`--make-mutated-repo`） |

复用的只读助手（未改动一行）：`scripts/verify_examples_proc.py` 的 `run_capture`
（进程树收口 + raw 捕获）、`scripts/verify-examples-lock.py` 的 `Lock`
（按 token 释放）、`scripts/record-command.py`（外层台账行）。

## 实测数据（都是本轮真实命令输出）

| 记录 | id | 结果 |
|---|---|---|
| 全流程（C2） | `r11c-c2-full` | rc=0，businessPass=true，9.828 s |
| 验收路径 A（空格+中文） | `r11c-c3-path-a` | rc=0，9.843 s，6 阶段，11 用例 PASS / 0 SKIP |
| 验收路径 B（含 `--check`） | `r11c-c3-path-b-check` | rc=0，9.266 s，6 阶段，11 用例 PASS / 0 SKIP |
| 19 例完整回归 | `r11c-c3-tests-19` | rc=0，34.203 s，19 passed / 0 skipped |

- 台账：`commands.jsonl`（4 行），`verify-evidence.py` → `verdict=PASS`。
- 外层台账行的 `lockReleased` 是 `null`（**故意**）：脚本内部自持公共锁，
  外层按「不嵌套取锁」纪律不加锁；锁是否释放看脚本自己报告里的
  `lock.released=true`，以及运行后锁文件确实不存在。

## 几个必须说明的判断

1. **锁在创建目录之前拿。** 锁忙时退 8，连输出目录都不创建——「他人持锁时
   零副作用」比「先建目录再退」更符合「不碰别人的东西」。
2. **注释行不参与占位符替换。** 模板里 `// 并把 __MODULE__ 换成 ...` 是给读模板
   的人看的说明，替换会把它改成一句自相矛盾的话。代码行照替。
   （`go.mod.txt` 属于模板材料、按所有权条款未改；它讲的是手工路径，
   README 已注明入口脚本写的是转义后的原生路径。）
3. **`go.mod` 写原生绝对路径 + Go 转义**（`"C:\\Users\\..."`），不再把反斜杠折成
   `/`——折斜杠正是手工步骤要人做的事，工具的存在意义是免掉它。用
   `go mod edit -json` 实测能解析回 `C:\Users\...`（用例 `gomod-parses`）。
4. **不信 `go test` 的 rc=0。** pass<1 / 有 SKIP / 有 FAIL / no test files 一律判
   不成立（`test-verdict-unit` + `boundary-*` 双向覆盖）。
5. **影子仓库整份复制仓库（不含 `.git`，约 9.5 MB）。** 拼最小材料会让
   `build-cli` 先失败，那样测到的就不是要测的那一步。

## 判别变异（都实测变红，可复现）

| 变异 | 改了什么 | 变红的用例 |
|---|---|---|
| `ownership` | 「已存在即拒绝」→ `os.makedirs(exist_ok=True)` 直接写 | `refuse-empty-dir`、`refuse-symlink` |
| `success-on-failure` | 抹平阶段退出码判据（`if res['rc'] != 0` → `if False`） | `boundary-generate-fails`、`boundary-build-fails` |

复现：`python scripts/test-verify-projects.py --make-mutated-repo <dest> --mutate <name>`，
再 `--repo <dest>` 跑对应用例。两条变异都**编译通过**（`if False:` 分支保留），
不是靠语法错误变红。

## 边界（不夸大的部分）

- 窗口入口**只构建、不运行**；测试断言入口二进制从未作为任何阶段的 `argv[0]`。
- 原生窗口与输入法**没有**自动验证；离屏测试只覆盖 View 语义。
- `--check` 只记录「已转发」，`passed` 恒为 `null`。默认流程不依赖它。
  A 线已有 `c263af0`（写盘前编译检查、成功仍写盘），与本工具转发后继续 build
  兼容，但**未由协调者冻结**，因此本线不宣称新 `--check` 已验收。
- Python 入口只在本机 Windows + 本机 Go 工具链实测，不宣称 PowerShell/Linux/macOS 全覆盖。
- 生成过程会在工程内写 `.goxml/`（CLI 自己的缓存与 manifest），这是 CLI 行为，
  不是本工具新增的目录。

## 未做 / 留给下一轮

- B 线（ZCode）评审：截至本轮结束时 `product-r11-zcode/Goxml` HEAD 仍是基线
  `9dfce78`，工作树有 2 个未提交修改（`codegen.go`、`local_static_subtree_test.go`），
  `product-r11/zcode/` 只有 `prompt.txt`/`task.md` —— **无冻结 SHA，记 PENDING**。
- 跨线组合（A 的 `--check` + B 的 codegen 变更 + 本入口）由协调者统一复验。
