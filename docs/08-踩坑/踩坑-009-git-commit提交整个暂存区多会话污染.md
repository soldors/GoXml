# PIT-009：`git commit` 提交的是整个暂存区，多会话共享工作树会互相污染

- 分类：工程纪律 / Git
- 关联：`docs/07-Bug/`（工作树串线事故）、`docs/05-开发/dev-log/2026-10-23.md` §19.1

## 现象

在同一棵 **共享工作树**（多人 / 多 Agent 会话共用）里，自己的改动提交上去了，
但**同一批里还夹着别人的文件**；反向也发生过——别人的提交里夹进自己的东西。

这类事故有两个特征：

1. 提交说明写的是自己那件事，但 diff 里有完全不相干的文件；
2. 事故不是"提交失败"，而是"提交多余内容"，事后要用 `reset`/`revert` 补救。

## 根因

```bash
git add -A            # 或不带路径的 add
git commit -m "..."   # ← 提交的是**整个 index**，不是"我刚改的那个文件"
```

`git commit` 在不给路径时，提交 index 的**全部**内容。
共享工作树的 index 是多个会话共写的，谁先 commit 谁就把当时的 index 整体带走。

与之配套的两个坑：

- 只看 `git status` 的"自己那几行"是不够的，**必须看 `git diff --cached`**；
- 看到他人正在同一工作树活动，正确动作是**立刻停手**，而不是"我小心点就好"。

## 解决

按项目纪律执行三步：

```bash
# 1. 只在自己的专用空间干活：独立克隆（--no-hardlinks）或 worktree
git worktree list            # 先确认自己在哪棵树、哪条分支

# 2. 按路径 add，并立刻用同一批路径 commit
git add -- "docs/03-架构/A.md" "docs/03-架构/B.md"

# 3. commit 之前再复核一次 index 里到底有什么
git -c core.quotePath=false diff --cached --name-only   # 应**恰好**是那几个文件

git commit -F - -- "docs/03-架构/A.md" "docs/03-架构/B.md" << 'EOF'
提交说明
EOF
```

参数顺序注意：`-F -` 必须在 `--` **之前**；写成 `-- <paths> -F -` 会报
`pathspec '-F' did not match`。

PowerShell 传数组要用 `-Command`，`-File` 会把 `'a','b'` 合成单个字符串 `"a,b"`。

## 预防

- 共享主工作树 `D:\Codes\Goxml` 已发生过多次串线，**只在确认独占时使用**。
- 提交前 `git diff --cached --name-only` 是硬动作，不是可选项。
- 发现他人在同一工作树活动 → 停手，只记录归属并上报，不抢 index。

## 数据来源

- `docs/05-开发/dev-log/2026-10-23.md` §19.1（四次提交均走 `add --` + `diff --cached` 复核）
- `scripts/commit-push.ps1` 第 103-117 行（按路径 add / rm，并对清单外暂存报错）
- 项目工作树分布与用途见 `docs/05-开发/dev-log/2026-10-22.md` 的整合章节
