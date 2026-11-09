# 2026-11-09 R27-A 集中返修 R2（HYDRATION-PROTOCOL）开发日志

返修父 = R1 冻结 `09b40d379a077f54dcd1af432bb4c71a9b0a04d4`（协调者 review-repair-r1 BLOCK）。分支 `codex/r27-r2-deepseek-hydration`，树 `r27-r2-deepseek-hydration`。主库 `d6b1fc0` 只读未动。R1 树/旧交付/审查证据只读不改。
提交：`09b40d3`（父）→ `9ef0c9e`（五项修复）→ 本 dev-log。

## 逐 finding：原红 → 新绿（同协调者材料）
- **F-A1-R1** 捕获后、提交前取消仍改写状态：定义**提交线性化点**——在接收捕获结果处与 `WriteAtomic` 前**两次复查取消/超时**；取消发生在提交前 => 返回 context 错误、旧状态字节/mtime 不变（不调用 WriteAtomic）；提交成功后才到的取消不回滚。原红 `TestCoordR27R1CancellationAfterCaptureKeepsState` FAIL；新绿 PASS。
- **F-A9** captureSem 惰性初始化 DATA RACE：改 `sync.Once` 安全初始化；单 channel、上限≤4；超时但回调未退仍占槽、迟到释放。原红 `TestCoordR27R1ConcurrentCaptureBoundIsRaceFree` **DATA RACE**；新绿 -race PASS。
- **F-A5-R1** lastPublished 冒充在服务进程：新增可选 `ServiceObserver` 端口 + `ProcessManager.ServingGeneration()`（以 `Current()/Running()` 为准）。Watch 用**真实在服务身份**请求快照；无在服务进程（冷启动/gen2 启动失败后服务已空）=> 不向死进程索取，按冷启动/已有有效快照让新代恢复。仅实现 `Swap(error)` 的自定义 Swapper 回退 lastPublished（兼容，文档说明）。原红 `TestCoordR27R1RealManagerLostServiceAfterFailedSwapCanRecover` FAIL（gen3 仍向死者 gen1 请求）；新绿 PASS。保持 stop-old→start-new。
- **F-A6-R1** 捕获后复扫/分类错误被吞仍发布：捕获后 `Scan`/`ClassifyChanges`/`NotifyChange` **任一失败 fail-closed**——停止本代、报告真实阶段（rescan/reclassify/requeue）、不 Swap、不谎报 published、保持旧服务。原红 `TestCoordR27R1CaptureScanFailureCannotPublish` FAIL（Manifest 坏仍发布 gen2）；新绿 PASS。保留捕获窗口 stale 重排、取消复查、状态写不自激。
- **F-A7-R1** E2E 绕开 Watch：重写为**真实 `watch.New` + NotifyChange/Tick** 驱动 runGeneration（首次发布→变更→捕获→stop-old/start-new→新代恢复），不手工调 hook.BeforeSwap/pm.Swap；保留新进程自报 pid/session/gen/nonce/精确 DTO/restored，父验证子报告。**同一健康 E2E 在两种可编译生产退化 overlay 下真实 FAIL**：①关闭 Watch Hydration 分支；②关闭子进程恢复（console 忽略 HYDRATE_STATE，嵌套 build 经 `HYDRATE_CONSOLE_OVERLAY`）。新绿 PASS；两退化均 rc=1。

## 变异/退化（独立副本 + Go overlay，红→恢复绿；健康→编译→行为红→字节恢复→同守卫绿）
- **MUT-1** 移除提交前取消复查 → `TestCoordR27R1CancellationAfterCaptureKeepsState` **FAIL**（raw/mutations-mut1-red.txt）。
- **MUT-2** sem 回退不安全惰性初始化 → `TestCoordR27R1ConcurrentCaptureBoundIsRaceFree` **-race DATA RACE**（raw/mutations-mut2-red.txt）。
- **OVL-A** 关闭 Watch Hydration 分支（生产源 overlay）→ 同一 E2E `TestTwoProcessHydrationRestoreViaWatch` **FAIL**（raw/mutations-ovlA-hookbypass-red.txt）。
- **OVL-B** 关闭子进程恢复（console 源 overlay，嵌套 build）→ 同一 E2E **FAIL**（raw/mutations-ovlB-childrestore-red.txt）。
- 恢复（无 overlay）→ 三包全 PASS（raw/mutations-restore-green.txt）。真红非编译/flags。

## 测试适配（仅 premise/helper，判据不降级）
新 E2E 的快照请求/响应/报告/旧值文件放在 **projectDir 之外**的 t.TempDir()，否则捕获期间写入会被 Watch 当作新用户输入（F-A6 复扫判 stale）——测试框架问题，非产品缺陷；断言不变。协调者 R1-secondary 探针原样保留在 `zz_coord_r1sec_test.go`（现绿）。未删健康断言、未加 Skip。

## 门禁（Windows/amd64，CGO=0；race MinGW）
三包全绿；`-race` 三包 rc0/0 DATA RACE；`go build ./...` rc0；**GOOS=linux CGO=0 build 三包 rc0**（交叉编译非运行）；vet rc0；gofmt 空；diagdoc/docgate ok；覆盖 runtime 88.7% / devx 86.0% / watch 88.4%（均≥85%）；台账见交付。

## 候选/未证边界
协议/timeout/路径/失败策略仍候选；无原生窗口/视觉/IME；无跨平台运行（仅 GOOS=linux 交叉编译）；无采样；helper 仅自有 console、有界、不开窗、不跑用户 main/init。

## 五态
实现 done(`9ef0c9e`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
