# 2026-11-03 R11-A PRECOMMIT-CHECK（DeepSeek）dev-log

基线 `9dfce78fa622963bf86869bb7b6dee22df261a82`；树 `product-r11-deepseek`，分支 `codex/product-r11-deepseek-20261103`。
只改 internal/cli（pipeline.go 接线 + 新 precommit_check.go/测试）+ 本线 dev-log；未改 Compiler/contracts/scripts/examples/事务算法/缓存格式，未新增 CLI 选项或退出码。

## 差距（先立红，raw/A1-counterexample.json）

`--check` 此前**只被解析**：`cli.go` 帮助写"提交前执行 package/build 检查"，而 `runPipeline` 注释自认"`--check` 暂与默认行为相同"。

真实反例（坏候选被接受）：目标包 `ui` 内已有用户文件
`use.go: var _ = NewGoxmlHomeView(nil)`，而 `home.gxml`（无 ViewModel）生成的
`NewGoxmlHomeView()` 是零参数 ⇒ 两者组合**编译失败**：

```
ui\use.go:3:26: too many arguments in call to NewGoxmlHomeView
        have (nil)
        want ()
```

基线 CLI 在 `--check` 下仍 **rc=0** 并写出 `home_gen.go` 与 Manifest（raw 中 a1b-check-accepts stdout：
`缓存未命中…/已生成 ui/home_gen.go`）。即"提交前检查"承诺未落实。

> 说明：同名符号冲突（`GoxmlHomeView` 重复声明）已被既有 L1 命名预检以 `CODEGEN-NAME-001` 拦下，
> 因此本反例刻意选**类型错误**而非同名冲突，避免与既有预检重合。

## A1 提交前检查引擎（internal/cli/precommit_check.go）

- `checkPrecommitCandidates(goExe, moduleRoot, cands)`：把**本批候选**组合进真实目标包后编译。
  - 未命中：阶段一刚生成、尚未落盘的内存字节，经 `-overlay` 注入 ⇒ 检查看的是**本批新内容**；
  - 命中：产物本次不重写，磁盘字节即本批最终内容（`content=nil ⇒ 读盘`）；
  - 同包多页归并到同一目录组；候选之间的跨包依赖由 overlay 一并可见。
- 检查手段：`go build`（仅编译/type-check）。**不** `go test`/`go run`、不执行测试初始化或用户程序。
- 输出边界：overlay 替换文件、overlay JSON、以及 `go build` 的 cwd 全部在**自有临时目录**；
  cwd 取 module 内的唯一临时目录（`.goxml-precommit-cwd-*`），既让 go 找得到 main module，
  又保证单 `main` 包的可执行输出只落临时目录。结束 `defer` 清理。
- `--out-dir` 首建新目录（目标目录尚不存在）：在 module 内建自有影子目录只看本批候选编译；
  **不**在用户目录临时造包再恢复。该形态下"候选包被其它候选包 import"不覆盖，已在 handoff 声明。
- 失败语义：返回含真实 stderr 的 error（`提交前检查失败：目标包未能通过编译（本批未写入任何产物/Manifest/缓存）`），
  超时 180s 单独报错，下游非零退出。
- 环境：沿用本次生成的权威上下文（`cmdEnvBase()` + `GOTOOLCHAIN=local`，生产不叠加覆写），
  不复制宿主标签逻辑。

## A2 Pipeline 接线（internal/cli/pipeline.go）

位置：**阶段一之后、阶段二写盘之前**（阶段一 B）。因此检查失败时：

- 产物未写、Manifest 未更新、成功缓存记录未写；
- `--clean` 尚未执行（它排在本次登记之后）⇒ 检查失败前不会删除任何文件。

全命中同样检查（不受 `len(preparedFiles) > 0` 影响）；不带 `--check` 的分支完全不变。

## A3 真实 CLI 验收（raw/A3-e2e.json，全 PASS）

| 场景 | 结果 |
|---|---|
| 正常生成 | rc0，产物+Manifest+1 条缓存 |
| 二次 `--check` | rc0，`缓存命中` + `提交前检查通过` |
| 破坏目标 Go 文件（错误签名调用） | **rc2 拒绝**；`home_gen.go`/Manifest 字节不变；缓存条数不变；无"已生成" |
| 修好后 `--check` | rc0，恢复生成 |
| 两个互相依赖的候选包（v2 `use.go` 引用 v1 生成类型） | rc0 **通过**（overlay 让 v1 候选对 v2 可见），两包均写出 |
| `--out-dir` 首建新目录 | rc0 通过，产物落在新目录 |
| 混合命中（改 p2 后） | rc0，同时出现 `缓存命中`/`缓存未命中`/`提交前检查通过` |
| 全命中 | rc0，`缓存命中` + `提交前检查通过`（证明全命中不跳过） |
| 路径含空格/Unicode（`项目 空格`） | 以上主流程即在该路径下运行 |

**变异红**（证明 checker 真被调用、且不是靠墙钟推断）：把接线改成
`if opts.check && len(preparedFiles) > 0 {`（即"全命中跳过检查"）后，
`TestCheckRunsOnAllCacheHits` 立即失败：

```
precommit_check_test.go:124: 全命中批次也必须执行提交前检查，实际 stdout: 跳过 home_gen.go（缓存命中）
```

恢复后全绿。另一组判别断言在 `TestCheckUsesCandidateContentNotDisk` 成对给出：
磁盘坏/候选好 → 通过（overlay 生效）；命中读盘（坏）→ 失败；磁盘好/候选坏 → 失败。
若 overlay 未接线，第一对断言直接变红。

## 检查范围与未覆盖（如实）

- 检查对象是**本批目标包**（含其编译依赖）；不编译 module 内其它无关包。
- 「只在提交时才可能出现的」跨包别名/类型问题由编译器判定；本检查不替代 Analyzer 语义诊断（ROOT-002 等）。
- `--out-dir` 首建新目录时用影子包编译，故"该新包被同批其它包 import"不在覆盖内。
- 不承诺跨文件 OS 原子性/断电恢复；未改 `commit.go`/clean 事务。

## 门禁

`go test ./internal/cli -count=1 -cover` = **86.4%**（≥85%）；`go vet` rc0；`gofmt -l` 空。

## 五态

源实现 done / 自测 pass / 外审 PENDING（ZCode 审 A）/ 主仓未合 / 远端未推送。
