# PIT-014：三种"看起来绿、其实没验"的假绿

- 分类：测试与门禁 / fail-open
- 关联：`internal/docgate`、`docs/05-开发/dev-log/2026-10-23.md` §1.1、§2

## 现象

本仓库已经踩过三种形态的"假绿"——它们的共同点是：**退化为通过，而不是失败。**

### (a) `t.Skip` 兜底：能力丢了，测试照样绿

```go
if !hasBinding {
    t.Skipf("本样例未命中绑定：%s", name)   // ← 与 PASS 一样是绿的
}
```

在 0 帧环境、0 绑定命中的场景里，`Skip` 与 `Pass` 在 `-v` 输出上都显示为通过。
后果：真正的能力丢失被显示成"没问题"。

**同一机理的另一个变体**：真实窗口探针写成 `go test`。没有窗口环境时它会 `t.Skip`，
于是"没验"和"验过"在 CI 里长得一模一样。这类探针必须用 `go run` + 显式退出码，
0 帧时记为 **BLOCKED**，而不是让它落在 `Skip` 里。

判据：`-v` 输出里 `--- SKIP` 的行数必须为 **0**（契约已冻结的行为不得用 Skip 兜底）。

### (b) 超时 panic 被读成"测试失败"

```text
panic: test timed out after 10m0s
FAIL    github.com/goxml/goxml/internal/compiler/codegen   600.407s
```

对照：同一轮里**加了** `-timeout 30m` 的 codegen 是

```text
ok      github.com/goxml/goxml/internal/compiler/codegen   627.376s
```

——**慢，但不是失败**。两者的差别只有一个 `-timeout` 参数。

`go test ./...` 默认超时 10 分钟，而 codegen 全包实测 **520–627 秒**，
已经贴着上限。超时的表现是 panic 风格的失败堆栈，**不是**用例断言失败。

判据：全量必须显式 `go test ./... -timeout 30m`。

### (c) 非 verbose 输出里"看不到 SKIP" ≠ 没有 SKIP

```text
ok    github.com/goxml/goxml/pkg/runtime/theme   12.265s     ← 不打印 --- SKIP 行
```

`go test ./...` 不加 `-v` 时，`--- SKIP` 行根本不输出。
因此**不能**从"全量输出里 grep 不到 SKIP"得出"全仓零 SKIP"。

判据：零 SKIP 的结论只能来自加 `-v` 的那一轮。

## 根因

三者都是 fail-open：被测项缺失/超时/被跳过时，工具链默认选择"认为通过"。
Assert 之外的路径没有被感知。

## 解决与预防

| 形态 | 硬性判据 |
|---|---|
| Skip 兜底 | 契约已冻结的行为严禁 `t.Skip`；`-v` 里 `--- SKIP` 必须为 0 |
| 超时 | 全量一律 `-timeout 30m`；看到几百秒的 `FAIL <pkg> 600.xxx s` 先怀疑超时 |
| 零 SKIP 结论 | 必须来自 `-v` 输出；非 verbose 那轮要在记录里注明"SKIP 不可判定" |

> 与之配套的一条写文档纪律：**未验证就写"未验证"**。
> 把"没验"写成"已验证"，与本条三种形态是同一个错误。

## 数据来源

- `docs/05-开发/dev-log/2026-10-23.md` §1.1（全量 11m40s，codegen 627.376s；明确注明非 verbose 轮次不能判定 SKIP）
- `docs/05-开发/dev-log/2026-10-23.md` §2（`-v` 复核：`TestFontLazyLoad` 17 PASS / 0 SKIP；theme 全包 44 PASS / 0 SKIP）
- `internal/docgate/docgate_test.go` 第 192-215 行（门禁自带负例，证明它会失败）
