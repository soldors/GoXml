# R27 协调者：批准基础范围后的隔离组合

负责人批准 Windows 基础范围进入组合验收及 API 收敛，尚未批准本轮开窗执行，也未完成发布。三位作者无需再次返修；协调者在独立树完成接线、门禁与原生验收准备。

## 提交身份

- A 冻结 `80210080b0f365d5b8c9ec6cdb9dbc41e74dc19c`；组合 `321ba15b55baffd5877fafb32f698bab66f6ede5`。
- B 冻结 `f1a92da60b73365a73574f9764c5d550c9fb79e5`；组合 `1b6fdabc3c90fae7101b0a97ee7b66b4860a4132`。
- C 冻结 `7c0d07f7b57b0eabbbb9ba85978b08c611a687b5`；组合 `94335f8b7f4e874a0e1e824bd283ad4c00e05dd2`。
- API 收敛与自有验收入口 `63fc5e056455ffbf514c966846bf801f290fbe8c`。

## 变化与判据

ProcessOptions 的四项自检注入和 ExitResult 的步骤序列转包内，公开仅保留生产字段。Theme/Lazy 歧义在开窗前拒绝。原 console helper 迁至包内测试二进制子进程，保留 normal/destroy/theme/strict 四场景真实进程 rc、marker、observer-before-exit 及 cleanup-before-result 断言，测试父进程不退出。新增公开字段形状和歧义拒绝守卫。

原生程序改为只消费公开 API，加入 10k DataGrid 场景；font-strict 用显式失败 Provider（非系统故障），invalid-options 证明开窗前拒绝。原生证据由真实 callback/cleanup 标志与 marker 给出，内部步骤未造假外推。所有原生程序当前仅 build。

定向 API 测试与 fixture 构建已通过。完整组合门禁和后续原生结果在同日验收文档及协调者交付补记；记录完整性与业务 PASS 分开。基线 d6b1fc0 主库只读，三位作者原冻结不改，当前未合主库、未推送。
