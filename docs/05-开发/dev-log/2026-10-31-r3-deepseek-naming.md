# 2026-10-31 R3 命名定位修复（DeepSeek）

任务：PRODUCT-R3-DEEPSEEK-REVIEW-NAMING-R1（B 项）。树 `product-r3-deepseek-naming`，分支 `codex/product-r3-deepseek-naming-20261031`，基底 `04ac791`。

## 修复（commit `aeee95f`）

- 问题（R3-E2E F1）：`internal/compiler/codegen/naming.go` 的 `packageLevelNames` 以硬编码文件名 `x.go` 调用 `parser.ParseFile`，而调用方 `existingPackageSymbols` 已持有真实 `full` 路径；真实 `bad.go` 的语法错误因此被标成 `x.go:3:14`（消息里同时出现 `bad.go` 与 `x.go`）。
- 修复：`packageLevelNames(filename string, src []byte)`，调用方传入真实文件名（`e.Name()`）；错误保留真实 `file:line:column` 与 cause。**保持原诊断阶段**（包级符号预检），不改成 ROOT-002、不伪造 GXML span、不新增诊断码。
- 回归：`internal/compiler/codegen/naming_position_test.go`（真实文件名 ASCII/空格/Unicode；`existingPackageSymbols` 集成路径含真实文件名与行号、无 `x.go`；正常声明正对照，不绕过解析）。

## 验证

- 窄测试通过；既有命名预检测试（排除 `TestButtonInRealWindowRuns`）通过；`go vet ./internal/compiler/codegen` rc=0；`gofmt -l` 空。
- 真实 fixture（路径含空格+Unicode）：旧冻结 CLI → 消息含 `bad.go` + **伪 `x.go:3:14`**；新树 CLI → `预检解析 <abs bad.go>: bad.go:3:14: ...`（无 `x.go`），与 `go build ./page` 的 `bad.go:3:14` **行列一致**。证据在交付 `implementation/`。

## 边界

- 仅改 `naming.go` 与该命名定位回归；未改 CLI/pipeline/analyze.go/作用域/全局诊断目录。
- `GXML CompilerError.File 为空`（CLI 呈现 `:行:列` 缺 GXML 文件名）**另记**交协调者，未在本任务改其它人文件。
- 本切片待非作者审核；A 对 qcode 的审核不构成本实现的外审。
