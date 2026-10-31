# 2026-11-01 GOFLAGS 与旧生成物恢复

基线为已发布 `196e619`。在隔离树组合原候选 `e8b4018`（本树 `622b0f6`）和非作者所有权修复 `eb3c3e8`（本树 `04da9f4`），补齐配置切换恢复，不改公开 CLI 语法/退出码或 GXML 语义。

## 实测缺陷与修复

1. 无 tag 生成 basepkg，保留旧产物切 tag 时，CLI 虽已选 taggedpkg，Analyzer 仍加载旧 basepkg 而 ROOT-002 拒绝。仅对本批未命中、Manifest 同源同输出、保护头与 Source 匹配、package 确实过时的产物建立只读 overlay；通过 pipeline.Config 传入 TypeEnv，构造时复制快照，再由正常 packages.Load 消费。旧文件直到既有事务提交前不改动。
2. 包名不变但 tag 选择的 VM 类型变化时，旧缓存可能假命中并跳过诊断。K5 纳入原始 GOFLAGS，缓存格式从 1 升到 2，旧缓存自然未命中；不删除旧缓存/Manifest。
3. 忽略所有已登记产物会让单页生成后遗留另一页的旧 package，形成错误成功。新增整包一致性检查：同目录有未包含在本批的过时自有产物时，失败关闭并提示一并生成。批量生成两页成功，单页失败保持旧产物与 Manifest。
4. 定包名排除仍需 Manifest、保护头与 Source 三方一致；仅登记或仅有头的用户源码继续参与多包冲突校验。

## 验证

真实 Run(generate)→离线 go build：无 tag→tag→切回→缓存命中字节/mtime不动；非法新上下文不可假命中；绑定失败保留旧文件；跨包 VM 的两页项目单页切换拒绝、整批切换构建成功；TypeEnv 快照不受调用者随后改 map/bytes 影响且磁盘不变。代码仍检查所有 packages.Errors，不改准入判据。

证据在 `parallel-product/coordinator/checks/`。`goflags-red3` 为两项有效行为红；前两次红是夹具漏 Goxml/Version 的我方错误，不作产品红证。`goflags-partial-red` 为新增单页误成功的有效红；修后 recovery-green 通过。第一次三个相关包全量通过后新增单页护栏，因此最终冻结状态还需非作者窄审及组合门禁，不沿用旧 SHA 的批准。

## 边界

只解决环境 GOFLAGS 的构建文件选择、过时包名生成物与缓存上下文。并非任意 GOOS/GOARCH 交叉配置验收，也不代表自动修复所有用户 Go 错误或引用尚未生成构造函数的引导依赖。没有磁盘暂移/删除来绕过加载，不扩大文件所有权。

原生窗口/性能采样/旧诊断工具/安装均0，重负载公共锁实际释放。所有源改在独立树，主仓 WIP 未触碰。源实现、自测、非作者复核、主仓接纳、远端发布分别登记。
