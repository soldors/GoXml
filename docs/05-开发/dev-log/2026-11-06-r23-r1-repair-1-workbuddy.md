# 2026-11-06 R23-R1-C 集中返修 R1（WorkBuddy）

> 父提交 `afc4cc22f7e58ec9540e9ac56b9cfe9359e2f838`；分支
> `codex/r23-r1-workbuddy-repair-1`；独立树
> `C:/Users/admin/.codex/worktrees/r23-r1-repair-1/Goxml`。
> 交付 `D:/Codes/Goxml-agent-rounds/2026-11-05/product-r23/closeout-r1/workbuddy/repair-r1/`。
> 协调者材料、审查树与上轮 `raw/` 全程只读；主库 `fa0f9c5`、66 条 WIP 原字节不变。

## 0. 提交链（短 SHA，供 dev-log 覆盖门禁核对）

| 短 SHA | 说明 |
|---|---|
| `afc4cc2` | 父提交：R23-R1-C C3 一致性文档（本轮返修的起点） |
| `8230374` | C2/B3-fix：归档目录 `.gitattributes` + CRLF 原件按原始字节重写 |
| `8826b8c` | C2/B4：README/CHANGELOG/文档索引的用户独有内容 |
| `52f2e61` | C2/B3：2 份治理草稿移位 + 归档索引 |
| `fd7a69a` | C2/B2：12 份历史记录逐字归档 |
| `8a0ad86` | C2/B1：29 份原始日志逐字归档 |
| `1085637` | C1：13 项证据资格复核 |
| `bab7a35` | R23-C 合并后的主基线 |
| `0fbf7d8` | 证据分类与运行时指引更正 |
| `788bf32` | R23-C 项目状态与 WIP 处置记录 |

主库 HEAD 由协调者接纳 A 补证推进到 `fa0f9c5`（新增 ZCode dev-log 1 文件），
属正常进度；不回滚主库，不把上轮 `headSame=True` 当目标。

## 1. 协调者 BLOCK 与三项关闭

`coordinator/review-c.md` 判「归档字节子项 PASS、整体证据资格文档 BLOCK」：

- **已证子项**：66 输入完整覆盖快照；46 个提交输入在声明批次 blob 逐字等于原件；
  终态 43/46 相同、3 个 B4 为明确扩展；目录 `.gitattributes` 的 `text`/`eol` 均 unset；
  docgate/diagdoc/CGO0 build 均 rc=0。
- **F-C1（P1）**：`go test -list <pattern> ./internal/compiler/parser` 三条均 stdout 只有
  包级 `ok`，无测试名；全仓符号扫描的「51/51」「1/1/18/41」不是指定包证据。
- **F-C2（P2）**：7+6+1=14 双计；12/16/13/8 不能同时当当前状态分布。
- **F-C3（P2）**：ORCH「部分独立」只有另一份文档，无执行者/会话依据。

## 2. F-C1 真实包重核

- `go list` 得 **15 个含测试文件的包**，逐包 `go test -list .`（标准台账
  `r1r-c1-go-list` + `r1r-list-*` 共 16 行）得 **1518 个顶层入口**。
- 51 个选择器：**47** 个精确顶层名且各只落在 1 个包；**4** 个前缀。
  旧「1/1/18/41」的真实出处为 `TestEdgeInsets` 1（contracts）、`TestColor` 1（expander）、
  `TestBinding` 18（contracts 3 + codegen 15）、`TestEvent` 41（contracts 1 + analyzer 19 +
  codegen 9 + expander 12），台账 `r1r-real-*`；**parser 包 0**。
- PARSER-002 三条卡内命令在 `./internal/compiler/parser` 的 `-list` 均 0 个名字；
  `-v` 实测 `testing: warning: no tests to run` / `[no tests to run]`（`r1r-empty-parser-*`）。
- `验收用例.md` AC-001-01..04 命令列含 Markdown 转义 `\|`，Go `-run` 视为字面竖线 →
  同样 0 个名字（`r1r-empty-ac0010*`）。旧「11 个 `-run` 名称不存在」也不准确：
  `TestInputSameFrameConflict` 是有效前缀，命中 `TestInputSameFrameConflictRuntime`
  （`r1r-real-prefix-sameconflict`）。
- 正则/前缀按 Go `-run` 分段非锚定语义与包路径一起解析，机器可读在
  `repair-r1/raw/real-test-inventory.json`；禁止全仓正则、注释字符串、同名他包、内部子测试。
- **计数口径更正**：上一版「91 PASS」实为顶层 **65**（8+9+7+41），含子测试共
  **91 条 PASS 行**（8+13+7+63）；两个口径分列，不混称。

## 3. F-C2 / F-C3 结论修正

- 13 项每项只占一行一个互斥状态：**A 6 / B 6 / C 1 = 13**。
  `TASK-PARSER-002` 的「本卡证据不足」与「相邻 AC 有限文字记录」是同一行不同字段；
  相邻 AC 表命令列同样含 `\|` 为空跑，登记表是文字不是命令执行记录，不凭它替整卡验收。
- **49 叶子分母、3 个父任务不计、父子不重复加总不变**；12/16/13/8 明确为
  **R23-C 审计队列历史分组**，不作本轮当前状态分布；其余 36 项不在本轮范围。
  文档冻结、限定范围通过记录、现行功能验收分开，不写完成百分比。
- 13 项历史执行方独立性一律 **未证（缺具名执行者/独立会话记录）**；ORCH「部分独立」
  撤销；其余 12 项的「否/同一会话」只是归属线索。本轮标准台账 `-v` 重放为 E3 可复跑
  原始证据，**另列**，不自动等于独立历史验收。

## 4. 逐项文档补丁

- 重写 [13 项证据资格复核（R1 返修补正版）](../../06-测试/2026-11-05-R23-R1-13项证据资格复核.md)：
  §0.1 换成真实包核对、§1 每项一行、§2 互斥 6/6/1、§4 可复核入口。
- 13 张任务卡：修正 C3 补记里被撤销的字段，各追加「R23-R1 返修 R1 补正」段
  （单一状态、原命令有效性、另选命令、真实用例集合、已证/未覆盖、独立性、E3 重放）；
  卡内历史原文不动。
- 总览 §0/§2.3/§2.5/§8/§9、WBS 总表抬头、文档索引、CHANGELOG、
  R23-R1 归并计划 §5/§6 同步更正。

## 5. 归档字节子项（保留，不重做）

| 口径 | 结论 |
|---|---|
| 协调者独立核实 | 46 提交输入在声明批次 blob 逐字等于原件；终态 43/46 相同、3 个 B4 明确扩展 |
| 作者 47 口径 | 46 份 WIP 原件 + 1 份新建归档 README（不同口径，不混称） |
| C2 末态 `8230374` | **47/47** 与冻结原件一致 |
| ready `afc4cc2` | **44/47**，3 条 `expectedDifferences`（B4 有意扩展） |
| 本轮 | 不重做 46 份归档、不改归档目录 `.gitattributes`；复算原件 blob 与声明的有意扩展，见 `repair-r1/raw/byte-recount.json` |

**不把本轮新增文档追加伪称原字节一致**。

## 6. 门禁与边界

- 本轮新录标准台账 `repair-r1/raw/commands.jsonl`（含 15 个包 `-list`、空选择器
  `-list`/`-v`、真实前缀 `-list`、9 组 `-v` 精确名重放、docgate/diagdoc/CGO0 build）；
  失败与空跑原始输出**全部保留**。
- 未改 Go 产品代码、字体测试、预算实现或正式语义；未开原生窗口、未采样、未安装、
  未定阈值；公共锁一层持有、只释放自己 token。
- 主库 66 条 WIP 与冻结原件逐字节一致（`repair-r1/raw/wip-main-repair-open.json`），
  只读；不合并、不推送、不改 Git 设置，不等待 DeepSeek 冻结。
- 冻结后停止，新 readyRef 交协调者独立复核。
