# R14-C 跨包工程维护生命周期（workbuddy 线）

- 基线：`03c1f8aa1986f59c1183c41c710c99920253d680`
- 工作树：`C:/Users/admin/.codex/worktrees/product-r14-workbuddy/Goxml`，分支
  `codex/product-r14-workbuddy-20261104`
- 材料：`examples/gxml/testprojects/crosspackage-preferences` 原样复制（`--copy-only`），
  一页未改；影子工程在交付目录 `work/` 下，不在仓库里
- 判据来源：真实 CLI（`go build ./cmd/goxml` 产物）、`record-command.py` 原始日志、
  逐文件字节 SHA-256 快照

## 0. 一句话结论

删掉一个页面源之后，**产物、Manifest 登记、成功缓存三者并不同步**：孤儿产物要等一次
成功的 `--clean` 才走，Manifest 只跟着成功删除走，缓存那条旧记录**允许一直留着**。
所以「清理成功」只能用「文件不在 ∧ 登记少了那条」证明，**不能**用缓存条数变化证明；
反过来，旧记录留着也不会造成假命中——恢复源后 CLI 报的是 `cache-missing`，不是命中。

## 1. 生命周期实测（L1–L6，全部真实 CLI）

| 步 | 命令 | rc | 关键输出 |
|---|---|---:|---|
| L1 | `goxml generate view --out-dir view --check` | 0 | 两页 `cache-missing` → `提交前检查通过：2 个目标包` → `已生成`×2 |
| L2 | 同一条命令再跑 | 0 | `跳过 …（缓存命中）`×2、`命中 2，生成 0`、检查仍覆盖 2 包 |
| L3 | 删 `view/summary/{summary.gxml,doc.go,sem_test.go}`；入口去接线 | 0 | 孤儿 `summary_gen.go` 留盘；Manifest 仍 2 条；去接线后 `go build ./cmd/preferences` 通过 |
| L4 | 让 `view/edit/doc.go` 编译失败，`goxml generate view/edit/edit.gxml --out-dir view/edit --check --clean` | **2** | `提交前检查失败：目标包未能通过编译（本批未写入任何产物/Manifest/缓存）` |
| L5 | 修好同一个包，跑同一条命令 | 0 | `已清理 view/summary/summary_gen.go`、`清理完成：删除 1，跳过 0` |
| L6 | 恢复页面源与入口，`goxml generate view --out-dir view --check` | 0 | `缓存未命中 view/summary/summary_gen.go：cache-missing` → `已生成 …`、`命中 1，生成 1` |
| L6' | 再跑一次 | 0 | `跳过 …（缓存命中）`×2、`命中 2，生成 0` |

原始 stdout（逐字，`raw/r14c-cycle-*/` 与 E2E 的 `logs/l*/stdout.raw` 里都有）：

```
L1  缓存未命中 view/edit/edit_gen.go：cache-missing
    缓存未命中 view/summary/summary_gen.go：cache-missing
    提交前检查通过：2 个目标包
    已生成 view/edit/edit_gen.go
    已生成 view/summary/summary_gen.go
    共 2 个文件生成成功（按路径排序处理）

L5  跳过 view/edit/edit_gen.go（缓存命中）
    提交前检查通过：1 个目标包
    命中 1，生成 0
    已清理 view/summary/summary_gen.go
    清理完成：删除 1，跳过 0

L6  跳过 view/edit/edit_gen.go（缓存命中）
    缓存未命中 view/summary/summary_gen.go：cache-missing
    提交前检查通过：2 个目标包
    已生成 view/summary/summary_gen.go
    命中 1，生成 1
```

L4 的 stderr 是判定「检查门在清理之前」的关键证据：

```
goxml generate: 提交前检查失败：目标包未能通过编译（本批未写入任何产物/Manifest/缓存）
# example.com/crosspackage-preferences/view/edit
view\edit\doc.go:14:9: undefined: noSuchSymbolR14C: exit status 1
```

> L4 里存活页的 miss 原因是 `package-sources-changed`，那是**我们自己**往 `view/edit/doc.go`
> 注入一行造成的（doc.go 属于 view/edit 包）。它和「删了 summary 页面」无关，不要混读。

## 2. 产物 / Manifest / 缓存各自的节奏

`sha256` 取前 12 位；「缓存」是 `.goxml/cache/*.json` 的条数。

| 阶段 | `view/edit/edit_gen.go` | `view/summary/summary_gen.go` | `generate-manifest.json` | 缓存 |
|---|---|---|---:|---:|
| L1 | `8b801812a626` | `57b6829e16e5` | `35ec3614296f` | 2 |
| L2 | `8b801812a626` | `57b6829e16e5` | `35ec3614296f` | 2 |
| L3 | `8b801812a626` | `57b6829e16e5` | `35ec3614296f` | 2 |
| L4 | `8b801812a626` | `57b6829e16e5` | `35ec3614296f` | 2 |
| L5 | `8b801812a626` | **不存在** | `53484c917d8b`（1 条登记） | 2 |
| L6 | `8b801812a626` | `57b6829e16e5` | `35ec3614296f` | 2 |

两点值得记下来：

1. **L1 → L4 三条哈希一个都没动**。删源本身不动 Manifest，检查失败也不动任何东西；
   L3 那句「删源后登记仍是 2 条」是 `--clean` 判据的前提：登记是「这里曾经生成过」的记忆，
   不是「源还在」的声明。
2. **L6 的字节回到 L1**：`edit_gen.go` 与 `summary_gen.go` 都和第一次生成逐字相同，
   Manifest 也回到 `35ec3614296f`。恢复页面后不是「生成了一份差不多的东西」，是同一条输入
   走同一套模板的重放。

## 3. 检查门在清理之前

`internal/cli/pipeline.go` 里 `--check` 属于第一段末尾、写入之前；`--clean` 在登记之后。
所以 L4 那条「带 `--clean` 但检查失败」的命令里，清理阶段**根本没有执行**——不是
「执行了但没删」。证据有三层：

- stdout 只有一行 `缓存未命中 …`，没有 `清理完成：…` 这一行（对比 L5 有）；
- 孤儿 `summary_gen.go` 字节与 mtime 均未变（快照比对，非仅字节）；
- Manifest 与 2 条缓存记录逐字未变。

这条对脚本的写法有直接影响：断言不能写「失败时删除 0」，要写「失败时清理阶段未执行」，
否则将来把清理提前到检查之前也会绿。

## 4. 成功清理只删「已登记 ∧ 有归属证明」的孤儿

L5 那条命令的作用域只有一个文件（`view/edit/edit.gxml`），却删掉了另一页的产物。这不是越界，
而是 `--clean` 的定义：**本轮没参与生成、且源已消失、且保护头仍归本工具、且没有别的存活源
声称同一输出**的登记，才删。四条同时成立才动手，缺一条只打印跳过。

L5 之后 Manifest 从 2 条降到 1 条（`53484c917d8b`），缓存仍是 2 条——**两者刻意不同步**。

## 5. 三条安全锁 + 范围外存活页面（都是实测文案）

| 场景 | 造法 | stdout | 结果 |
|---|---|---|---|
| 用户接管产物 | 把产物首行换成 `// Copyright 2026 Example Corp. …`，再删该页源 | `跳过 view/summary/summary_gen.go (保护头不符，视为用户文件)`<br>`清理完成：删除 0，跳过 1` | 文件在、Manifest 仍 2 条 |
| 归属歧义 | 保护头在，但 `// Source:` 改成 `other_summary.gxml` | `跳过 view/summary/summary_gen.go (保护头 Source 与登记不符，归属歧义)`<br>`清理完成：删除 0，跳过 1` | 文件在、Manifest 仍 2 条 |
| 所有权争议 | 人工在 Manifest 里加一条「另一个仍存活的源也登记这个输出」 | `跳过 view/summary/summary_gen.go (输出仍被存活源 view/extra/summary.gxml 登记，所有权争议)`<br>`清理完成：删除 0，跳过 1` | 文件在、两条登记都在 |
| 范围外存活页面 | 两页源都在，只对 `view/edit` 跑局部 `--clean` | `清理完成：删除 0，跳过 0` | 两页产物字节 + mtime 都没动 |

前两条是公开 CLI 可达的真实路径；第三条需要两条登记指向同一个输出，**公开 CLI 走不到**
（同一份 `.gxml` 无论用文件模式还是目录模式登记，`source` 都是同一条项目相对路径，写不出
两条不同的登记），所以它是**人工构造 Manifest 夹具**，只用于验证守卫本身，不当作 CLI 行为。
凡在交付里引用这条，都按「人工构造」标注。

## 6. 缓存旧记录：允许保留，但不得造成假命中

L5 清理成功后 `.goxml/cache/` 仍是 2 条（`052d6e4304b8`、`32c4ee170122`，与 L1 同名同集合）。
那条属于已删页面的记录**没有被一起清掉**，这是允许的——但必须证明它不会骗人：

L6 恢复源后，CLI 报的是

```
缓存未命中 view/summary/summary_gen.go：cache-missing
```

而不是「命中」。原因是命中判定要先过 Manifest 登记：登记已被清理移除，于是同键的历史记录
只能解释成 `cache-missing`（`internal/cli/cache_wire.go` 的 `explainMiss`），随后真生成、
再登记、再写缓存。所以：

- **记录条数变化不是删除判据**（L5 前后都是 2 条）；
- **记录条数不变也不是假命中的证据**——要看不含歧义的 miss 原因文案 + `已生成` 行。

E2E 里这条是硬断言：`missReasons[view/summary/summary_gen.go] == 'cache-missing'`，并且
`generatedLines == ['已生成 view/summary/summary_gen.go']`。

## 7. 入口引用了被删视图：显式去接线

`cmd/preferences/main.go` 直接 import 两个生成包，删掉 summary 后不改入口，`go build` 必挂
（`undefined: summary`）。所以 L3 必须同时改入口，共 4 处（`shell` 字段、import、`layout.Flexed`
那一栏、`summary.NewGoxmlSummaryView` 调用），diff 38 行（`n=2` 上下文；交付里另有一份 46 行的
`n=3` 版本）：

- 去掉 `"…/view/summary"` import；
- `shell` 只留 `edit *edit.GoxmlEditView`；
- 删掉右栏 `layout.Flexed(1, …)`；
- `view := &shell{edit: ev, summary: sv}` → `view := &shell{edit: ev}`。

验证顺序固定为：`gofmt -l`（无输出）→ `go build ./cmd/preferences`（rc=0）。这样「构建失败」
只可能来自入口自身，不会和生成失败混在一起。恢复页面时把入口整份写回（保留原始字节），
L6 的二次构建 rc=0。

## 8. 工程 E2E 脚本与变异

脚本 `scripts/test-crosspackage-maintenance-e2e.py`，4 个用例，全部搬上面实测的生命周期：

| 用例 | 干什么 | 结果 |
|---|---|---|
| `lifecycle-maintenance` | L1–L6 全链 + 入口去接线 + 离屏 11 例 | PASS（`pass=11 skip=0 fail=0`，锁在 `finally` 释放） |
| `adoption-and-scope` | 用户接管 / 归属歧义 / 范围外存活页面 | PASS |
| `mutation-skip-clean` | 影子仓库里让 `--clean` 缺席，重跑生命周期 | 内层 FAIL，7 条判据翻红 |
| `mutation-clean-without-check` | 影子仓库里让清理不设检查门，重跑生命周期 | 内层 FAIL，14 条判据翻红 |

两条变异都只改 `CLEAN_ARGS`（`['--check','--clean']` → `['--check']` / `['--clean']`），
用一次本地替换实现，`--make-mutated-repo` 可复现。翻红项举例：

```
skip-clean：
  L5 只应清理那一页孤儿，实得 []
  L5 孤儿产物应已删除
  L5 Manifest 应只剩存活页登记，实得 ['view/edit/edit_gen.go', 'view/summary/summary_gen.go']
  L6 恢复页面必须报 cache-missing（旧记录不得假命中），实得 {}

clean-without-check：
  L4 检查失败应以 2 退出（实得 0）
  L4 检查失败时孤儿产物必须保留
  L4 除注入的 doc.go 外工程不得有任何改动：['内容变了 .goxml/generate-manifest.json',
     '被重写但字节相同 view/edit/edit_gen.go（mtime 变化）', '新增 .goxml/cache/4ecba57c…', '消失 view/summary/summary_gen.go']
  L5 缓存条数变化不得被当作删除成功（实得 2 → 3）
```

第二条变异顺带自证了一件事：把检查门去掉以后，**目标包坏着也照样把孤儿删了**，而且缓存
从 2 条涨到 3 条——这正是「只看条数会得出错误结论」的现场。

脚本自己踩过的坑（都已修，留档）：

1. 入口去接线的锚点一开始用 `%s` 拼模块路径，`old % mod` 在两处 `%s` 时报
   `not enough arguments for format string`；改成 `__MOD__` + `str.replace`。
2. 变异锚点最初写的是整行 `CLEAN_ARGS = [...]`，但 `MUTATIONS` 表里也含有这个字面量，
   `patch_file` 数到 3 次命中。改成「唯一标记行 + 在其后追加覆盖赋值」，并把标记用字符串
   拼接构造，保证它在文件里只出现一次。
3. `DEWIRE_EDITS` 的第三条（整块删除 `layout.Flexed`）漏了第二个元素，解包时
   `too many values to unpack`；补空串。

三次运行的账都在 `reports/e2e-run{1,2,3}*.json` 与 `raw/r14c-e2e-run*/`：run1 是三条脚本
缺陷（见上），run2 是解包缺陷，run3 PASS。前两次照样留在交付里，不做「只交绿的那次」。

## 9. 边界与未做

- 只动脚本、教程与本 dev-log；`internal/cli`、`internal/compiler`、锁与 Job 助手一行未改。
- 本线没有发现生产缺陷：四条安全锁、`cache-missing` 的判定顺序、检查门与清理的先后都符合
  文档描述，因此没有提交反例给 CLI/Codegen 线。
- 零原生窗口（入口只 `go build`，从不执行）、零采样、零安装、零联网；E2E 每个用例自持
  `heavy.lock` 并在 `finally` 按自有 token 释放，变异用例本身不持锁（内层运行自己取）。
- `docs/00-文档索引.md` 的 R14 索引行属协调者，本线不加。
- 未测：Windows 以外的平台；用户手工删掉 Manifest 后重跑；并发两个 CLI 同时 `--clean`。

## 10. 复现命令

```bash
# CLI
go build -o /tmp/goxml ./cmd/goxml

# 造影子工程（不联网、不 go mod tidy）
python scripts/verify-projects.py crosspackage-preferences --out-dir /tmp/shadow \
  --repo <repo> --copy-only

# L1 / L2
/tmp/goxml generate view --out-dir view --check      # 在 /tmp/shadow 里
/tmp/goxml generate view --out-dir view --check

# L3 删源（并去接线 cmd/preferences/main.go）
rm view/summary/summary.gxml view/summary/doc.go view/summary/sem_test.go

# L4 注入类型错误后局部清理
printf 'var _ = noSuchSymbolR14C\n' >> view/edit/doc.go
/tmp/goxml generate view/edit/edit.gxml --out-dir view/edit --check --clean   # rc=2

# L5 修好后同一条命令
$EDIT view/edit/doc.go                                                       # 去掉注入行
/tmp/goxml generate view/edit/edit.gxml --out-dir view/edit --check --clean   # 删除 1，跳过 0

# L6 恢复页面与入口后
/tmp/goxml generate view --out-dir view --check       # cache-missing → 已生成
/tmp/goxml generate view --out-dir view --check        # 命中 2，生成 0
go build ./cmd/preferences
go test -count=1 -v -timeout 20m ./view/... -run ^TestCross
```

一键跑全部用例（自带锁、逐条留原始日志）：

```bash
python scripts/test-crosspackage-maintenance-e2e.py --work-root /tmp/r14c --report /tmp/r14c.json
```

## 11. 外审：R13-B（ZCode 线，冻结于 `0eaa567`）

B 线在本轮期间冻结，顺手把 R13-C 遗留的 PENDING 兑现（报告在交付目录
`review-r13b-mixed-page.md`，不进本仓库）：

- 独立复跑 `go test ./internal/compiler/codegen/ -run TestMixedPage -count=1 -v`：**6 PASS /
  0 FAIL / 0 SKIP**（20.9s），实测 `t.Skip` 出现 0 次，断言都是具体值（`recs0/1/2/3` 计数、
  `Dimensions` 对同帧参考 View、`LastTapped` 期望行 ID）。
- 在**自有副本**（`work/review-b-mut`，被审树零改动）里复现它的 MUT-A：生成键字面量去掉
  `constraints:` → `mix-sibling-width` 红（`recs0=1, want 2（M1 违反）`），
  `mix-fixed-text` / `mix-context-items` 仍绿——特异性与 dev-log 记载逐字一致。
- **未复核**：MUT-B（事件归属）没独立复现、`-cover 88.7%` 门禁没重跑、R10/For 契约没逐条对照。
  这三条写在报告里，不当作「已证明无缺陷」。

R14-B 仍未冻结（HEAD == base `03c1f8a`，1 个脏文件，交付目录无 handoff），登记 PENDING。
