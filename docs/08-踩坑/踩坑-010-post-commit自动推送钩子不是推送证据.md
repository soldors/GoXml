# PIT-010：post-commit 自动推送钩子不可当作推送证据

- 分类：工程纪律 / Git 钩子
- 关联：`scripts/commit-push.ps1`、`docs/05-开发/dev-log/2026-10-23.md` §1、§19.1

## 现象

本项目主 worktree 装了 `post-commit` 自动推送钩子。三种情况下它**都不会**给出可靠结论：

```text
① fast-forward 合并 —— 钩子**完全不触发**
   git merge --ff-only b33ce6d
   → Updating d9cf0b9..b33ce6d；37 files changed
   → 本地 master 领先远端 41 笔，**远端一个字节都没动**

② 推送失败 —— 钩子打印失败信息，但退出码仍为 0
   [master 92f0b2f] docs(wbs): ...
   [hook] ⚠ 自动推送失败（提交已在本地产生）
   [hook]   修复后可手动执行： git push origin master
   $ echo $?   → 0

③ 推送成功 —— 输出常被后续命令滚屏或丢弃，看不到
```

因此："我没看到报错" ≠ "已推送"；"钩子跑绿了" ≠ "远端是最新的"。

## 根因

1. **`post-commit` 只在创建提交对象后运行。** fast-forward 只是把分支指针前移，
   没有新提交对象，钩子自然不触发——FF 之后"自动推送"这件事根本没发生过。
2. 钩子内部用 PowerShell + `Exit 0` 收尾，git 对钩子非 0 退出码的处理也容易被忽略，
   失败不会阻断已经完成的 commit（这是设计如此，但让它不能当证据）。

## 解决

**远端状态只有一个可信来源：实测读取远端引用。**

```bash
git ls-remote origin refs/heads/master refs/heads/<branch>
```

拿到输出后与本地 `git rev-parse HEAD` **逐个比对**，不看 push 回显、不看钩子的文案。

git 通道不可用时的**降级只读手段**（须在记录中标注为 API 核查，**不等于** `ls-remote` 实测）：

```bash
curl -s https://api.github.com/repos/<owner>/<repo>/commits/<branch> | head -c 200
# rc=200 时返回 JSON，其 "sha" 字段即分支头
```

验证"某个 SHA 是否真的到了远端"，可直接查该 SHA：

```bash
curl -s -o /dev/null -w "%{http_code}\n" \
  https://api.github.com/repos/<owner>/<repo>/commits/<sha>
# 404/422 = 不在远端；200 = 存在
```

## 预防

- 任何"已推送 / 已同步"的结论，必须给出 `ls-remote` 输出或上述 API 的 rc 与 sha。
- FF 整合后要**单独 push**——别以为合并完就同步了。
- 远端不通时，如实写"本轮无法核对远端 SHA"，并在网络恢复后补做，不用 push 回显顶替。

## 数据来源

- `docs/05-开发/dev-log/2026-10-23.md` §1（FF 到 `b33ce6d` 后本地 ahead 41，钩子未触发）
- `docs/05-开发/dev-log/2026-10-23.md` §1.2、§19.1（API rc=200 核到三条分支 SHA；
  API 查 `827ab41` 返回 422 = 确认不在远端；两次 commit 的 hook 回显"自动推送失败"）
