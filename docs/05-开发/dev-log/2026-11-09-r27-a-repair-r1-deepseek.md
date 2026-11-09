# 2026-11-09 R27-A 集中返修 R1（HYDRATION-PROTOCOL）开发日志

返修父 = 原冻结 `b2967b41bd8cd6d54e87759f359ccfa40cdb89f0`（协调者 review-1 BLOCK）。分支 `codex/r27-r1-deepseek-hydration`，树 `r27-r1-deepseek-hydration`。主库 `d6b1fc0` 只读未动。旧 delivery/raw/冻结树/审查 raw 只读不改。
提交：`b2967b4`（父）→ `2aa9881`（F-A1..A8 修复）→ 本 dev-log。协调者探针复制到本线树（`zz_coord_r27_test.go`）作持久回归。

## 逐 finding：原红 → 新绿（同协调者材料）
- **F-A1** 捕获超时仍无界等待回调：`Snapshot` 超时/取消时**不 join 不合作回调**、有界返回不提交；回调迟到结果经缓冲通道丢弃；新增**有界在途并发** `captureSem`（满则 ErrBusy，不无限遗留 goroutine）。原红 `TestCoordR27UncooperativeCaptureIsBounded` FAIL（阻塞至1s）；新绿 PASS。
- **F-A2** Encode 改变合法 JSON 字节自毁 checksum：改为**规范（紧凑 JSON）校验和** `canonicalChecksum` + `Encoder{SetEscapeHTML(false)}`，使 pretty/`<tag>`/Unicode payload 的 Encode→Decode 往返成功；真实篡改（值变）仍 ErrChecksum。原红 `TestCoordR27PrettyPayloadRoundTrip` FAIL；新绿 PASS，`TestChecksumMismatch` 仍 PASS。
- **F-A3** 已取消 Restore 仍 Apply：`Restore` 应用前**复查 ctx 取消**；取消/过期不调用 apply。库不声称能自动回滚任意用户回调（注释明确）。原红 `TestCoordR27CanceledRestoreDoesNotApply` FAIL（applied=true calls=1）；新绿 PASS。
- **F-A4** Store 未证明所有权即覆盖：`WriteAtomic` 提交前 `existingIsOwned`——目标已存在且**非本会话**（Envelope.SessionID 不符/不可解析/超大）→ **拒绝覆盖，保旧字节/mtime**；读取改为**有界 readBounded（≤MaxEnvelopeBytes）**不再整份 os.ReadFile。原红 `TestCoordR27ForeignExistingFilePreserved` FAIL（覆盖丢字节）；新绿 PASS。
- **F-A5** 冷启动/失败代次身份错（gen-1）：`BeforeSwap(ctx, newGen, oldGen)`，oldGen 取 **w.lastPublished**（真实已发布/在服务代次）；oldGen==0（冷启动无旧进程）**合法跳过**快照直接发布。原红 `TestCoordR27ColdStartHasNoOldGeneration`、`TestCoordR27OldGenerationFollowsPublishedProcess` FAIL；新绿 PASS（gen1成功/gen2失败/gen3成功 => capture-old=1）。
- **F-A6** 快照窗口内新输入未重验即发布落后构建：`BeforeSwap` 后、`Swap` 前**重扫用户输入 + 复查取消**；用户输入变化 => 本代 stale、保旧、重排；本代 .goxml 状态写入经所有权过滤**不自激**。原红 `TestCoordR27InputChangeDuringCaptureCannotPublish` FAIL（发布 gen2）；新绿 PASS（保持 gen1）。
- **F-A7** 双进程恢复守卫未验新进程/未走 Watch：e2e 改为**真实 Watch opt-in 链**（Hook + pm.EnvFor(RestoreEnv)），**新进程自报** report 文件（pid/session/gen/nonce/精确 DTO/restored），父进程验证**子报告**而非重读父状态；加严：禁用子恢复时同一守卫拒绝（restored=false/default）。原红（禁用子恢复仍 PASS，盲）；新绿：恢复 e2e PASS + 禁用子恢复 rejection PASS。
- **F-A8** devx 83.5% 未达门禁：补真实失败路径（协议非法/超大、Store 覆盖成功/不可解析拒绝/目录冲突/读取失败等），当前源码 **runtime 88.7% / devx 85.6% / watch 87.6%**（均 ≥85%，不凑数、不改分母）。

## 两条可编译行为变异（独立副本 + Go overlay，红→恢复绿）
- **MUT-1** `Snapshot` 超时回退为 join → `TestCoordR27UncooperativeCaptureIsBounded` **FAIL**（raw/mutations-mut1-red.txt）。
- **MUT-2** `WriteAtomic` 去掉所有权检查 → `TestCoordR27ForeignExistingFilePreserved` **FAIL**（raw/mutations-mut2-red.txt）。
- 恢复（无 overlay）→ 三包全 PASS（raw/mutations-restore-green.txt）。真红非编译/flags。

## 测试适配（仅 premise/helper，判据不降级）
`TestWatchHydrationCaptureFailureKeepsOld`/`TimeoutKeepsOld` 按 F-A5 冷启动语义**先暖起 gen1 再驱动 gen2**（capture 仅在“有旧进程”时发生），断言仍为“不切换、保 gen1”。协调者 R27 探针原样保留在 `zz_coord_r27_test.go`（现全绿）。未删健康断言、未加 Skip。

## 门禁（Windows/amd64，CGO=0；race MinGW）
runtime/devx/watch test 全绿；`-race` 三包 rc0/0 DATA RACE；`go build ./...` rc0；**GOOS=linux CGO=0 build 三包 rc0**（交叉编译非运行）；vet rc0；gofmt 空；diagdoc/docgate ok；覆盖 runtime 88.7% / devx 85.6% / watch 87.6%；台账见交付。

## 候选/未证边界
协议/timeout/路径/失败策略仍候选（未批准）；无原生窗口/视觉/IME；无跨平台运行（仅 GOOS=linux 交叉编译）；无采样；helper 仅自有 console、有界、不开窗、不跑用户 main/init。

## 五态
实现 done(`2aa9881`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
