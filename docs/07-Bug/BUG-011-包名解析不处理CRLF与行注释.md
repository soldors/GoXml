# BUG-011：`parsePackageName` 不处理 CRLF、不切分第一个 token

## 元信息

- 状态：**Fixed**（2026-10-23）
- 发现日期：2026-10-23
- 发现方式：代码审查（读 `internal/cli/pipeline.go` 的轻量文本辅助段）
- 影响范围：`internal/cli/pipeline.go`（`packageDirName` → 生成产物的 `package` 子句）
- 关联：`PIT-013`（跨平台结论不外推）、`BUG-007`（同为"看着无害、实则污染下游"）

## 现象

```go
func trimSpace(s string) string {          // 只裁前导空格 / tab
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') { s = s[1:] }
	return s
}

func parsePackageName(src []byte) (string, bool) {
	for _, line := range errors_(src) {
		line = trimSpace(line)
		if rest, ok := cutPrefix(line, "package "); ok && rest != "" {
			return rest, true                 // ← 直接返回整行剩余部分
		}
	}
	return "", false
}
```

两种输入会产出非法包名：

| 输入 | 得到的包名 | 后果 |
|---|---|---|
| `package main\r\n`（CRLF） | `main\r` | 生成产物 `package main\r` —— 非法标识符 |
| `package main // 入口包` | `main // 入口包` | 包名连注释一起被收走 |

Windows 上手写 `.go` 文件默认 CRLF，这不是边缘场景。
而错误不会在这里暴露：包名一路流到 Codegen，在生成产物里才炸，
**报错点离真正的成因很远**。

## 根因

自实现的 `trimSpace` 只做了"前导裁剪"这一半；
`return rest` 又把"包声明行剩余部分"当成了"包名"——而 Go 的包声明行
完全可以带行尾注释。

两者叠加：`\r` 属于空白字符，所以只要后续有一次按空白切分，`\r` 就消失；
旧代码没有这一步，于是 `\r` 直接进了包名。

## 修复

```go
line = strings.TrimSpace(line)                 // ① 裁两端，含 \r
...
fields := strings.Fields(rest)                 // ② 取第一个 token，切掉行注释
if len(fields) == 0 { continue }
name := fields[0]
if !token.IsIdentifier(name) { continue }      // ③ 非法标识符跳过，不静默污染
return name, true
```

三处构成**双保险**：CR 由 ① 或 ② 任一处都能处理掉；② 额外负责行注释；
③ 保证非法名字不会被当成包名继续往下传（用标准 `go/token.IsIdentifier`
而不是自写边界，避免再出现同类裁剪遗漏）。

顺带把辅助函数 `errors_`（为避开 `errors` 包而造的怪名）改名为 `sourceLines`，
并删掉只裁前导的 `trimSpace`。

## 回归测试

新增 `internal/cli/package_name_test.go`，8 个子用例：LF 常规 / CRLF / 行注释 /
前导空白 / 注释行干扰 / 非法标识符跳过 / 无 package 子句 / 空源码，
外加一条 `TestParsePackageNameRejectsCROnly`。

**变异取证**（2026-10-23 执行，两次）：

```text
① 只退化 Fields（保留 TrimSpace）：
   --- FAIL: TestParsePackageName/行注释不得被当成包名的一部分

② 同时退化 TrimSpace→TrimLeft 与 Fields（即完整旧实现，保留 IsIdentifier 以免编译失败）：
   --- FAIL: TestParsePackageName/CRLF_不得把_\r_带进包名
   --- FAIL: TestParsePackageName/行注释不得被当成包名的一部分
```

还原后复跑 `exit 0`。

> 一次被作废的变异：把三处一起删掉曾得到 `[build failed]`
> （`go/token` 变为未使用 import）。**编译失败造出的红灯不算证据**，
> 已在 `docs/08-踩坑/踩坑-014-三种假绿...` 记为纪律。

## 关联

- `PIT-014`：三种假绿 —— 本条是"编译失败的红灯不算证据"的又一次实例。
- `BUG-007`：同为"死代码/隐性缺陷"，都是靠审查而非测试发现的。
