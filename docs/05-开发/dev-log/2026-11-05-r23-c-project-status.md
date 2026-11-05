# 2026-11-05 R23-C PROJECT-STATUS-AND-WIP（WorkBuddy）dev-log

基线 `369703bb906058caaffe7bada4e80e92775e71ea`（本地 master；R23-A/B 接纳后）。
树 `C:/Users/admin/.codex/worktrees/r23-workbuddy-project-status/Goxml`；分支
`codex/r23-workbuddy-project-status`。主库与用户 WIP **全程只读**，未推送、未合并。

三个切片（逐文件 `git add --`，`GOXML_AUTO_PUSH=0`，临时空 `core.hooksPath`）：

| 切片 | 提交 | 内容 |
|---|---|---|
| C1 任务全集与 WIP 身份 | `e78e88e24b40f9a8c92c7a7c10819b6913d7facd`（`e78e88e`） | `docs/04-WBS/2026-11-05-R23-项目状态总览.md`、`docs/04-WBS/2026-11-05-R23-WIP处置与归并说明.md` |
| C2 正式进度文档 | `10a5855fc7471d161900c2452354a55073c110a8`（`10a5855`） | 需求清单 / WBS 总表 / TASK 总账 / 三张任务卡 / 文档索引 / README / CHANGELOG |
| C3 处置包与收口 | 本文件所在提交（HEAD，docgate 对 HEAD 有意豁免） | 本日志 + 交付目录里的 disposition / apply-list / merge-plan / handoff |

> dev-log 门禁用 `git log --format=%h` 的短 SHA 做包含匹配，所以两种写法都留在文本里；
> 交接包 `slices.json` / `commands.jsonl` 一律用完整 SHA。

## C1 锁定任务全集与 WIP 身份

- 起点：新基线版本化的 WBS（51 行 / 51 卡）、需求清单、TASK 总账、README、CHANGELOG、
  文档索引与 Git 历史；结合 R18–R23 的真实接纳记录（`state.json` 与 `coordinator/*/review.md`）。
- **R16/R17 清点工具未进基线**：`977e2ce` 的 `scripts/delivery-recovery/inventory_local_files.py`
  只在 `codex/product-r16-workbuddy-20261105`、`codex/product-r17-workbuddy`、
  `codex/r16-delivery-coordinator` 上（`git cat-file -e 369703bb:<path>` 失败）。
  本轮把该工具与 `build-apply-list.py` 从隔离分支**只读提取到交付目录 `tools/`**，外部复用，
  **没有整包迁回仓库**；复用来源的 SHA-256 与行数留在交接包。
- WIP 逐路径枚举：`git status --porcelain=v1 -z -uall` → **68 条**（19 `M` + 49 `??`），
  其中 **63 条在 `docs/`**，索引空。「porcelain 40」是不同枚举口径，本轮不推断差异原因。
  每条记状态、HEAD blob、工作区字节 SHA-256/大小、Markdown 引用位置、日志族与失效引用；
  开工与收口各一次指纹快照，按四元组逐条复算（`raw/wip-fingerprints-*-comparison.json`）。
- 发现 R17 工具已覆盖「双哈希 + 引用位置」，本轮只补它缺的**末尾复算**
  （`tools/wip-recompute.py`）与**五类处置/三方重叠**（`tools/build-disposition.py`、
  `tools/review-image-card.py`）。三个脚本都只在交付目录，不进仓库。
- 四状态分别记：已实现（基线有代码/文档/测试）· 已验收（有版本化独立结论）·
  已合入（`merge-base --is-ancestor` 实测）· 已推送（是远端 `a8a40e3` 的祖先）。
  本地领先远端 **7** 个提交；R23 的 7 个均**未推送**。
  预算候选 `35c9ff3` 经祖先判定**不在本地 master**。
- 7 处来源冲突逐条点名（SC-01..07，见 `raw/acceptance-chain.json`）：Input 未实现、
  Router 未实现、ADR-0015/Image 未批准未合入、TASK总账 50 vs WBS 52、CHANGELOG 10-19 条目、
  比较机制/真实 App M1「从未实现」、WIP 枚举口径。

## C2 修正过期事实

- 修订 9 个版本化路径（8 改 + 1 新增），全部采用「顶部追加 + 按日期标注」，
  **历史原文不删**：需求清单 REQ-002/003/004；WBS 抬头、`TASK-ROUTER-001` 行、
  新增 `TASK-IMAGE-001` 行、误解表；`TASK-ROUTER-001` / `TASK-FONT-001` 卡；
  TASK 总账追加现行快照章节；文档索引顶部 R16–R23 入口与现行声明；README 与 CHANGELOG。
- 补齐的现行事实：Image v1（ADR-0015 已批准、R18 pass、R20 合入发布）、
  Router 基础与 `app.WithOnDestroy(r.View(), r.Shutdown)` 关闭接线（R20 B1/B2）、
  CLI/缓存/overlay（R7–R15/R21-A/R22-A）、三个跨包工程（R10/R15/R21-C）、
  随仓验收入口（R11/R22-C）。**MacroOp 转场与 OSWindow 逃生接口仍未实现**。
- 字体按 R23-B 已接纳范围：共享 Provider `loadEmbedded` 互斥修复可接纳，
  **整体正式验收未通过**。比较机制与真实 App M1 已有历史证据，不再写「从未实现」；
  第 20 版 90 次历史样本 lazy 最终就绪 median 比 sync 慢 **14.17%**，**本轮无新性能采样**。
  旧时钟断言 `TempElapsed=0`（基线专项 100 次 4 FAIL、整包复验通过，原 `>0` 断言未改）
  明确登记，**未改产品或测试去隐藏**。
- Router 预算按 R23-A 裁决：ready `9268601` 仅设计与记录，生产代码等同 `a8a40e3`；
  candidate `35c9ff3` 技术范围通过、**语义未批准、仍隔离、未进主库**；主库沿用同帧排空。
- AOT Diff 明确为**已批准推迟项**；Watch/Preview、系统交互、组件库、强类型 ThemeRef
  按事实登记为未实现/未批准。
- 可审计总览：顶层目标 6 个、叶子分母 **49**（WBS 52 行 − 父任务 3 个；
  `TASK-FONT-001-D` 亦为父，不重复加总），验收完成 9 / 未闭环 10 / 无法核实 22 /
  未开始 8；只对有版本化证据的项计验收完成，22 项无独立验收原件**单列**。
  证据分级把「独立复核」与「版本化历史记录（本轮未重放 stdout）」分开。
- `TASK-IMAGE-001` 卡：主库未跟踪草稿（51 行，sha256 `3d30b012…`）**只读**；
  6 个 hunk 锚点各自唯一命中后形成独立树可审查版本（sha256 `3c7939fc…`），
  unified diff 与每 hunk 元数据留在 `raw/hunk-diff-image-card.txt` /
  `raw/image-card-hunks.json`。**主库草稿字节未变**。

## C3 WIP 与发布文档处置包

- 68 条逐项分五类：可原样接纳 5 / 需合并 17 / 依赖功能批准 5 / 历史证据保留 37 /
  来源内容待核实 4；逐项给精确路径、HEAD 与工作区双侧 SHA-256、大小、引用数、依赖、建议。
- 复用 `build-apply-list.py` 生成 **78 条** apply 清单（68 WIP + 10 个本轮文档候选）。
  工具输出的历史标签（`r16c-apply-list-1` / 「R16-C C4」）已在交付说明里标注，
  原始未加工输出保留在 `raw/apply-list-with-drafts.md`。
- **历史日志原件全部按字节保留**。空日志 `t20_fmt.txt`（0 B）与另 9 份 < 200 B 日志
  不能撑起 PASS；4 份带 FAIL 标记按原文保留、不追认。
- 安全归并清单与冲突清单：`merge-plan.md` 定位 **5 条**与本轮文档补丁重叠的 WIP
  （`README.md`、`CHANGELOG.md`、`docs/00-文档索引.md`、`docs/04-WBS/WBS总表.md`
  为 `M`；`docs/04-WBS/任务卡/TASK-IMAGE-001.md` 为 `??`），给三方 SHA-256 与
  逐 hunk 归并建议。**本线不归并主仓 WIP，协调者负责实际归并。**
- 未改任何 Go 产品代码、预算实现、字体生产实现或 Git 认证/系统设置。
- 门禁：`go test ./internal/docgate -count=1`（链接 / WBS 卡与依赖无环 / dev-log 覆盖）、
  `internal/compiler/diagdoc`、`CGO_ENABLED=0 GOFLAGS=-mod=readonly go build ./...`；
  docgate 实测扫描 386 份 Markdown、672 个行内本地链接，零断链；WBS 52 行 / 52 卡零孤儿。
  本轮**文档 only**，未跑全量 `go test ./...`，不声称测试覆盖。

## 五态与边界

| 态 | 状态 |
|---|---|
| 实现 | 完成（3 个提交，工作树 clean） |
| 自测 | 完成（docgate / diagdoc / build + 逐路径指纹复算 + 78 条 apply 清单） |
| 外审 | 未开始（交协调者） |
| 主仓 | 未合入（隔离分支）；68 条 WIP 前后字节一致 |
| 远端 | 未推送；`git ls-remote --heads origin master` 实测 `a8a40e3`，本地领先 7 |

未验证边界：原生窗口、跨平台（仅 Windows）、字体 M1 阈值与第二个成功 family、
完整 GUI 性能验收、Router 预算语义批准、预检/委派之外的并发 WIP 变更。
本轮没开窗、没采样、没跑 Go 测试套件、没安装、没改系统设置。

## 协调者复核补正

原文9/10/22/8为作者初版归类，现由总览的12/16/13/8取代。Analyzer、绑定运行时读取、S2已有历史验收报告；6项卡内明确部分/条件/冲突状态不归“无证据”。新统计仅为记录分类，非项目完成率，不升级为本轮独立验收。原交付raw与清单保留不回改。
