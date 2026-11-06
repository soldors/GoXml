# 提交并同步到 GitHub

> **2026-11-06 全库同步说明**：本页保留早期提交/推送工具说明；隔离 agent 分支按已签发任务冻结，由协调者审查后合入发布，不能把“每次本地提交后立即推送 origin/master”解释成所有隔离分支自动获准发布。远端状态用 `git ls-remote origin refs/heads/master` 实测，origin/master 只是本地跟踪引用。

本仓库的固定工作流：**每次本地提交后立即推送到 origin/master**。

---

## 方式一：用脚本（推荐）

```powershell
# 提交（自动加 -a？不——先显式暂存，见下）
.\scripts\commit-push.ps1 -Message "feat(codegen): xxx" -Files "internal/compiler/codegen/codegen.go","docs/05-开发/dev-log/2026-10-19.md"

# 或者工作区已全部准备好（谨慎：会带上所有改动）
.\scripts\commit-push.ps1 -Message "docs: xxx" -All
```

脚本做四件事：
1. 暂停确认（`-Yes` 可跳过）；
2. 逐项打印**实际暂存的文件清单**（防并发会话内容混入）；
3. 提交；
4. 推送 `origin/master`，失败则明确报错且不假装成功。

---

## 方式二：Git 钩子（自动推送）

安装一次即可，之后 `git commit` 成功会自动 `git push`：

```powershell
.\scripts\install-hooks.ps1
```

卸载：删除 `.git/hooks/post-commit`。

> ⚠️ 钩子的代价：推送失败时**提交已经产生**（本地领先远端），
> 下次提交时会再次尝试推送当前分支。如果你在离线环境工作，
> 建议用方式一（显式推送）。

---

## 纪律（来自 dev-log 的两次事故）

1. **禁止 `git add -A`**——曾两次把并发会话文件 / `.tmp.driveupload` 垃圾带入提交。
   必须显式列文件，或至少在提交前逐项核对暂存清单。
2. 提交前核对：`git diff --cached --name-only`
3. 门禁在提交前跑：`gofmt` / `build` / `vet` / `test` / `diagdoc` / `docgate`
4. 推送失败不得忽略——远端落后会让协作者看到过期状态。

---

## 常用命令

```powershell
# 看本地领先远端多少
git status -sb

# 手动推送
git push

# 查看远端实际状态
git log origin/master --oneline -3
```

## 已发布的工程与验收入口

- [verify-projects.py](verify-projects.py)：四个 testprojects 共用复制、批次生成、构建、离屏测试入口。
- [verify-gallery-accept.py](verify-gallery-accept.py)：图库缓存/有效资产/变异的真实链路验收。
- [字体正式验收工具](font/README.md)：准备与正式执行区分；真实采样仍需独立授权。
- [证据记录协议](../docs/05-开发/2026-11-02-R8-证据工具使用说明.md)：原始双通道、真实退出码、公共锁及工具/业务判定分离。
