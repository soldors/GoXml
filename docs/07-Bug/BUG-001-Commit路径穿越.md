# BUG-001：`pipeline.Commit` 可被 `OutputName` 写到目标目录之外

## 元信息

- 状态：**Fixed**（2026-10-19）
- 发现版本：`ebab6b5`
- 发现日期：2026-10-19
- 发现方式：`TASK-SEC-001` 安全审查的实证探针
- 影响范围：`internal/compiler/pipeline.Commit`
- 关联任务：`TASK-SEC-001`
- 修复提交：`d4f3e38`
- 关联文档：`docs/03-架构/安全边界与威胁模型.md` §3.1

## 现象

`Commit` 把 `Artifact.OutputName` 直接拼进目标路径，未做任何校验。
调用方（或任何能构造 `Artifact` 的代码）可以通过 `../` 把生成文件写到
指定目录之外。

复现（探针测试，修复前）：

```go
outside := t.TempDir()
inside  := t.TempDir()

a := &Artifact{
    OutputName: "../" + filepath.Base(outside) + "/evil.go",
    Source:     []byte("package evil\n"),
}
path, err := Commit(a, inside)
```

实际结果：

```text
path = "C:\Users\...\Temp\TestProbePathTraversal298198949\001\evil.go"
err  = <nil>
→ 文件确实被写到 inside 之外
```

期望结果：`Commit` 返回错误，且不在目标目录之外产生任何文件。

## 根因

```go
// 修复前
target := filepath.Join(dir, a.OutputName)
```

`filepath.Join` 会**规范化**路径，`../` 因此生效。代码假设
`OutputName` 是可信的纯文件名，但这个假设不成立：

```go
// Artifact 的字段全部导出
type Artifact struct {
    PackageName string
    SourcePath  string
    Source      []byte
    OutputName  string   // ← 调用方可任意赋值
}
```

`Commit` 是**唯一**写盘的地方，合同上它必须自己防守，不能信任上游。

## 修复

新增 `validateOutputName`，要求：

| 规则 | 拦截目标 |
|---|---|
| 非空 | — |
| 不含 `/` 或 `\` | 目录穿越、写到子目录 |
| 不是 `.` 或 `..` | 目录引用 |
| 不以 `.` 开头 | 隐藏文件、`..foo` 之类歧义 |
| 以 `.go` 结尾 | 生成物必然是 Go 源码 |

```go
if err := validateOutputName(a.OutputName); err != nil {
    return "", err
}
```

## 回归测试

`TestCommitRejectsPathTraversal` 覆盖 9 种非法名：

```text
../evil.go     ../../evil.go    ..\evil.go
sub/evil.go    sub\evil.go      .
..             .hidden.go       evil.txt
（空串）
```

并断言两条不变量：

1. 目标目录**之外**不得出现任何文件；
2. 目标目录内不得有残留（含临时文件）。

另有 `TestValidateOutputNameAcceptsPlainName` 确认合法名不被误伤。

## 这条 Bug 揭示的问题

不是「少写了一个校验」，而是**信任模型缺失**：

- 安全审查前，`Artifact` 被默认为「Codegen 的产物」；
- 实际上它是普通的导出结构体，任何调用方都能构造；
- 因此凡是对它做假设的代码（不只 `Commit`）都需要重新审视。

审查报告已把 `Artifact` 标为**半可信**，并写入信任边界表（§1.2）。

## 关联

- 同批审查还量化了两处资源风险（Parser 深度、`<Def>` 膨胀），标注为**未修**，
  理由见安全审查报告 §3.2 / §3.3。
- 本 Bug 的处置方式（修复 + 回归测试 + 写入准入清单）应作为后续缺陷的模板。
