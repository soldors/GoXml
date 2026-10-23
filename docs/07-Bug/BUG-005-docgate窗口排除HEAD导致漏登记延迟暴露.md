# BUG-005：docgate 窗口排除 HEAD，最新一笔提交的漏登记被长期隐藏

## 元信息

- 状态：**Fixed**（2026-10-23，补登记后门禁恢复绿）
- 发现日期：2026-10-23
- 发现方式：C1 提交后复跑 `internal/docgate`，报出一条与本次改动无关的缺失
- 影响范围：`internal/docgate/docgate_test.go`（`TestDevLogCoversRecentCommits`）
- 关联：`PIT-011`（`core.quotePath` 让同一判定彻底失效）

## 现象

提交一笔内容改动之后照例复跑 docgate，得到与本次改动毫无关系的报错：

```text
--- FAIL: TestDevLogCoversRecentCommits
    docgate_test.go:164: 未在 dev-log 记录: 27aa03a fix(scripts): 护栏三个解释器级缺陷…
```

`27aa03a` 是主干上的脚本修复提交（见 `BUG-004`），确实从未登记过——
但它在此之前一直显示"绿"。

## 根因

门禁窗口的定义是**最近 10 个提交、不含 HEAD**：

```go
const recentCommits = 10   // docgate_test.go:24
commits := gitLog(t, root, recentCommits)   // --skip=1，跳过 HEAD
```

因为 `27aa03a` 当时**正好是 HEAD**，它长期不在被检查的集合里；
HEAD 前移一位后它进入窗口，这条陈年漏登记才暴露出来。

也就是说：**"提交后立刻跑门禁"并不能证明这一笔提交合格**——
它恰恰是被排除的那一笔。必须等下一笔提交压上去再跑。

配套机理（`PIT-011`）：若再叠加上 `core.quotePath` 的影响，
连"这笔提交是否算日志维护提交"的判断都会出错。

## 修复

- 补登记：在当天的 dev-log 中登记 `27aa03a`（含归属与门禁实况）；
- 保留并强化了两条可以被人工核对的判据：`exit 0` + 窗口内每个非维护提交的哈希都出现在 dev-log 中。

## 回归测试

`internal/docgate` 本体（`TestDevLogCoversRecentCommits`）仍为唯一自动门禁；此外本仓库还要求：

```bash
# 反向验证：故意改坏登记里的哈希，必须变红
sed -i 's/<sha>/<sha被改坏>/g' docs/05-开发/dev-log/2026-10-23.md
go test ./internal/docgate/    # 期望 FAIL，含「未在 dev-log 记录: <sha>」
# 还原后
sha256sum -c <备份的校验值>      # 期望 OK
go test ./internal/docgate/    # 期望 exit 0
```

2026-10-23 对本项目的 C5 登记做过一次该反向验证：
改坏哈希得 `--- FAIL: TestDevLogCoversRecentCommits` + `未在 dev-log 记录: 6eff102 …`；
还原后 `sha256sum -c` OK 且复跑 `exit 0`。

门禁自身也带负例（`docgate_test.go` 第 192 行起，`TestGateDetectsMissingCommit`），
证明它真的会失败，不是恒绿。

## 这条 Bug 揭示的问题

**"窗口不含 HEAD"是一个结构性空档**：任何"刚提交、还没登记"的空档期都不会被检出，
缺陷被推迟到下一次提交才暴露，届时看起来像"刚才那笔改动引入的问题"。

因此纪律确定为：内容提交与 dev-log 登记**必须分成两笔**，登记提交之后再跑一次门禁；
并且只把" HEAD 前移后的那次门禁"当作有效判据。

## 关联

- `PIT-011`：`core.quotePath` 会让中文路径上的同类判定永久失效。
- `BUG-004`：本次被延迟暴露的那笔提交内容。
