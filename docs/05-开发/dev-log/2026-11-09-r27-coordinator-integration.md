# R27 协调者：批准基础范围后的隔离组合

负责人批准 Windows 基础范围进入组合验收及 API 收敛，尚未批准本轮开窗执行，也未完成发布。三位作者无需再次返修；协调者在独立树完成接线、门禁与原生验收准备。

## 提交身份

- A 冻结 `80210080b0f365d5b8c9ec6cdb9dbc41e74dc19c`；组合 `321ba15b55baffd5877fafb32f698bab66f6ede5`。
- B 冻结 `f1a92da60b73365a73574f9764c5d550c9fb79e5`；组合 `1b6fdabc3c90fae7101b0a97ee7b66b4860a4132`。
- C 冻结 `7c0d07f7b57b0eabbbb9ba85978b08c611a687b5`；组合 `94335f8b7f4e874a0e1e824bd283ad4c00e05dd2`。
- API 收敛与自有验收入口 `63fc5e056455ffbf514c966846bf801f290fbe8c`。
- 范围及现行文档同步 `081fa2d07e29994494c2e7718e363da806ae16ac`。
- 无窗口收口文档 `66651497a99df23f0b711d364227c3eec9ddf6b9`。
- 无桌面控制的自有原生关窗 fixture `7ce0c0de89567eb4bf98a2f3deff1656edf1ad6f`。
- 原生限定验收与文档收口 `d28d463a15005588aab7b75f840349f5f9c0864a`。

## 变化与判据

ProcessOptions 的四项自检注入和 ExitResult 的步骤序列转包内，公开仅保留生产字段。Theme/Lazy 歧义在开窗前拒绝。原 console helper 迁至包内测试二进制子进程，保留 normal/destroy/theme/strict 四场景真实进程 rc、marker、observer-before-exit 及 cleanup-before-result 断言，测试父进程不退出。新增公开字段形状和歧义拒绝守卫。

原生程序改为只消费公开 API，加入 10k DataGrid 场景；font-strict 用显式失败 Provider（非系统故障），invalid-options 证明开窗前拒绝。原生证据由真实 callback/cleanup 标志与 marker 给出，内部步骤未造假外推。所有原生程序当前仅 build。

定向 API 测试与 fixture 构建已通过。完整组合门禁和后续原生结果在同日验收文档及协调者交付补记；记录完整性与业务 PASS 分开。基线 d6b1fc0 主库只读，三位作者原冻结不改，当前未合主库、未推送。

## 无窗口门禁收口

源码态 081fa2d 九包 test/race、CLI 完整兼容回归、build/vet/gofmt、docgate/diagdoc、nativewindow 标签仅编译均通过。八个直接受影响包覆盖率 86.0%–95.6%；未改 Preview 的单包覆盖 327/385，与主库逐块完全一致。联合现有 CLI Preview 四用例的跨包覆盖后为 329/385（85.45%），保留同源码 profile 及合并来源，不凑分母。

新增公开字段泄漏与跳过 Observer 两个外部副本均先 build rc0，再使健康守卫 rc1，移除 overlay 后恢复绿；未改生产源码做变异。唯一采样 opt-in SKIP 未被算作采样 PASS。原生二进制下一步固定 SHA，只构建，不运行；BUG-028 仍 Open。

## 后续原生收口与用户停止记录

负责人授权两项原生执行后，DataGrid 实际桌面交互/最大化还原/标题栏关闭通过，ROW-00000/ROW-00020、滚动720px、17帧、rc0、零race/残留。第二项被用户按Escape停止Computer Use，停止后仅定向收口自有进程；原件标中断，不记PASS。

用户随后要求“不控制电脑继续”。仅给fixture增加 normal-auto/font-strict-auto：双真实帧和Lazy.Done为同步点，后台核对唯一标题+本进程PID后仅向自己的窗口发正常WM_CLOSE，不调用鼠标/键盘/截图，不新增产品API。两场景真实原生/race各4帧，rc0/1、回调清理/Observer顺序、严格ErrFontNotFound与activeAfter0均通过；自动场景不声明人工视觉或系统字体故障通过。新fixture的标签编译/build/vet已过，产品运行时和组件源码未改。

首版二进制因本机Go VCS探测误认上层C盘仓库而缺ref/标dirty，语义身份校验失败原件保留。按真实worktree限定构建进程Git环境后ref/clean/race属性正确，未改全局Git或SDK。最终源码/文档门禁及准确合入推送SHA见协调者交付，不用预期退出码替代完整业务验收。
