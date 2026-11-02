# 2026-11-02 R7-B BUILD-CONTEXT（ZCode）

任务书：`night-product-r7/zcode-task.md`。基线 4617f27，分支
`codex/night-r7-zcode-20261102`。

## 提交

| SHA | 内容 |
|---|---|
| `79ec275` | fix(cli)：B1 有效构建上下文统一 + B2 包目录错误 fail-closed（生产 4 文件） |
| `65c1595` | test(cli)：B1/B2/B3 对照、fail-closed 与缓存闭环回归 |
| `902b5cf` | test(cli)：GOENV 用例在宿主注入 GOFLAGS=-mod=readonly 下保持判别力 |

## B1 事实与修复（先复现后修）

真实复现（`raw/repro-b1/`，真实 CLI 二进制 + 真实 go env/go list 同环境对照）：

- GOENV 文件写 `GOFLAGS=-tags=goxmlr3`：`go env GOFLAGS` 回读该值、
  `go list` 选 **pkgtag**；基线 CLI 生成 **package pkgbase**（rc=0 静默分叉）。
- GOENV 文件写 `GOOS=linux`：`go list`=**pkglinux**；基线 CLI=**package pkgwin**。
- 对照组：同 fixture 进程 GOFLAGS=-tags=goxmlr3（GOENV=off）→ 基线 CLI
  得 pkgtag ✓——分叉特异于 GOENV 持久化路径。

根因：packageDirName 走 `buildContextFromGOFLAGS(os.Getenv("GOFLAGS"))`
（build.Default + 进程环境投影），而 go 命令/Analyzer（go/packages 子进程）/
缓存 K10（R5 起的 go env 实测）都消费 GOENV 持久化值。

修复：`measureContextValues`（dependency_context.go 重构，返回实测原值；
envOverrides 供测试注入自有 GOENV）→ `buildContextFromMeasured`
（buildcontext.go，GOOS/GOARCH/CGO 直采实测值，-tags 复用 tagsFromGOFLAGS
原语义——引号/重复/非法规则不变）→ runPipeline 每批测量一次、批内共享、
写盘前 fail-closed（保留 cause）；packageDirName/resolveInput 改收显式
上下文。不做跨调用缓存——两次 generate 之间 GOENV 变化必须可见。

**实测语义备注**：`go env` 对非法 GOFLAGS 只回显不校验（rc=0 返回
"garbage"），fail-closed 点在上下文构造的 tagsFromGOFLAGS；显式空值
`GOFLAGS=`（环境里存在）不遮蔽 GOENV 文件（两条均实测）。cmd/go 的
`GOFLAGS=-mod=readonly` 在进程层会遮蔽文件值——GOENV 专项用例以空值
覆盖继承值（902b5cf）。

## B2 事实与修复（先复现后修）

旧代码两处 continue（任务书点名的风险线索）：被构建选择命中的文件
ReadFile 失败 → continue；无合法 package 子句 → continue。目录唯一真实源
缺席后被误判「未初始化」，--out-dir 下回退源包名写出错误产物。

证明分层：解析失败/不回退/失败保旧 = 真实文件系统反例（坏 package 子句
与 go list Incomplete 同判；端到端 Run 失败且旧产物字节/mtime/Manifest
不变）；读取失败 = readPackageFile 注入（Windows 无法可移植构造
「ReadDir 列出但 ReadFile 拒绝」的真实文件——注入只证明接线，未标成
OS 故障）。修复后边界冻结：只有确实不存在/为空的目录走 errPackageUninitialized
回退；读取/解析/多包/上下文错误一律 fail-closed 且 errors.Is 链保留。

## B3 闭环（真实 Run()，进程内 + 真实 go env/go list/go build 子进程）

`cache_context_closure_test.go`：命中（字节/mtime/Manifest 全不变）→
只切 GOENV 上下文 → 正确 miss（有原因行）+ 重分析（包名随 go list 权威
切换）→ 还原恢复（字节恢复 pkgtag 版）→ 坏 .gxml 失败保旧。跨包输出
（vm 包 + 已初始化目标包 uiout）真实 `go build ./...` 通过（go.mod/go.sum
复制自本仓，依赖走本机模块缓存，GOPROXY=off 离线）。

**cacheFormatVersion 分析（不升版）**：修复不改变键组成——K10 本就含
有效 GOFLAGS，键内 K5 packageName 随上下文切换变化 → 旧记录键不同 =
安全 miss；不存在「旧键继续假命中」路径（旧 pkgbase 记录与新 pkgtag 键
天然不同）。`cacheKeyFor` 的 BuildFlags 字段仍是进程环境投影，但被 K10
支配（K10 含有效 GOFLAGS），不构成假命中面——按「不凭习惯升版」保留，
登记于此。

## 测试范围与限制

- internal/cli 全包：`go test -count=1 -cover -skip '^TestButtonInRealWindowRuns$'
  -timeout=15m ./internal/cli/` → rc=0，coverage **87.8%**（≥85%），
  heavy.lock 内（token 0235ca76…，已验证释放）。
- vet rc=0；gofmt 无差异。
- 未测：Windows race（需 CGO，与本轮 CGO=0 冲突，按公共任务列未测）；
  GOWORK 工作区（既有保守 miss 语义未动）。
- 环境纪律：全部环境变化仅限自有子进程（measureContextValues envOverrides /
  t.Setenv / goEnvFor），父环境与用户 GOENV 未动。

## 踩坑

- 公共任务的固定 GOFLAGS=-mod=readonly 会让 GOENV 专项用例被进程层
  遮蔽——首跑全包 4 例红，以空值覆盖 + 断言点上移修正（902b5cf）。
- 单文件输入的 rel 是 basename：测试里误传 `src/home.gxml` 使目标目录
  变成 out\src（不存在→合法回退），修正为 `home.gxml`。
