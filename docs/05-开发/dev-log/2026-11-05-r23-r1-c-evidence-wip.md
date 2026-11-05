# 2026-11-05 R23-R1-C EVIDENCE-AND-WIP-DISPOSITION（WorkBuddy）

> 基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`；分支
> `codex/r23-r1-workbuddy-evidence-wip`；独立树
> `C:/Users/admin/.codex/worktrees/r23-r1-workbuddy/Goxml`。
> 交付目录 `D:/Codes/Goxml-agent-rounds/2026-11-05/product-r23/closeout-r1/workbuddy/`。
> 主库 66 条 WIP、索引、他人树与旧 raw 全程只读；不合并、不推送、不改 Git 设置。

## 0. 提交链（短 SHA，供 dev-log 覆盖门禁核对）

| 序 | 短 SHA | 完整 SHA | 内容 |
|---:|---|---|---|
| 1 | `1085637` | `1085637b147ccfd42a9883e5e9ce732a78ac5dee` | C1：13 项证据资格复核正式文档 |
| 2 | `8a0ad86` | `8a0ad866e38dae5d904cf93e1c42685a9d30ca39` | C2/B1：29 份测试矩阵原始日志按字节归档 |
| 3 | `fd7a69a` | `fd7a69a225e9cc52bff47108bae9b13e6d1378eb` | C2/B2：12 份历史工作记录/报告按字节归档 |
| 4 | `52f2e61` | `52f2e618f7b2905c1ca19ebe42d29ab7eba7bd1e` | C2/B3：2 份治理草稿移位 + 归档索引 README |
| 5 | `8826b8c` | `8826b8c81fa911eb671980ae2e4689bec38e1b14` | C2/B4：README/CHANGELOG/文档索引的用户独有内容 |
| 6 | `8230374` | `8230374c1d088ea46085249d0daa1357722d03c8` | C2/B3-fix：归档目录 `.gitattributes` + CRLF 原件按原始字节重写 |

> 序 6 在序 5 之后提交，是发现序 4 的 CRLF 归一化后追加的修正；Git 历史顺序为
> `1085637 → 8a0ad86 → fd7a69a → 52f2e61 → 8826b8c → 8230374 → <C3 readyRef>`。

## 1. C1 13 项证据资格复核

按 E0（无执行记录）/ E1（实现方自验）/ E2（版本化报告）/ E3（独立复核或本轮标准台账重放）
分层，逐项给终结论，不再放回「待复核」。逐项机器可读字段在交接包
`raw/c1-evidence-qualification.json`，正式文档见
[13 项证据资格复核](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)。

- 机械核对：13 卡引用的 51 个测试选择器 51/51 命中；`验收用例.md` 命令列 32 个 `-run`
  名称中 11 个在基线源码不存在，按原命令执行会「零测试 + ok」。
- 标准台账：C1 共 9 行（先 5 条无 `-v`，判据不足；补跑 4 条 `-v`），两组原始字节都保留。
  `-v` 四组合计 **91 PASS / 0 SKIP / 0 FAIL / 0 no-test-files**（编排 8、载荷 13、
  Parser 7、Input 63）。现行窄重放只是适用性检查，不追认 2026-10 历史验收。
- 汇总：7 项有通过记录（限声明/文档冻结范围）、6 项未闭环、1 项本卡无法核实；
  `TASK-PARSER-002` 双重归类，三桶和 14 > 13。**不写完成百分比**。
- Input 按 2026-11-02 已批准的用户编辑优先规则核对，旧 D4 顺序只作历史；
  D5-F 原生焦点/IME 明确未执行，不计通过。未开原生窗口、未重跑产品全仓 `go test ./...`。

## 2. C2 66 条 WIP 的实际处置

按 `git status --porcelain=v1 -z -uall` 逐路径复算 = 66 条（18 `M` + 48 `??`，61 条在 `docs/`），
与 `input/wip-snapshot.json` 及 `input/wip-bytes/` 冻结原件一致；索引空。
工具 `tools/build-disposition-r1.py` 生成 `raw/disposition-66.json`（98 条 hunk/whole-file
记录，原文在 `raw/hunks/`）；`tools/materialize-batches.py` 从冻结原字节物化 B1–B4。

- **候选提交 46 条**：B1 29（原始日志逐字保留，0 B / < 200 B / FAIL 日志不追认 PASS）、
  B2 12、B3 2（移位到 `docs/05-开发/历史归档/`，明确非现行规则）、B4 3（三条已归并未提交
  文档按 WIP 原字节整份承接，保留 HEAD 修复与用户独有内容）。
- **明确保留 20 条**：逐条给具体事实理由（重复/冲突的候选期架构增量、未批准 ThemeRef
  提案、3 个 Go WIP、候选期卡片与测试文档），见交接包 `retain-list.md`。
- **3 个 Go WIP**：只在 `raw/shadow-go/` 影子副本做逐 hunk 审查与窄测；`contracts` 2 PASS /
  0 SKIP / 0 FAIL rc=0，两个 `go build` rc=0，`go vet` rc=0；**未执行 `concurrencyprobe`
  二进制**（设计为崩溃）。不混入文档 readyRef，不改他方生产代码。
- **B3 字节异常与修复**：序 4 提交后回读发现
  `docs/05-开发/历史归档/GXML 项目质量审计统一交付协议 V2.md` 冻结原件 4280 B /
  `908edb14…` 与提交 blob 4167 B / `566f1b58…` 不符。根因是根 `.gitattributes` 的
  `* text=auto eol=lf` / `*.md text eol=lf` 把 113 处 CRLF 归一化，工作区文件仍是原字节，
  故 `git status` 为空。序 6 在归档目录新增 `.gitattributes`（`* -text -eol`）并用
  `git add --renormalize` 重写 blob。修复后 `tools/verify-c2-commits.py` 实测
  **47/47 文件**（46 原件 + 1 归档索引）提交 blob、工作树、冻结原件三者 SHA-256 一致，
  `raw/c2-commit-byte-verify.json` 的 `problems=[]`；B1 的 29 份原始日志从未受影响。

## 3. C3 一致性文档与归并包

- 更新 [项目状态总览](../../04-WBS/2026-11-05-R23-项目状态总览.md)：§0 补 E0–E3 证据层、
  §2.3 换为 13 项终结论表、§2.5 去掉完成百分比改为证据层分布、§6.1 登记现行 66 条。
  **49 叶子分母、3 个父任务不计、12/16/13/8 分桶均不变**，不擅自重分桶。
- 新增 [R23-R1 WIP 候选提交与归并计划](../../04-WBS/2026-11-05-R23-R1-WIP候选提交与归并计划.md)：
  精确提交 SHA、逐路径动作、冲突、顺序、回滚原件与窄门禁。
- 13 张任务卡各追加「R23-R1 证据资格复核补记」，**不改历史原文、不改状态与判据**。
- WBS 总表抬头、文档索引、README、CHANGELOG 追加 R23-R1 现行入口；不覆盖用户独有内容。
- 交接包按当前 66 逐路径重建 `apply-list.json` / `merge-plan.md` / `overview.md` /
  `retain-list.md`；不沿用旧 78 条目 / 73 唯一路径表。

## 4. C3 门禁实测（标准台账 `raw/commands.jsonl`，18 行）

| 台账 id | 真实 argv（摘要） | rc | 原始输出 |
|---|---|---:|---|
| `r1c-c3-docgate` | `go test -count=1 -timeout 10m -v ./internal/docgate/`（CGO0、`-mod=readonly`） | 0 | 13 个顶层 `--- PASS` / 0 `--- FAIL` / 0 `--- SKIP`；`ok … 0.311s` |
| `r1c-c3-diagdoc` | `go test -count=1 -timeout 10m ./internal/compiler/diagdoc/` | 0 | `ok … 0.135s`，stdout 61 B |
| `r1c-c3-cgo0-build` | `go build ./...`（`CGO_ENABLED=0`、`GOFLAGS=-mod=readonly`） | 0 | stdout/stderr 均 0 B |
| `r1c-c3-wip-final` | `tools/wip-r1-verify.py --repo D:/Codes/Goxml --input ../input --out raw/wip-r1-final.json --compare raw/wip-r1-open.json` | 0 | `WIP-R1-RECOMPUTE OK added=0 removed=0 changed=0` |

- docgate 覆盖：本地 Markdown 链接（407 份 Markdown、752 个行内本地链接目标）、
  WBS 任务卡与依赖无环、dev-log 提交覆盖、主题/字体边界与验收命令非空转。
- 收口复算 `raw/wip-r1-final.json`：`head=bab7a35f`、`total=66`、`frozenTotal=66`、
  `indexPathCount=0`、`problems=[]`；与开工快照四元组比较 `added/removed/changed=[]`，
  **主库 WIP 未被本线改动，也没有他方新变更**。
- 标准台账校验：`verify-evidence.py --jsonl raw/commands.jsonl --json raw/verify-evidence-c3.json`
  实测 `verdict=PASS exit=0`，`lines=18 pass=18 fail=0 incomplete=0 unsupported=0`，
  `bytes=61711 sha=1c9ef5add11b…`。这是**随仓校验器的实测结论**，不是手写索引。
- 本轮**无 Go 产品代码改动**：未重复全仓产品 `go test ./...`、未跑 `gofmt -l .`；
  C2 的 3 个 Go WIP 只在影子副本做过 `go test` / `go build` / `go vet`（台账 `r1c-c2-shadow-*`）。

## 5. 边界

- 主库 WIP、索引、他人分支、旧 raw 只读；三线并行、不等别线外审；公共锁一层持有，
  只释放自己 token。
- 不替 DeepSeek 写字体差距/执行方案/theme 测试结论，不替 ZCode 写预算补证完成状态；
  不批准 ThemeRef 语义，不擅定阈值或预算语义。
- 本轮无 Go 产品代码改动，不重复全仓产品测试；门禁按真实影响执行。
- 未安装、未启动原生窗口、未采样；作者只冻结 readyRef，主库合入与准确 SHA 远端发布
  由协调者统一完成。
