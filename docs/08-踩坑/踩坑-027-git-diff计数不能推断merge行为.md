# PIT-027：`git diff A..B` 的 D/M 计数不能推断 merge 行为

> **编号说明**：本条原拟 `PIT-020`。因集成树 `night-font-integration`(`c1da35e`) 已提交
> `踩坑-020-Windows-junction不能用symlink测试代替.md`（内容不同），为避免同号异义，本条改号 `PIT-027`。
> **内容未改动**，来源：WorkBuddy 2026-10-31 取证线。


## 现象

用 `git diff --name-status master..origin/docs/fix-inv-perf-scope` 统计分支差异，得到：

- A（新增）0
- M（修改）38
- **D（删除）107**

据此判断「把这个分支 merge 进 master 会删掉 107 个文件」，并据此否决 merge。**该判断是错的。**

## 根因

`git diff A..B` 是**两棵树之间的直接差异**，不是三向合并的结果。它报的 D 是「**A 有而 B 没有**」，
在本例中绝大多数是「master 在分叉**之后**新增、分支从未触碰」的文件，而不是分支的删除动作。

以 `merge-base` 为基准重新统计，真实情况（2026-10-31 实测）：

| 口径 | 结果 |
|---|---|
| `merge-base` | `27aa03a` |
| `git rev-list --count <branch>..master` | **95**（master 领先 95 个提交） |
| `git rev-list --count master..<branch>` | **4** |
| `git diff --diff-filter=D <merge-base>..<branch>` | **0 个删除** |
| `git diff --diff-filter=A <merge-base>..master` | **108**（master 分叉后新增，merge 会保留） |
| `git diff --name-status <merge-base>..<branch>` | 仅 3 个文件：For/If 契约 M + `dev-log/2026-10-23.md` A |

## 处理与预防

- 判断 merge 风险**必须用三向口径**：以 `git merge-base A B` 为基准看双方各自改了什么。
- 更直接：用 **`git merge-tree --write-tree A B`** 做**只读**预演（不动工作树、不动索引），它会直接列出冲突。
  本例实测输出唯一冲突：`CONFLICT (add/add): Merge conflict in docs/05-开发/dev-log/2026-10-23.md`。
- 「B 相对 A 缺了 N 个文件」和「merge 会删掉 N 个文件」是两件事，**不得混用**。
- 判断单个提交能否干净应用：确认该文件在目标分支与 merge-base **逐字节相同**（`git diff --quiet <merge-base> <target> -- <path>`），
  相同即 cherry-pick 无冲突。本例 `0647fbc` 的两个契约文件均满足。

## 数据来源

2026-10-31 裁决 2 复核（`workbuddy/rulings/facts.json`，断言 F05–F10）。分支 `origin/docs/fix-inv-perf-scope` @ `5b3caf5`。

关联：[PIT-026](踩坑-026-行尾差异在git-diff中隐身.md)、[PIT-022](踩坑-022-git修订引用大小写静默空diff.md)。
