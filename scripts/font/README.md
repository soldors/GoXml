# 字体测量探针

## 同步／异步比较口径：font-init-v1

本口径对应 D-ACCEPT-02 的“同步冷字体路径总耗时”取数准备；只定义可观测区间，
不设性能通过阈值，不改变 `App.FirstFrame()` 的 M1 定义或正式验收状态。

### 运行并行度与构建限流

构建阶段固定 `GOMAXPROCS=2`、`go build -p=1` 以限制构建资源，结束后恢复调用者环境；
这不应强制限制被测应用。`-RuntimeProcs -1`（默认）继承调用者的 GOMAXPROCS，
`-RuntimeProcs 0` 清除此环境变量，让 Go 自行决定；正整数则显式限制运行时。
每条新样本记录策略与探针 ENV 中实际观测的并行度；不同策略／观测值分组，缺少观测的新记录不得算有效。
旧记录保持其原始字段与历史条件，不自动追认为默认并行度样本。

2026-10-27 的 `7f6621c` 性能批次明确在 GOMAXPROCS=2 下执行，其结论仍适用于该条件。
此前采集器会让构建阶段的设置泄漏至样本，现已分离；默认并行度对照属于不同配置，
不能用更多核心带来的变化冒称代码优化收益，也不能覆盖旧批次。

| 字段 | 同步 sync | 异步 lazy |
|---|---|---|
| `fontInitAtNs` | 调用 `theme.NewThemeWithReport()` 之前 | 调用 `theme.NewLazy()` 之前，随后以同一 Lazy 调用 `NewLazyApp` |
| `fontReadyAtNs` | 上述调用成功返回后立即取时 | `Lazy.Report().LoadedAt`，后台最终主题构造完成的时刻 |
| `fontReadyElapsedNs` | `fontReadyAtNs - fontInitAtNs` | 同左；包含临时主题及 Request 的同步部分、后台加载和实际调度等待 |
| `fontToFirstFrameElapsedNs` | `App.FirstFrame().FirstFrameAt - fontInitAtNs` | 同左；不要求最终主题已在首帧前就绪 |
| 原 `m1ElapsedNs` | NewApp 构造入口至首帧处理开始 | NewLazyApp 构造入口至首帧处理开始 |

所有时间来自本进程 `time.Now()`，不注入虚拟时钟。同步止点是公开调用返回观测点，
异步止点是报告内已存在的产出时间，均表示最终主题可用；二者都不代表首次 shaping、
帧提交、物理呈现或进程启动。同步调用返回的极小封装成本无法单独剔除，须随结果披露。
异步主题就绪与首帧没有预设先后，禁止把两区间相减称为“净收益”。

### 样本条件

- 每个样本使用独立 OS 进程，启动后默认 Provider 未解析（Diagnostics 为 nil），
  最终报告必须 `ProbeRan=true`；这叫进程内 Provider 冷缓存，不宣称磁盘／OS 字体缓存冷。
- 同一可执行文件、Git 快照、源码哈希、机器与图形会话下交替执行 sync／lazy。
  两个场景分组统计，旧 M1 样本与本口径不可混合成一个分布。
- 比较模式只接受 sync／lazy。sync 的报告必须存在且无 Err；lazy 必须到 Ready、
  Err 为 nil、LoadedAt 非零。首帧已到但仍 Loading 不算完成时间样本成功。
- 起止必须存在且区间非负；零耗时可因粒度出现，带 Measured=true 保留，不能据此宣称零成本。
- 超时、失败、退出码未知／非零、被收集器终止或字段缺失均不得进入有效比较统计；
  原始输出和失败记录保留。不能用 sleep、删断言或重跑替换异常样本。

### 执行与解释

在收集器上显式开启 `-CompareFonts -Load sync` 或 `-CompareFonts -Load lazy`。
未开启时沿用原 M1 采样行为，不等待后台字体完成。比较模式中的原 M1 同样在首帧锁定，
后续等待最终主题不会移动其止点。汇总按 `font-init-v1` 分组并另外输出两个新增区间。

得到双方数字只说明比较机制已运行；是否满足“不超过同步路径总耗时”，需要指定样本量、
比较统计量、容差与正式裁决，不能凭单次快慢、none 基线或几个 smoke 样本关闭 D-ACCEPT-02。

每次使用全新输出目录；将下例的仓库、完整 HEAD 和输出根目录替换为被验快照：

```powershell
./scripts/font/collect-m1.ps1 -RepoDir C:/audit/Goxml -ExpectedHead <full-sha> -OutDir C:/evidence/sync-01 -Load sync -CompareFonts -Runs 1 -Strict -LockFile C:/evidence/heavy.lock
./scripts/font/collect-m1.ps1 -RepoDir C:/audit/Goxml -ExpectedHead <full-sha> -OutDir C:/evidence/lazy-01 -Load lazy -CompareFonts -Runs 1 -Strict -LockFile C:/evidence/heavy.lock
./scripts/font/summarize-m1.ps1 -InputDir C:/evidence/sync-01
```

同模式同来源多次记录可合并至新的 `samples.jsonl` 后统计，ID 必须保持唯一、不得覆盖原始记录；
sync／lazy 及旧 M1 跨组数据默认拒绝合并，`-AllowMixed` 仅分组输出，不会混算分布。
解析与统计回归：`./scripts/font/test-comparison.ps1`（合成 fixture，不弹窗，原始日志保留到打印的临时目录）。
