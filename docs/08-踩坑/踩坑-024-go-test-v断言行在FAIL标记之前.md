# PIT-024：`-v` 下 Go 的断言行打印在 `--- FAIL` 标记**之前**

## 现象

写脚本从 `go test -v` 输出里截取「失败文案」，做法是「从 `--- FAIL` 那一行开始，截到用例结束」。
结果只拿到：

```
--- FAIL: TestNamingConflictDiagnosticsNotDuplicated (2.54s)
FAIL
```

**真正的断言内容（`naming_c1_test.go:155: …` 以及紧随其后的多行诊断正文）全部丢失**。

## 根因

`go test -v` 对顶层用例的输出顺序是：

```
=== RUN   TestXxx
    file_test.go:NN: 断言消息
        多行补充内容
--- FAIL: TestXxx (2.54s)
FAIL
FAIL	pkg/path	2.597s
```

即 `t.Errorf` 的内容在 `--- FAIL` **之前**打印，只有耗时与「FAIL」在标记行里。
按「`--- FAIL` 之后」截取，天然丢掉断言正文。

## 处理与预防

- 变异取证 / 失败取证**必须保存完整 stdout 原始字节**（`raw/<tag>.stdout.bin`），不要只存截取片段。
- 需要结构化字段时，另外用**正则扫全文**提取断言行（形如 `<file>_test.go:<line>:`），而不是靠行序截取。
- 判「是否变红」用退出码 + 是否出现 `--- FAIL`；判「文案是否完整」用「断言行条数 ≥ 1 且含 `_test.go:`」，两者分开断言。
- 收口脚本里加**判别力护栏**：若 N 次运行里正则一次都没匹配到（`notFoundCount > 0`），必须报失败，
  不能把「没匹配到」当成「没失败」。

## 数据来源

2026-10-31 M-G5/M-G6 变异取证（`workbuddy/m-g56-evidence/`）：
首次截取只得到 2 行标记，改存完整 stdout 后取得完整文案（M-G5 1608 B、M-G6 1063 B），
断言位置分别为 `naming_c1_test.go:155` 与 `naming_c1_test.go:223`。
D-ACCEPT-08 复测脚本沿用此护栏，实测 `notFoundCount=0`。

关联：[PIT-008](踩坑-008-管道吞掉命令退出码.md)。
