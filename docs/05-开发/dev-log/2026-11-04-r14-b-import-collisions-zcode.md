# 2026-11-04 R14-B GENERATED-IMPORT-COLLISIONS（ZCode）dev-log

基线 `03c1f8aa1986f59c1183c41c710c99920253d680`；树 `C:/Users/admin/.codex/worktrees/product-r14-zcode/Goxml`，
分支 `codex/product-r14-zcode-20261104`。
改动范围：internal/compiler/codegen 及直接回归 + 本线 dev-log。**无 CLI/Pipeline 新接线**（见下）。

## 反例（基线真实 CLI 复现，先立红）

R12 修正了跨包 ViewModel 别名；本轮验证固定导入限定符（Gio/stdlib）的另一类风险：

- 目标包 `ui` 用户文件合法声明 `type layout struct{}` / `type material struct{}` / `type op struct{}`；
- 生成文件 `import layout "gioui.org/layout"` → **go build 直接失败**（raw/r14b-ud-gen）：

  ```
  ui\userdecls.go:4:6: layout already declared through import of package layout ("gioui.org/layout")
      cand-000-000.go:10:2: other declaration of layout
  ```

- 机理：Go 规范禁止同一标识符同时出现在**文件块**（import 名）与**包块**
  （任何文件的包级声明）——不是作用域遮蔽问题，是硬规则。--check 的候选编译
  同样失败（`提交前检查失败：目标包未能通过编译`，磁盘零写入）。

## 修复（a3120fb，仅 codegen）

- `importSet` 登记时按 **taken**（= `Options.PackageNames` 的键，命名预检事实）
  与已登记名取第一个空闲名 `pkg_2`、`pkg_3`……（升序、确定性），重命名即强制
  写出别名；taken 为空时行为与基线**逐字节一致**（裸 import、默认限定符）。
- 新增 `g.q(path, default)`：全部限定符发射点统一经它取有效名并拼接引用，
  import 块与实际引用同步——**逐点改发射点，不做文本替换**；用户字符串与
  事件实参不经此路径，天然保真。
- 覆盖发射点：assemble 签名/构造函数/结构体字段、容器与 Flex 片段、
  sizeWrap/gioAlignment/gioSpacing/flexFieldsExpr/writeExactConstraint
  （改收 `g`）、padding/leaf 装饰、Grid 行/格/补位、Input 事件与 Editor、
  renderKeyDecl/bindCacheDecl/cacheFieldLines（改 generator 方法）、
  行事件轮询 `op.InvalidateCmd`。
- VM 别名路径不变：既有 vmTypeExpr 环已避开 PackageNames/Universe/已登记名；
  taken 另含本文件将声明的 view/keyType/cacheType（防御性）。

## 接线（无需新提交，如实报告）

基线已具备完整事实通道：`codegen.InspectPackageNames(targets)` →
`pipeline.Prepared.SetPackageNames`（cli/pipeline.go:456-462，预检后接线）→
`opts.PackageNames` → vmTypeExpr 既有环。B1 只让 Gio/stdlib import 的登记
消费同一事实。无新增 CLI/Pipeline 代码，**无单列接线提交**。

## 缓存与修订标识

- 缓存键不动：用户声明属目标包 Go 源 → K3 指纹覆盖，增删声明必安全 miss。
- **生成字节规则变化**：仅冲突包的字节改变（无冲突包与基线 CLI 逐字节相同，
  实测 sha256 一致 7788f171…）；登记需协调者统一提升 generatorOutputRevision，
  本线不撞 CLI 文件。

## 验收矩阵（真实 CLI + 单元；raw/repro 在交付目录）

| 项 | 结果 | raw/证据 |
|---|---|---|
| 多名同时冲突（layout/material/op） | `layout_2`/`material_2`/`op_2`，`--check` rc0、`go build ./...` rc0、引用同步 | r14b-ud-gen-2 / r14b-ud-build |
| 消歧候选名也被占用（layout+layout_2、material+material_2） | `layout_3`/`material_3` | repro/candidatetaken |
| VM 别名与 Gio 导入竞争（VM 包真实名 layout）+ taken | `layout_vm2 "…/vmdir"`（既有环） | repro/candidatetaken other_gen.go |
| 同包多页 | home_gen.go/other_gen.go 同包共存，各文件独立 import 块，build rc0 | r14b-cand-build |
| 同输入生成字节确定 | 同 doc 两次 Generate 逐字节一致（单元 TestImportDeterminismWithTaken）+ CLI 两次 --no-cache 一致 | r14b（noconflict） |
| 无冲突产物基线逐字对照 | 基线 CLI（03c1f8a detached 构建）vs 修复 CLI：sha256 相同 7788f171… | repro/noconflict |
| 单元回归 | TestImportTakenNamesForceAliases / CandidateEscalation / NoTakenBytesUnchanged / DeterminismWithTaken / VMAliasAvoidsTakenAndGio | import_collision_test.go |
| **变异红** | freeName 忽略 taken（可编译）→ TestImportTakenNamesForceAliases 红（import 块缺 layout_2）；复现后还原 | dev-log 记录 |

## 边界与未测

- 未改命名预检的 CODEGEN-NAME-001 语义（生成符号撞名仍拒绝）——本线只管
  import 限定符；两机制互补不重叠。
- `--out-dir` 场景的 taken 取自目标目录事实（同一 InspectPackageNames 口径），
  与同包路径一致，未单列用例（同包多页已覆盖 per-dir 映射）。
- Windows race / 原生 IME / 物理焦点：延续未测。

## 门禁

codegen 全包 `-skip '^TestButtonInRealWindowRuns$' -cover`：见 handoff（锁内、台账）。
gofmt 空、vet 干净、`go build ./...` ok。
