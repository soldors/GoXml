# BUG-008：`TestA54NestedBench` 只打日志、无任何断言

## 元信息

- 状态：**Open**（未修）
- 发现日期：2026-10-23
- 发现方式：为补写踩坑 / Bug 文档，逐项检查"测试里到底有没有断言"
- 影响范围：`internal/compiler/codegen/a54_bench_test.go`
- 关联：`PIT-014`（三种假绿）、`docs/03-架构/性能预算与优化策略.md`

## 现象

`TestA54NestedBench`（`internal/compiler/codegen/a54_bench_test.go` 第 9 行起）
遍历三个形态（Single / Column3 / Nested），对每个形态跑一次基准_module，
然后把结果原样打印：

```go
out := benchModule(t, src, probe)
for _, line := range strings.Split(out, "\n") {
    if strings.HasPrefix(line, "Benchmark") {
        t.Logf("[%s] %s", c.label, strings.TrimSpace(line))
    }
}
```

整个测试里**没有** `t.Errorf` / `t.Fatalf` / `t.Skip` 之外的任何失败路径——
即**没有任何断言**（不过滤 `-v` 时，这些 `t.Logf` 甚至不会显示）。

## 根因

这是一支**测量型**测试，作者的本意是"把三个形态的缓存收益打出来给人看"，
属于调试脚手架被留成了测试。它没有把任何数值钉死为契约，
因此也就无法在数值退化时报警。

## 影响

- **它永远 PASS**，所以"这一条通过了"提供不了任何保证；
- 真正的风险在于：**它会让人以为 A5.4 的缓存收益是"测过的"**。
  性能一旦退化，最有可能先出现在 `Layout` / shaping 的热路径上（见 `PIT-014`），
  而这支测试恰好覆盖的就是这条路径——却没有门槛。

按项目既有结论，这条**不算覆盖**。

## 修复

二选一（需所有者裁决）：

1. **补断言**：把 `BenchmarkNoCache` / `BenchmarkFirstRecord` / `BenchmarkCacheHit`
   三者的 `allocs/op` 与 `ns/op` 解析出来，按 `性能预算与优化策略.md` 的口径断言
   （例如：稳定帧文本路径 `allocs/op == 0`；缓存命中显著优于无缓存），
   并把"退化后仍是零值/负优化"当作失败而不是通过。
2. **降级并注明**：若只想保留观测能力，则应把它从 `Test*` 改名、或在文档/任务卡里
   明确标注"本例只打日志，无断言，不作为验收依据"，避免它被计入覆盖率。

无论哪种，都要先确认**本机墙上时间（wall-clock）的数值不能直接写成跨平台阈值**
（见 `PIT-013`：跨平台结论不得外推）。

## 回归测试

修完之后，验证"它真的会红"：

```bash
# 制造一次性能退化（例如临时让 BindCache 失效），期望：
go test -run TestA54NestedBench ./internal/compiler/codegen/  → FAIL
# 还原后
go test -run TestA54NestedBench ./internal/compiler/codegen/  → PASS
```

**先证明能变红，再放绿**——这正是本项目对每条断言的要求。

## 关联

- `PIT-014`：同一类 fail-open，三种形态一起看。
- `docs/03-架构/性能预算与优化策略.md` §3.2 / §6.1：若补断言，阈值口径以此两处为准
  （注意 `BUG-002` —— §4 预算表缺限定词，不要拿它当唯一依据）。
