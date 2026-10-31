# PIT-026：行尾差异在 `git diff` 中隐身，恢复判据只能是字节 SHA

> **编号说明**：本条原拟 `PIT-019`。因集成树 `night-font-integration`(`c1da35e`) 已提交
> `踩坑-019-Go工具准备与样本解析应分阶段.md`（内容不同），为避免同号异义，本条改号 `PIT-026`。
> **内容未改动**，来源：WorkBuddy 2026-10-31 取证线。


## 现象

在「改文件 → 跑取证 → 恢复原状」的变异取证流程里，恢复后的文件 `git diff` **显示为空**、`git status` 也不报改动，看起来已经完好还原；但字节 SHA-256 **与基线不同**，文件实际已被整篇改写。

实测数据（`internal/cli/pipeline.go`，2026-10-31）：

| | 字节数 | CR LF | LF |
|---|---:|---:|---:|
| 基线（克隆检出后的磁盘文件） | 18049 | 0 | 466 |
| 用 Python 文本模式写回后 | 18515 | 466 | 466 |

## 根因

仓库根目录 `.gitattributes` 含：

```
* text=auto eol=lf
*.go text eol=lf
*.md text eol=lf
```

即工作区文件**必须是 LF**。而 Windows 下 Python 的 `Path.write_text()` / `open(..., 'w')` 默认 `newline=None`，写入时把每个 `\n` 翻译成 `os.linesep`（Windows 为 `\r\n`）。于是：

1. 写入后文件变成 CRLF，字节数 +466；
2. Git 在 diff/status 时按 `eol=lf` **归一化** CRLF → LF，与索引中的 blob 一致 ⇒ **diff 为空**；
3. 只有**不经过归一化**的字节 SHA-256 能发现差异。

也就是说，在这个仓库里 **「`git diff` 为空」根本不能证明「文件内容没变」**。

## 处理与预防

- 「改了再恢复」的**恢复判据必须是字节 SHA-256**，不能是 `git diff` / `git status`。
- 变异、恢复一律用**字节级**读写：`read_bytes()` / `write_bytes()`，不要用文本模式。
- 文档（`.md`）同理受 `eol=lf` 约束；新增或重写文档后应确认落盘为 LF。
- 若已被污染，用 `git checkout -- <path>` 复位，并**再算一次 SHA 与基线比对**确认，不要只看 `git status`。
- 收口脚本里把「恢复后 SHA == 基线 SHA」写成**硬断言**，让它能变红。

## 数据来源

2026-10-31 M-G5/M-G6 变异取证第一次运行（`workbuddy/m-g56-evidence/raw-attempt1/`、`out-attempt1/`）：
`fileRestored: false`、`cloneDirty: "M internal/cli/pipeline.go"`，而 `git diff --numstat` 为空。
改用字节级读写后第二次运行：`fileRestored: true`、`cloneDirty: ""`、退出码 0。

关联：[PIT-027](踩坑-027-git-diff计数不能推断merge行为.md)。
