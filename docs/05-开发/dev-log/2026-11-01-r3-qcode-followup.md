# 2026-11-01 开发日志 · R3-FOLLOWUP · qcode（N1/N2 成因保留 + N4 阶段证据 + GOFLAGS 包名一致性）

任务书：
- `D:/Codes/Goxml-agent-rounds/2026-10-31/product-batch-r3/common-task.md`
- `D:/Codes/Goxml-agent-rounds/2026-10-31/product-batch-r3/review-wave-r2/qcode-task.md`

工作树：`C:/Users/admin/.codex/worktrees/product-r3-qcode-followup/Goxml`
分支：`codex/product-r3-qcode-followup-20261031`
基线（冻结，不再改动）：`bcb18405d789e751228e33071546af8b36d922e9`（R3 切片 1-3 + 交叉审核）

> 本线只改 `internal/cli/**`、`cmd/goxml/**` 与本日志。**不重做原回滚三片、不改原冻结交付、不 push。**
> 本日志记录的是**新增**的两个切片，新提交 `0f5af3c` / `e8b4018` 不冒用旧 SHA `bcb1840` 的审核结论。
> 本轮发现沿用 `N1 / N2 / N4 / F2` 标号（承接 R3 评审），不占全局 BUG/踩坑编号。

## 0. 起点核对（实测）

| 项 | 实测值 |
|---|---|
| `git rev-parse HEAD`（起点） | `bcb18405d789e751228e33071546af8b36d922e9` |
| 分支 | `codex/product-r3-qcode-followup-20261031` |
| Go | `go1.26.1 windows/amd64`，`CGO_ENABLED=0`、`GOMAXPROCS=2`、`GOPROXY=off`、`GOWORK=off`、`-mod=readonly` |
| 重负载门禁 | `raw/followup-heavy.py` 取共享 `heavy.lock`，只持自己 token、`finally` 释放实测值；锁忙以 99 退出不抢不守 |

约束遵循：`GOXML_AUTO_PUSH=0`、单一空 hooks 路径、精确逐文件 `add`、提交前后看 `--cached` 名单；未触碰主仓暂存区与他人工作树/锁。

## 1. 切片 1 — N1/N2：保留 close/remove 原始成因；N4：补 createdDirs 实际阶段证据

提交：短 `0f5af3c`，全 `0f5af3c866cfe27be1fbd17722413116c5621e51`
（3 files changed, 381 insertions(+), 29 deletions(-)）

### 1.1 最小根因
`txnError` 此前只有单个 `cause`：
- N1：写入失败分支里 `_ = ops.close(tmp)` 直接丢弃 close 错误；close 也失败时调用方 `errors.Is/As` 追不到。
- N2：`cleanup` 只回传「删不掉的路径」，多个 `remove` 的原始错误被吞；主 cause 之外无任何次错误可追。

### 1.2 修复（私有错误结构内保留多 cause，不改公开面）
- 新增私有字段 `extraCauses []error`；`Unwrap` 由 `error` 改为 `[]error`，主 `cause` 恒在首位、**不被次错误顶替**；无次错误时链形态与单 cause 等价（全仓 `errors.As/Is` 消费方核对：cli 内无裸 `errors.Unwrap()` 直调，改返回 `[]error` 安全）。
- `cleanup` 一并回传每个 remove 的原始 error；7 个失败点（暂存目录/建临时/写/close/备份/stat/提交 rename）统一用 `residue, removeCauses := cleanup(...)` 填 `extraCauses`。
- `op/path/residue/restored/withdrawn/unrestored` 与主次措辞不变；成功路径与缓存路径**不新增**任何次错误。
- 退出码、公开 API、CLI 选项、契约不变；`rollbackCommit` 签名未动（不属 N1/N2 面）。

### 1.3 N4：真实暂存/提交阶段的受控反例
`internal/cli/pipeline_createddirs_test.go::TestGenerateCommitFailureAfterNewDirsNamesCreatedDirs`
用「输出目录 `gen` 与某产物目标 `gen/a_gen.go` 撞名（源子目录字面叫 `a_gen.go`）」在真实文件系统上触发**提交期** rename 失败（Windows 下 `rename file→已存在目录` = `Access is denied`），`MkdirAll` 已把两级目录建出 → `createdDirs` 确实到达提交阶段。实测措辞（`raw/slice1/n4-createddirs.log`，PASS 0.16s）：

```
提交失败（已回滚；新建输出目录 …\gen; …\gen\a_gen.go 保留）: 提交失败（…\gen\a_gen.go）:
rename …\gen\.goxml-<tmp> …\gen\a_gen.go: Access is denied.
```

该措辞点名了新建输出目录与残留目录本身，**不谎称**「读取产物」级早停、**不谎称**「磁盘无变更」；断言同时核对了目录仍为目录、`b_gen.go` 缺席、Manifest/缓存目录未落、无 `.tmp` 残留。

### 1.4 判别性证据
- 红（修复前）：`raw/slice1/red-n1n2.log` rc=1，2 个用例 `--- FAIL`（close/remove 成因不可达 + `Error()` 文本缺附带失败），op/path/residue 仍 PASS —— 是行为失败，非删符号编译红。
- 绿：`raw/slice1/green-n1n2-targeted.log` rc=0（三个 N1/N2 用例全过，含「清理成功时不加次错误」这条防过度修复）；`raw/slice2/full-cli-regression.log` 整包 `ok … 21.875s` rc=0。

## 2. 切片 2 — F2：生成包名消费 GOFLAGS `-tags`

提交：短 `e8b4018`，全 `e8b4018437a2b42ec9c12f12ce163471aa764f16`
（4 files changed, 669 insertions(+), 2 deletions(-)）

### 2.1 最小根因
`packageDirName` 用 `build.Default.MatchFile` 选文件，而 `build.Default` 的 `BuildTags` 不读 `GOFLAGS`（实测 `go/build` 默认上下文里 `BuildTags` 未由 GOFLAGS 填充）。当 `GOFLAGS` 带 `-tags` 时，`go build` / `go list` / Analyzer(go/packages) 把带该 tag 的源文件纳入本包，CLI 却按「无 tag」集合定名 → 产物 `package` 与真实包名分叉 → 提交后 `go build` 报 `found packages A 和 B`。

### 2.2 修复（权威 Go 规则兼容，不手写标签表达式）
新增 `internal/cli/buildcontext.go`：
- `goFlagsSplit` 逐行复刻 `cmd/internal/quoted.Split`（认 `''`/`""` 包裹**整个字段**、字段内不再转义、空格/制表/换行/回车分隔、未闭合引号报错）。不用 `strings.Fields`——它会把 `"-tags=a b"` 错切。
- `tagsFromGOFLAGS` 复刻 `base.InitGOFLAGS` 的 flag 形状校验（`!-`前缀、`-`、`--`、`---*`、`-=*`、`--=*` 一律拒绝）与 `work.tagsFlag.Set` 的值解析（值含空格或单引号→ `quoted.Split`；否则逗号切、忽略空串）；重复 `-tags` 以最后一次为准（每次重置切片，对齐 `*v=[]string{}`）。裸 `-tags` 无值→ fail-closed。
- `buildContextFromGOFLAGS` 以 `build.Default` 为基础（继承 GOOS/GOARCH/ToolTags/ReleaseTags），只合并 `-tags`；非法 GOFLAGS 返回 error，调用方 fail-closed，**绝不**降级到 `build.Default` 蒙混。`packageDirName` 改用 `ctxt.MatchFile`。

### 2.3 死锁排除：工具自有产物不参与定名
`packageDirName` 对读取到的源文件先 `codegen.HasProtectedHeader(data)` 判定，命中即跳过——它是**本工具自己**的产物，不是「真实生产包」。修掉「旧错误包名产物绊住 `go list`、再次生成前就因两 package 被拒而永远无法自愈」的死锁。仍**拒绝 ≥2 个人手写的真实生产包**（排除的是工具自有，不是包冲突本身）。

### 2.4 真实 Go 工具正负对照（`internal/cli/package_dir_goflags_test.go`，均用 `go list` 子进程，`GOFLAGS` 显式注入并剔除继承项）
- 正例 `TestPackageDirNameMatchesRealGoListUnderTag`：合法互斥标签 fixture（`!goxmlr3`→`pkgbase`、`goxmlr3`→`pkgtag`），无 tag 与带 tag 两种配置下 CLI 名**逐一等于** `go list` 名，且 `go list` `Incomplete=false`。
- 负例 `TestPackageDirNameLegacyDivergesFromRealGoList`：复刻修复前算法得 `pkgbase`，与 `go list` 的 `pkgtag` 分叉（断言二者必不相等）——把「旧行为就是错的」固化成常驻回归。
- 端到端 `TestGenerateTaggedPackageMatchesGoList`（0.26s）：真实 `Run(generate)` 在 `-tags` 切换下把同一页生成进随标签激活 `pkgbase`/`pkgtag` 的独立 module 目录，产物 `package` 等于该配置下 `go list` 权威名。
- fail-closed `TestPackageDirNameInvalidGOFLAGSFailsClosed`：`garbage`/`-`/`--`/`---x`/`-=v`/`--=v`/未闭合引号/裸 `-tags` 全部报错且错误指向 `$GOFLAGS`。
- 分界 `TestTwoRealPackagesRejectedAndGoListAgrees`：两个真实生产包目录 CLI 拒绝，`go list` 同样 `Incomplete=true` → 属「fixture 在 Go 里即非法」，不是 CLI 回归。
- 死锁排除 `TestStaleGeneratedArtifactDoesNotBlockRename`：带保护头的错误旧产物被跳过、按激活真实源定名 = `pkgtag`；去掉保护头（变成人手写的第二真实包）后仍被拒。

### 2.5 判别性证据
- 红：`raw/slice2/red-goflags.log` rc=1 —— 一次性探针以**修复前**算法断言「包名应等于 go list」，得 `pkgbase ≠ pkgtag`；探针同时用「修复前会写入的产物包名」落一枚 `home_gen.go`，同 `-tags` 下真实 `go build` 报 `found packages`（构建期真实失败面，非编译期假红）。
- 绿：`raw/slice2/goflags-green.log` rc=0（含全部对照用例），`raw/slice2/vet.log` rc=0，`raw/slice2/full-cli-regression.log` 整包 `ok … 21.875s` rc=0。

## 3. 边界与不可证（如实，不过度承诺）
- **未复制 go 的 flag 注册表校验**：`InitGOFLAGS` 还会用 `hasFlag(Go, name)` 拒「该命令未知的全局 flag」。CLI 只需知道哪些 build tag 参与文件选择，无法在不 `import` cmd 内部包的前提下复制那张注册表；且 `-mod`、`-v` 等与文件选择无关，忽略它们与 `SetFromGOFLAGS`「对本命令未知 flag 选择忽略」一致。故只拒**语法级**非法（非 flag 形态、未闭合引号、裸 `-tags`）。此为有意边界，非遗漏。
- **GOOS/GOARCH 交叉环境**未纳入本切片（属既有 `build.Default` 行为面，且 F2 命题只关于 `-tags`），未改动、不声称覆盖。
- **独立 module 里 `go build` 产物**不能离线解析 gioui（产物 `import gioui.org/...`，`GOPROXY=off` 下 scratch module 无该依赖）。故对照统一用 `go list`（它做 tag 感知的**文件/包名**选择，正是 F2 命题层），全量真实构建以整包 `go test`/`go vet` rc=0 佐证；「fixture 离线不能建」≠「CLI 错名」，已在 §2.4 分列。
- 门禁：`go test ./internal/docgate/` 由后续统一验证；两个代码提交的全 hash 已写入本日志，供 docgate 的 dev-log 覆盖检查命中。

## 4. 五态登记（本线）
| 态 | 状态 |
|---|---|
| 源码实现 | 已完成（`0f5af3c`、`e8b4018`，仅 `internal/cli/**` + 本日志） |
| 自测 | 已完成：`go vet ./internal/cli/` rc=0；整包 `go test ./internal/cli/` rc=0（21.875s）；切片级红绿各一（真实行为红） |
| 外部评审 | 待 DeepSeek/协调者；**本线新提交不复用 `bcb1840` 旧结论** |
| 主仓接受 | 未发生（主仓 `D:/Codes/Goxml` 只读；未 merge） |
| 远端发布 | 未发生（未 push） |

交付目录：`D:/Codes/Goxml-agent-rounds/2026-10-31/product-batch-r3/review-wave-r2/qcode/`（`handoff.md`、`slices.json`、`commands.jsonl`、`raw/**`）。
