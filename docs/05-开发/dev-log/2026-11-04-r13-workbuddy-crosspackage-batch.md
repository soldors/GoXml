# R13-C：crosspackage-preferences 的一键入口改用一次目录 generate

> 基线 `ed76cbbb353511cc3de94c20b5f6726104ac9842`；工作树
> `C:/Users/admin/.codex/worktrees/product-r13-workbuddy/Goxml`，分支
> `codex/product-r13-workbuddy-20261104`。只本地提交，未 push、未合主仓。
>
> 本线改动只有三处：`scripts/verify-projects.py`（生成阶段改成一次目录命令）、
> `scripts/test-crosspackage-batch-e2e.py`（新增，本线 E2E）、
> `examples/gxml/testprojects/crosspackage-preferences/README.md`（阶段表与手动步骤），
> 另加两个随改动更新的既有测试断言（见 §8）与本文件。**五个内置示例、
> `examples/gxml/testdata`、锁/Job 助手一字未动。**

## 1. 先证明目录模式的契约，再接脚本

CLI 侧的事实（读码 + 实测，均为基线 `ed76cbb`）：

| 项 | 事实 | 出处 |
|---|---|---|
| 发现 | 输入是目录时**递归**找 `.gxml`，按相对路径排序，`SourceDir` = 文件实际所在目录 | `internal/cli/discover.go:37-98` |
| 输出映射 | `--out-dir X` 时目标目录 = `X + dirname(rel)`（`rel` 相对**发现根**） | `internal/cli/cache_wire.go:111-115` |
| 产物名 | `<源文件名去 .gxml>_gen.go` | 同上 `:144-146` |
| Source 头 | `// Source: edit.gxml`（**basename**，与模式无关） | 产物首两行，实测 |

实测发现清单（一键入口报告 `gxml` 字段，递归走完全工程后排序）：

```
['view/edit/edit.gxml', 'view/summary/summary.gxml']
```

除这两个模板源之外工程里没有第三个 `.gxml`（证据目录 `.verify-projects/` 下只有
CLI 二进制与各阶段日志），所以批次根 `common_parent_dir(...)` 算出来是 `view`。

因此有两条等价的写法：

```
goxml generate view/edit/edit.gxml --out-dir view/edit      # R12 逐页形态
goxml generate view --out-dir view                          # R13 批次形态（发现根=输出根=view）
```

**错用工程根做输出根会静默写错位置**（实测，`r13c-probe-outdir-dot`）：

```
$ goxml generate view --out-dir .         # rc=0
缓存未命中 edit/edit_gen.go：cache-missing
已生成 edit/edit_gen.go
已生成 summary/summary_gen.go             # 落在工程根的 edit/、summary/，不是模板的 view/edit、view/summary
```

rc=0、日志说「已生成」，产物却在新包里——所以脚本里的 `--out-dir` 必须与发现根一致。
脚本用 `common_parent_dir()` 算这个目录：本批 `.gxml` 的共同父目录（本项目为 `view`）。
共同父目录落在工程根时（文件就在根上）返回 `.`，映射同样正确（`rel` 会带上子目录）。

## 2. 逐 View 与批次的等价性（先证不改产物）

两份模板副本，一份用 R12 的两条逐页命令，一份用一条批次命令，其它条件相同：

| 文件 | 逐 View | 批次 | |
|---|---|---|---|
| `view/edit/edit_gen.go` | `8b801812a6262bac…` | `8b801812a6262bac…` | 逐字相同 |
| `view/summary/summary_gen.go` | `57b6829e16e5fbda…` | `57b6829e16e5fbda…` | 逐字相同 |
| `.goxml/generate-manifest.json` | `35ec3614296f7c33…` | `35ec3614296f7c33…` | 逐字相同 |
| 成功缓存键集合 | `{2975d474…, b9e7ea22…}` | 同左 | 同键 |
| 缓存记录内容 | — | — | 除 `createdAt` 外逐字段相同（keyHash/source/output/sourceHash/outputHash/compilerVersion） |
| 离屏测试 | 11 PASS / 0 SKIP | 11 PASS / 0 SKIP | 同一批 `TestCross*` |

产物里两页都是 `vm *model.Preferences`、`import model "example.com/crosspackage-preferences/model"`
——共享同一个 ViewModel 的类型身份没有因为批次而变；`TestCrossSummaryFollowsSharedModel`
与两个 `InstancesIsolated` 在批次产物上同样通过。

耗时（同一台机、同一份材料，只作量级参考）：逐页 2.687 s + 2.516 s = 5.203 s，
批次 4.630 s——两次进程、两次构建上下文测量并成一次。

## 3. 一次 generate 的证据链

一键入口（独占新目录，`--check`，真实 argv）：

```
stage copy           rc=0    复制 11 个文件，替换 4 个
stage build-cli      rc=0
stage generate:view  rc=0
stage build-entry    rc=0
stage test           rc=0
lock      : sha256:0e9e7b359d5b9763… released=True
```

报告的 `generate:view` 阶段：

- `argv`: `<out>/.verify-projects/goxml.exe generate view --out-dir view --check`
- `checkReported=true`、`checkTargets=2`（`提交前检查通过：2 个目标包` 里的 2）
- 原始 stdout（证据目录里的 `stdout.raw`）逐行：
  `缓存未命中 view/edit/edit_gen.go：cache-missing` /
  `缓存未命中 view/summary/summary_gen.go：cache-missing` /
  `提交前检查通过：2 个目标包` / `已生成 view/edit/edit_gen.go` /
  `已生成 view/summary/summary_gen.go` / `共 2 个文件生成成功（按路径排序处理）`

`checkTargets` 是本轮新加的观察字段：检查覆盖面从「每页各报 1」变成「一批报 2」，
它与「阶段只有一个」一起才能说明批次是真的（只看 argv 还能被逐页循环伪造）。

## 4. 自有实验工程的完整序列

B1 → B4 全部在 `verify-projects.py --copy-only` 造出的工程里跑（允许反复写同一目录）：

| 步 | 动作 | rc | 观察 |
|---|---|---:|---|
| B1 | `generate view --out-dir view --check` | 0 | 两页 miss、`checkTargets=2`、2 个产物 + Manifest + 2 条缓存 |
| B2 | 再跑同一条命令 | 0 | `跳过 …（缓存命中）`×2、`提交前检查通过：2 个目标包`、`命中 2，生成 0`；**全 16 个文件字节与 mtime 都没变** |
| B3 | `view/edit/doc.go` 追加 `var _ = noSuchSymbolR13C` | 2 | edit `package-sources-changed`（miss）、summary 命中；stderr `goxml generate: 提交前检查失败…` + `view\edit\doc.go:14:9: undefined: noSuchSymbolR13C`；两页产物、Manifest、两条缓存**逐字保留（含 mtime）** |
| B4 | 还原 doc.go 再跑 | 0 | 回到原键 → 两页命中 + `提交前检查通过：2 个目标包`；入口 `go build ./cmd/preferences` rc=0 |

B2 是「全命中仍执行检查」在**多目标包**上的证据（R12-C 只在单页上验过）：
命中的两个候选都进 overlay，检查按目录各编译一次。

## 5. 为什么必须是批次：拆回逐页会留下混合状态

同一份材料、同一组输入（`edit.gxml` 改标题 → edit 必 miss；`summary/doc.go` 注入类型错误）：

| 形态 | 结果 |
|---|---|
| 批次 `generate view --out-dir view --check` | rc=2；edit miss、summary miss；检查在 summary 包失败 → **两页都不写**，Manifest/缓存不动（实测 `r13c-probe-half-batch` 5.084 s） |
| 逐页两条命令 | 第一条 rc=0 并**写入**新的 `edit_gen.go`（sha 由 `8b801812…` 变 `ec084692…`），第二条 rc=2 → `summary_gen.go` 仍是旧字节、Manifest 里第一页是新 `inputHash`、第二页是旧登记、缓存 2 条变 3 条 |

也就是说：逐页调用把一个「要么都新、要么都旧」的批次变成了**跨页不一致**的工程状态。
这正是本轮把一键入口改成一次目录 generate 的实质理由，不只是少起一个进程。

## 6. 自动验收（`scripts/test-crosspackage-batch-e2e.py`）

5 个用例，87.619 s，全 PASS：

| 用例 | 断言要点 |
|---|---|
| `entry-batch-success` | 阶段里 `generate` 只有一个且 argv 恰为 `generate view --out-dir view --check`；`checkTargets=2`；原始 stdout 两页已生成；Manifest 两条；`check={forwarded:true,passed:true}`；入口构建 + 离屏 11 例；generate 证据目录唯一 |
| `own-project-cycle` | B1–B4（上表），含全命中零改动与失败保旧的字节 + mtime 对照 |
| `per-view-half-batch` | 复现 §5 的混合状态（第一页落盘、第二页失败），作为「必须批次」的反证 |
| `equivalence-per-view` | §2 的三份产物逐字比对 + 缓存键集合 + 两边各 11 个 CROSS-DONE |
| `mutation-per-view-loop` | 见 §7 |

## 7. 变异：拆回逐页必须翻红

变异体把脚本里的一处调用换回 R12 的逐页循环（`--make-mutated-repo --mutate per-view-loop`，
锚点是 `generate_batch(...)` 那一行）。变异后流程本身仍 rc=0，但批次判据 **4 条同时红**：

```
只启动一次 generate：阶段数应为 1，实得 2 ['generate:view/edit/edit.gxml', 'generate:view/summary/summary.gxml']
generate argv 应恰为 generate view --out-dir view --check，实得 [[…'view/edit/edit.gxml'…], […'view/summary/summary.gxml'…]]
本次检查应覆盖 2 个目标包，实得每阶段 checkTargets=[1, 1]
generate 原始 stdout 应有「提交前检查通过：2 个目标包」（一次检查覆盖两页）
```

## 8. 随改动更新的既有断言（语义不变，只是阶段名/条数）

一键入口的阶段从「两条 `generate:<file>`」变成「一条 `generate:<dir>`」，两处既有测试
按新形态更新，判据含义不动：

- `scripts/test-verify-projects.py`（协调者维护的 25 例）`full-pipeline`：
  期望阶段序列 → `['copy','build-cli','generate:view','build-entry','test']`。
- `scripts/test-precommit-fail-e2e.py`（R12-C）E1：失败阶段 → `generate:view`，
  并新增「检查失败时 `checkTargets` 必须为 None（计数只来自通过标记）」；
  E3：期望阶段序列同步。
  更新后重跑：4/4 PASS（66.284 s）。

回归：基线 25 例 **25 passed / 0 skipped**（49.518 s）；R12-C 4 例全绿。

## 9. B 线（ZCode）

- **R13-B：PENDING**（未冻结）。`product-r13-zcode/Goxml` HEAD 仍等于基线 `ed76cbb`，
  仅 1 个未提交改动，分支上无提交，`product-r13/zcode/` 交付目录不存在 → 没有冻结 SHA，
  「重排后行事件」与「双实例隔离」的复放无法对着一个固定版本做。
- **R12-B 冻结版 `5ecd4f6`（已进基线）**：本轮从产物侧顺带核对——两页产物都是
  `import model "…/model"` + `*model.Preferences`，没有出现 R10 的
  `import edit "…/model"` 形态；离屏 11 例（含 `TestCrossSummaryFollowsSharedModel`）
  在批次产物上通过。这与该线「别名取 ViewModel 实际声明包名」的结论一致。
  该线自己的判别测试未在本轮复跑（本线只做产物侧核对，不代替独立评审）。

## 10. 边界与未验证

- 零原生窗口：入口只构建、不运行；原生窗口与 IME 仍未自动验证。
- 离屏测试覆盖 View 语义与实例隔离，不覆盖真实渲染帧。
- 目录模式的**多 module** 批次、符号链接目录（`discoverInputs` 明确不跟随）未在本轮实践。
- `common_parent_dir` 只按「共同父目录」取批次根：若将来工程里出现两个毫不相干的
  `.gxml` 根（如 `docs/` 下的示例），它们也会被并进同一批——本项目模板没有这种布局，
  未做扩展。
- 本轮未改 CLI/编译器一行代码；脚本外的行为差异（如批次诊断顺序）不属本线结论。

## 11. 复现

```bash
R=D:/Codes/Goxml-agent-rounds/2026-11-04/product-r13/workbuddy
python scripts/test-crosspackage-batch-e2e.py --work-root "$R/work/e2e2" --report "$R/reports/e2e2.json"
python scripts/test-crosspackage-batch-e2e.py --make-mutated-repo "$R/work/mut" --mutate per-view-loop
python scripts/test-verify-projects.py --work-root "$R/work/base4" --report "$R/reports/base4.json"
python scripts/test-precommit-fail-e2e.py --work-root "$R/work/r12c2" --report "$R/reports/r12c2.json"
```
