# Git 操作边界与自动推送治理

> 本文记录 2026-10-23 发生的两次**未经明确授权的自动推送**事实、裁决结论与已执行的治理动作。
> 原则：**技术正确 ≠ 流程授权完整**。内容正确不等于获得了推送授权。

## 1. 事实记录

### 1.1 两次自动推送

| 提交 | 说明 | 推送方式 |
|---|---|---|
| `6d8ce38a4a8194de0ca00bae603d416b95d34a79` | dev-log：登记 `f707378`（补 PIT-008~015 / BUG-002~008） | **post-commit hook 自动推送** |
| `e92e696748b982adb151e397586ccfbc8ede0edb` | dev-log：登记 `e7ea34b`（代码审查 6 处修复） | **post-commit hook 自动推送** |

两次均**不是**手工执行 `git push`，而是主工作树 `D:\Codes\Goxml` 的 `post-commit` 钩子
在网络恢复的那一刻自动生效。此前多轮 `git push` / `ls-remote` 均因网络不可用失败
（`502` / `Empty reply from server` / TLS 握手失败），钩子每次打印「⚠ 自动推送失败」后
**仍以 exit 0 结束**，于是提交照常产生、推送静默失败；网络一恢复，同一次提交就被推了出去。

远端状态经 `git ls-remote origin refs/heads/master` **实测**确认为 `e92e696`（与本地 HEAD 一致）。

### 1.2 CI 状态的证据边界

项目负责人已核对 GitHub 上 `e7ea34b` 与 `e92e696` 两条提交记录，并查询了 `e92e696` 的提交状态：
**当前接口未返回可用的 CI status checks**。

→ 因此**不得声称 GitHub CI 已通过**。本轮质量结论的依据**仅为本地测试日志**
（`docs/06-测试/原始日志/2026-10-23-测试矩阵/`），CI 状态为**未知**，不是通过。

## 2. 裁决（2026-10-23）

**内容层面与流程层面分开记录：**

| 层面 | 结论 |
|---|---|
| 内容 | **暂不回退** `6d8ce38` 与 `e92e696`，保留现有主线及其审查证据 |
| 流程 | 明确记录：这两次推送由 `post-commit` hook 在网络恢复后自动触发，**不能记为用户明确授权的推送** |
| 后续 | 在推送机制修正前，不得默认认为本地 `git commit` 只影响本地仓库 |

选择「认可现状 + 补说明」而非回退的理由：**先保留现有证据，避免在未核对影响前进行破坏性回退**
（`git push --force` 属破坏性操作，须所有者明确指示才执行，不主动执行）。

## 3. 已执行的治理动作

### 3.1 改造 `post-commit` hook

| 项 | 处置 |
|---|---|
| 原文件备份 | `.git/hooks/post-commit.autopush.bak`（md5 与原文件一致：`f2793d27b10a86a2faa7ceff6360d41d`） |
| 默认行为 | **不再自动推送**；提交后仅打印提示，提交只影响本地仓库 |
| 显式开启 | `GOXML_AUTO_PUSH=1 git commit ...`（按需、可审计） |
| 常规做法 | 提交后显式 `git push origin <branch>` |
| 附带修正 | 原版把 push 的 stdout/stderr 全部重定向到 `/dev/null`，失败时看不到真实错误（与 `PIT-010` 同源）；现仅在显式开启时不屏蔽输出 |

**验证**：不设开关时手动执行 `sh .git/hooks/post-commit` →
打印「自动推送已停用（默认）：本次提交仅影响本地仓库。」，`rc=0`，**未发生推送**。
另 `git push --dry-run origin master` → `Everything up-to-date`（远端与本地一致，无意外待推内容）。

### 3.2 全仓库配置核查（不凭"无钩子"的名称判断）

| 仓库 | 类型 | 激活 hook | `core.hooksPath` | 结论 |
|---|---|---|---|---|
| `D:\Codes\Goxml` | 主工作树 | 仅 `post-commit`（已治理） | 未设置（默认 `.git/hooks`） | 已受控 |
| `D:\Codes\Goxml-impl` | 独立克隆 | **无** | 未设置 | 无自动推送 |
| `D:\Codes\Goxml-docs` | 独立克隆 | **无** | 未设置 | 无自动推送 |
| `D:\Codes\Goxml-c2` | worktree（共享主 hooks） | 随主仓库 | 未设置 | 随 §3.1 一并受控 |

四个仓库的 `remote.origin.url` 均为 `https://github.com/soldors/GoXml.git`，无 `pushurl` 覆盖。

> 说明：`Goxml-c2` 是 worktree，`.git` 是文件而非目录，**共享主仓库的 hooks**，
> 因此主工作树的 hook 改造对它同样生效——不能只治理主工作树就算完。

### 3.3 未改动的部分

- **未删除** hook（保留能力，仅改默认行为）；卸载方式仍为删除本文件
- **未改动** `scripts/install-hooks.ps1`（该脚本属版本控制内容，改动会波及他人，待裁决）
- **未回退**任何远端提交

## 4. 后续纪律

1. **提交 ≠ 推送**。默认状态下 `git commit` 只影响本地仓库；推送必须显式执行 `git push`。
2. 需要"提交即推送"时**显式开启**：`GOXML_AUTO_PUSH=1 git commit ...`，且仅在对推送后果有明确判断时使用。
3. 核验远端状态**只认 `git ls-remote` 实测 SHA**，不认 hook 回显（hook 失败也 exit 0），
   也不认 `push` 的回显（见 `PIT-010`）。
4. 在独立克隆中提交（如轮四产物）时，仍需**先确认该克隆无激活 hook**，
   不能只凭"独立克隆"这个名称判断——本次已实测确认 `Goxml-impl` / `Goxml-docs` 无激活 hook。
5. 任何 force push / 回退远端的操作，**须所有者明确指示**才执行。

## 5. 恢复与复现

| 目的 | 方法 |
|---|---|
| 恢复原自动推送行为 | `cp .git/hooks/post-commit.autopush.bak .git/hooks/post-commit` |
| 单次临时自动推送 | `GOXML_AUTO_PUSH=1 git commit ...` |
| 完全卸载 hook | 删除 `.git/hooks/post-commit`（备份仍在 `.autopush.bak`） |
| 核查当前是否会自动推送 | `sh .git/hooks/post-commit`（默认应打印"已停用"且不推送） |

## 6. 相关记录

- 踩坑：`docs/08-踩坑/踩坑-010-*` —— post-commit 钩子不是推送证据（FF 不触发；失败仍 exit 0）
- 踩坑：`docs/08-踩坑/踩坑-008-*` —— 管道吞掉命令退出码（`push_rc=0` 实为 `tail` 的 rc）
- 综合：`docs/05-开发/2026-10-23-四轮工作综合.md` §7 待裁决清单第 1 项
