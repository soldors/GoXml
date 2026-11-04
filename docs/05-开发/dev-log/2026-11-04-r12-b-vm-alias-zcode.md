# 2026-11-04 R12-B CROSS-PACKAGE-VM-ALIAS（ZCode）dev-log

基线 `b12980c1c939892cb90221a620121e9070f55ef3`；树 `C:/Users/admin/.codex/worktrees/product-r12-zcode/Goxml`，
分支 `codex/product-r12-zcode-20261104`。
改动范围：internal/compiler/codegen（别名逻辑 + 回归）、internal/compiler/pipeline 与 internal/cli（VM 包名事实接线，单列提交）、本线 dev-log。
未改 Parser/Expander/Analyzer、commit.go 事务算法、缓存键格式（cacheFormatVersion 仍为 7）、contracts、scripts、examples；未新增公开 CLI 选项/诊断码/GXML 语法。

## 反例（真实 CLI 复现，先立红）

R10 反例在基线 b12980c 重放成功（raw：`r12b-ce-gen-1`、`r12b-cea-gen-1`，工程材料 repro/counterexample*）：

- 页面 `view/edit/edit.gxml`（package edit）、ViewModel 在 `model/`（package model）：

  ```go
  import (
      edit "example.com/prefs/model"   // 限定符是【页面包名】，不是 model
  ```

- `view/summary` 同样产出 `import summary "example.com/prefs/model"`。
- 更差变体：`model/` 声明 `package vmodel`（目录≠包名）时，生成代码仍是
  `import edit ".../model"`——限定符与包真实名、目录名、用户声明三者全不一致。

两种变体 `go build ./...` 均 rc=0（`r12b-ce-build-2`、`r12b-cea-build-2`）：
**可编译但误导阅读**，与任务书判断一致。

## 根因

`codegen.vmTypeExpr` 无条件优先 `Options.ViewModelPackageName` 作 VM 限定符，
而 CLI（cache_wire.go resolveInput）传入的该值是**源目录**（页面）真实包名
`srcPkgName`。VM 与源同包时它恰好也是 VM 包名（正确）；VM 声明在其他目录时
它与 VM 包名无关（错误）——这就是反例机制。CLI 管线自身没有 VM 目录解析
接线：VM 目录只有前端解析出 ViewModel 声明后才可知，之前无从填对。

## 契约核对（先核输出契约，再动手）

- 现状（基线）：R4-A 片2 的「强制显式别名」机制只保证**编译期自洽**
  （限定符=别名绑定），不保证别名=真实包名；代码注释与
  `Options.ViewModelPackageName` 文档写的是「ViewModel 所在包的真实 Go 包名」，
  接线传的却是页面包名——文档语义与接线事实不一致。
- 承诺（`代码生成输出契约.md` §7.2）：「读取 Go package 的**真实默认包名**」
  作默认名；§4.1 包名必须解析得到、不得猜测。修正方向与契约一致，
  不需要新语义批准：默认别名取 VM 实际声明的 Go 包名。

## 修复 1：Codegen 别名优先级（3be0564）

- 新增 `Options.VMPackageName`（VM 目录解析出的真实包名，调用方填入）。
- `vmTypeExpr` 优先级改为按 ViewModel 实际所在包取事实：
  - 同包（`vmRef.PkgDir` 为空，或与 `ViewModelPackageDir` 同目录）→
    沿用 `ViewModelPackageName`（源包名，此时它就是 VM 包名；`--out-dir`
    迁移场景行为不变，既有 R4-A 片2 用例不变红）；
  - 跨目录 → 改用 `VMPackageName`；未接线时回退目录名
    （GXML 声明文法里用户写的限定符就是目录名，回退与用户声明一致）；
    **绝不**再拿源包名冒充 VM 包名。
- 冲突消歧完全复用既有规则：`vmTypeExpr` 的注册期碰撞环（`pkg_vm2`…，
  先于 import render 检查 byPath 全量名字）+ importSet §7 的路径字典序
  `pkg_2` 规则。核实过 `vmTypeExpr` 在 assemble 内、所有 Gio 常量名注册之后
  调用，且每文档至多一个 ViewModel——同文件内 VM×VM 同名不可达，
  VM×Gio 同名由注册期环消歧，无遗漏路径。
- Codegen 仍不读盘、不自行解析构建上下文：两个包名都是调用方输入事实。
- 回归（vm_crosspkg_test.go 新增 3 条）：跨目录优先 VMPackageName 且断言
  页面包名不泄漏；未接线回退目录名；同包沿用源包名。
  **变异红**：把优先级改回无条件源包名（`vmInSourcePkg := true`），
  `TestVMExprCrossDirUsesVMPackageNameNotSourceName` 即红（`edit.VM`），
  已复现并还原。

## 修复 2：CLI VM 包名事实接线（991d44e，单列接线提交）

- `pipeline.Config` 增 `VMPackageName`；`Prepared.SetVMPackageName` 供编排层
  在 Prepare 之后、RunPrepared 之前填入（只补调用方输入事实，
  不触碰 VerifiedDoc 的 Facts freeze 语义）。
- `runPipeline` 在阶段零 C（预检，preparedFE 就绪）之后统一
  `wireVMPackageNames`：对跨目录 VM 用与目标包定名**同源**的
  `packageDirName`/go list 权威口径解析 VM 目录真实包名；批内 memo
  （edit/summary 共享 model/ 只查一次）；解析失败 fail-closed（页面能通过
  Analyzer 意味着 VM 包已被类型环境加载，此处失败属工程/上下文不一致）。
  同包 VM 不解析。阶段一 `preparedFE` 兜底分支同接线。
- 缓存键不动：K8 依赖闭包指纹含 VM 包源码（`package vmodel` 行在内），
  包名改名必然改 K8 → 安全 miss；别名修复不引入新键分量。
- 回归（vm_alias_wire_test.go，真实 `Run()` 四条）：目录≠包名（vmodel）、
  目录=包名（model，行为不变）、同包无自引用、VM 目录缺包 fail-closed。
  **变异红**：跳过 `SetVMPackageName` 接线，vmodel 用例即红（退回 model 别名），
  已复现并还原。

## 真实 CLI 验收（B3，全部锁内 go build；raw 与 repro 在交付目录）

| 验收项 | 工程 | 结果 | raw |
|---|---|---|---|
| 反例重放（目录=包名） | repro/counterexample | 基线 `edit "…/model"` rc0 → 修复后 `model "…/model"`，build rc0 | r12b-ce-gen-1 / r12b-ce-regen / r12b-ce-build-new |
| 目录≠包名 | repro/counterexample-alias | 基线 `edit "…/model"` → 修复后 `vmodel "…/model"`（edit+summary 两视图），build rc0 | r12b-cea-gen-1 / r12b-cea-regen(-2) / r12b-cea-build-new |
| 两个依赖包声明同名 | repro/depsame（alpha/model 与 beta/model 均 `package model`） | 同包两生成文件各自独立 import 各自路径（每文件独立 import 块），build rc0 | r12b-dep-gen / r12b-dep-build-2 |
| 同包无自导入 | 单元（真实 Run()） | 裸 VM 类型名、无本模块 import | TestWireVMPackageNameSamePkgNoImport |
| --out-dir 跨包 | counterexample-alias → gen/ | `package edit`（未初始化回退源包名，既有语义）+ `import vmodel "…/model"`，`go build ./gen` rc0 | r12b-outdir-gen / r12b-outdir-build |
| 相同输入确定性 | counterexample-alias | 两次 `--no-cache` 生成 sha256 相同（da78bc76…） | r12b-det-regen |
| 新缓存闭环 | counterexample-alias | 重生成后再跑 `generate view/edit` 命中（新记录正常读写） | r12b-cea-hit2 |

## 旧缓存对别名变化的影响（A 线修订标识的依据，B 线只记录）

实测（`r12b-ce-newcache-hit`、`r12b-cea-hit`）：基线二进制生成的缓存记录，
**新二进制仍命中**（K1 源哈希、K3/K5/K6/K8/K10 均未变，K4=codegen.GoxmlVersion
"1" 是语言版本、不含生成器修订）→ 产物保留旧别名 `import edit ".../model"`，
直到 `--no-cache` 或输入变化才重生成（`r12b-ce-regen` 证实恢复路径）。
即：**别名修复对热缓存工程不是自动生效**；是否引入生成器修订标识（K4 或新
分量）属 A 线职权，本线不撞线。新二进制自身的缓存读写闭环正常（r12b-cea-hit2）。

## 边界与未测

- Windows race、原生 IME/物理焦点：不在本轮（延续列未测）。
- 多 VM 目录的批量 go list 上限：复用 R9-A 有界批次/单查兜底，未另设压测。
- `packageDirName` 对 VM 目录的多包/坏文件语义：fail-closed 路径由
  TestWireVMPackageNameMissingVMDirFailsClosed 覆盖缺失形态；多包形态复用
  既有 packageDirName 测试，未重复造例。
- 契约 §4.4 Options 样例（未含 R4-A 片2/R12-B 新字段）已交 docs-proposal.md
  由协调者归并；本轮不改正式契约文件。
