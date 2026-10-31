# 踩坑 R2-WORKBUDDY-01：shell 变量命名为 `TMP` 会让 Go 忽略该目录的 go.mod

- 临时编号：**R2-WORKBUDDY-01**（R2 批次临时 ID，**不占全局 PIT 编号**，全局号由协调者归并时分配）
- 日期：2026-10-31
- 触发场景：在临时目录里拼一个独立 module 跑真实 `goxml generate`

## 现象

```text
go: warning: ignoring go.mod in system temp root D:\...\workbuddy\tmp\tasks
go: go.mod file not found in current directory or any parent directory; see 'go help modules'
```

随后 CLI 报：

```text
:2:20: GXML-ROOT-002: ViewModel "ViewModel": package "example.com/goxml-tasks" failed to load:
no required module provides package example.com/goxml-tasks: go.mod file not found ...
```

看起来像"Go 拒绝在名字含 tmp 的目录里用 go.mod"，很容易被记成一条假结论。

## 真实根因（实测确认）

触发它的脚本里有这一行：

```bash
TMP="D:/.../workbuddy/tmp/tasks"   # ← 我给 shell 变量起名叫 TMP
```

`TMP` **本来就是导出变量**（Windows 上 Go 的 `os.TempDir()` 取 `TMP`/`TEMP`）。在 bash 里给一个已在环境中的变量赋值，会**更新它的导出值**，于是子进程 `go` 看到 `TMP` = 我的工作目录。

`cmd/go/internal/modload/init.go` 的判定是：

```go
} else if search.InDir(modRoot, os.TempDir()) == "." {
    fmt.Fprintf(os.Stderr, "go: warning: ignoring go.mod in system temp root %v\n", os.TempDir())
```

即**只有当模块根恰好等于 `os.TempDir()` 时**才忽略。对照实测：

- 变量名改掉后，同一目录 `go env GOMOD` 正常返回该 go.mod；
- 同一环境下 `os.TempDir()` 实测为 `C:\Users\admin\AppData\Local\Temp`（见 `bin/tempprobe.exe` 输出）。

所以"目录名含 tmp 就不行"是**错的**；真正原因是环境变量被覆盖。

## 影响面

`go/packages` 加载失败会一路传成 `GXML-ROOT-002`，看起来像 ViewModel 或 package 路径写错，排查方向会被带偏。

## 防御

1. 脚本里**不要用 `TMP`/`TEMP`/`GOFLAGS`/`GOPROXY` 这类导出名当普通变量**；用 `TD`、`WORKDIR` 之类。
2. 出现 "ignoring go.mod in system temp root" 时，先 `echo "$TMP" "$TEMP"` 与实测 `os.TempDir()` 对照，再判断是不是环境问题。
3. 这条也解释了为什么 `t.TempDir()` 能正常工作：它是 `os.TempDir()` 的**子目录**，不等于 temp root，因此不会命中该分支。
