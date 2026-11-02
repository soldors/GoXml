# R5 CLI 最终独立复核与必要修复

基线为 DeepSeek 冻结 `efec33c`（包含其 `f2d6f11`、`cfec15c`、`efec33c` 三片）。作者树只读。独立审查树为 `codex/r5-final-cli`；本记录的“通过”只指本线实测，不代表已经合入主仓或完成最终三线组合。

## 发现与修复

1. **外部替换依赖仍可假命中**：原收集器只标记返回 error 的情况，固定 missing/truncated sentinel、被忽略的外部 import 没有禁用复用。真实 CLI 在本地 `replace example.net/ext` 的 `Name = string` 改成 `Name = int` 后仍 rc0 缓存命中。新版本对无法完整覆盖的外部模块、目录/文件链接、嵌套模块、闭包超限、导入解析失败显式置 `dependencyUnsafe`，不遍历仓外；真实 CLI 改坏后 rc2，旧生成物与 Manifest 字节保持。只用本模块/标准库的正常工程仍能真命中，字节、mtime、Manifest 均保持。
2. **根属性和模块路径的语法面**：用 XML 根属性读取 ViewModel（处理注释、单双引号），以 `modfile.ModulePath` 读取合法带引号的 module 路径；修复 import 恰好为模块根时错误拼子目录。以上均有正反对照。
3. **有效上下文的工作目录**：`go env` 在实际 moduleRoot 执行，测量 GOROOT 纳入上下文；项目 go.work 不再因启动 CLI 的当前目录不同而漏测。测量失败仍只禁缓存复用，让正常分析判定。显式 `--no-cache` 保持原 bypass reason，不因 unsafe 额外改变其输出策略。仍使用内部缓存格式4，未改语言版本或诊断号。
4. **跨错误类型的清理记账**：此前 remove 失败后再遇 read/stat 失败会丢失前一个 cause 和 failed 事实。现保留全部已完成/失败事实与全部 cause，并列出被中止后尚未尝试清理的 pending。错误文案打印全部 cause；原失败 Manifest 保留策略不变。
5. 修正相邻测试中“Go1.21 POSIX 语义使同进程无共享删除句柄不可靠”的无根据说明；真实 Windows 共享删除锁回归已在本机通过，注入重试测试与真实句柄证据分别标注。

## 验证与边界

独立新行为检查先红后绿，红为行为断言失败而非编译失败。外部替换模块真实 CLI 旧/新对照、普通本模块缓存命中对照通过。项目目录 go.work 测量、有效 GOENV、合法带引号 module/根属性、根模块 import、缺失/外部/截断/嵌套依赖、早期 remove + 后续 read 失败、真实 Windows 禁共享删除句柄等窄回归通过。公共 heavy.lock 均按自有 token 实测释放；一次锁忙未启动测试，记录保留，后续用唯一日志名执行。

原始命令与双通道证据在协调者 `night-product-r5/coordinator/final-closeout/cli/`，其中 `red-safety.*`、`green-context-and-clean-2.*`、`e2e-commands.json`、`e2e-result.json` 可逐项复算。最终全包覆盖、vet、构建与三线组合由协调者在唯一组合树执行，避免重复重负载。

0 原生窗口、0 新诊断/采样/profile/trace、0 安装、0 push；未修改原作者树、主仓、Background 或公开语义。缓存对外部模块采取明确保守 miss，不声称完整指纹所有依赖；将来扩展缓存命中范围须另实现可验证的依赖快照。
