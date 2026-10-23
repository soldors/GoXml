# BUG-007：`renderKeyDecl` 的 `cacheType` 参数从未被使用

## 元信息

- 状态：**Fixed**（2026-10-23，删除参数）
- 发现日期：2026-10-23
- 发现方式：按行阅读 `internal/compiler/codegen/codegen.go` 时核对参数使用情况
- 影响范围：`internal/compiler/codegen/codegen.go`
- 关联：`TASK-CODEGEN-NAMING-001` / C4

## 现象

```go
// internal/compiler/codegen/codegen.go:1004
func renderKeyDecl(view, keyType, cacheType string) string {
```

函数体全文是第 1004-1039 行（`return` 一个反引号模板字符串，第 1039 行闭合）。
其中只用到了 **`view`** 与 **`keyType`**：

- `view` → `func (v *<view>) currentKey(...)`；
- `keyType` → 类型名、`equal` 的接收者、`currentKey` 的返回值；
- `cacheType` → **零引用**。

而调用点照传不误：

```go
internal/compiler/codegen/codegen.go:949:  b.WriteString(renderKeyDecl(g.view, g.keyType, g.cacheType))
internal/compiler/codegen/codegen.go:973:  b.WriteString(renderKeyDecl(g.view, g.keyType, g.cacheType))
```

## 根因

推测（**未确认**）：函数最初可能打算把 `cacheType` 写进生成类型（作为字段或注释），
后来形态收敛成只依赖 `keyType`，参数却没跟着删。

之所以能长期存在：

- Go 编译器**不报未使用的函数参数**（只报未使用的局部变量与 import）；
- `go vet` 也不覆盖这一项；
- 现有测试只断言生成代码的包级命名（`naming_test.go` 第 139-140、331-332 行），
  不会因为参数多传或少传而变红。

## 影响

当前**不影响行为**：多传一个不用的字符串不会改变输出。

真正的问题是**误导**：读代码的人会以为 renderKey 声明与 textCache 类型存在耦合，
进而在改动命名规则时误判影响面（这正是 C1/C4 那类工作的高风险区）。

## 修复

采用方案 1（删除参数）：

```go
// renderKeyDecl 生成 renderKey 类型与其方法。
//
// 只依赖 view 与 keyType：cacheType 曾在这里出现但从未被使用（BUG-007），
// 保留它会让人误以为 renderKey 声明与 textCache 类型存在耦合，
// 从而在改命名规则时误判影响面。需要缓存类型的声明走 bindCacheDecl。
func renderKeyDecl(view, keyType string) string {
```

- 定义：`internal/compiler/codegen/codegen.go`（原签名 `renderKeyDecl(view, keyType, cacheType string)`）；
- 调用点：同文件两处 `renderKeyDecl(g.view, g.keyType)`；
- 测试调用点：`naming_test.go` 四处同步去掉第三个实参（`cacheType` 仍由
  `bindCacheDecl` 使用，未被牵连）。

**为什么选删除而不是"保留并注释"**：没有任何计划把 cache 字段加进 renderKey ——
renderKey 是帧上下文指纹（constraints / metric / theme / locale），
与显示缓存槽位无关；留一个只为"将来可能用到"的参数，等于长期支付误解成本。

## 回归测试

无论哪种修法都应满足：

```bash
grep -rn "renderKeyDecl(" --include=*.go .          # 调用点与定义签名一致
go build ./...                                       # rc=0
go test ./internal/compiler/codegen/ -run TestNaming # 命名相关的绝对数断言仍绿
```

## 关联

- `internal/compiler/codegen/slots.go` 定义了槽位规则，二者都在同一条命名链上，
  改动时应同时回归 `TestSlotCountEqualsEmittedSlots`（13 形态绝对数断言）。
