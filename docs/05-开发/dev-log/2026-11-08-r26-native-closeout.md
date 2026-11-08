# R26 原生验收与进程清理收口

日期：2026-11-08。公共接线 `e9589c7e8afe02bc05ebb46877e1b13dbaeb325d`、
前次文档/测试 `50de78c33d17836ca61680c576da148573b09efe`、原生/并发修复
`97dbc9a2fd3d7035b9dd7734904f83643d3f22bd`、退出凭据修复
`620cb34ced78a78f28367fb5385caab654c5de8c`。核心组合 `b6137bd` 与各作者原冻结树保持不动。

负责人已批准基础语义、原生执行，并在物理 Escape 中断后明确续作。原生结果来自
Computer Use 实际观察，不冒称负责人亲眼签字。公共 Preview 显示静态文字/图片/Def/Slot/
Expanded、最大化后重绘、标题栏关闭均有实跑；旧宿主关闭后退出2的 stderr 与失败记录保留。
专用单窗 Gio 宿主解决本路径退出，启动器转发 stderr；通用 App.Run 的退出策略另登
[BUG-028](../../07-Bug/BUG-028-Windows-App-Run独立进程关窗退出.md)，未改共享 Runtime API。

Widgets 原生发现滚轮方向与关闭帧残留命中区两处问题。原材料六个子例先红；滚轮按正
Scroll.Y 前进，关闭时不提交弹层记录的 Ops，关闭后的首次背景点击有效。原生复验下滚到
第24项、单次动作、Modal Escape、遮罩点击不关、ASCII输入与焦点回交；IME、真实DPI比例未验。

Watch 原生实跑1→2代不同PID、坏GXML第3代不发布且旧窗口可交互、恢复第4代、定向
Ctrl+Break退出与自有树清理。另修NotifyChange两次扫描间吞掉用户保存：采用提供路径与
实际新Diff的并集，所有权仍按Manifest和真实保护头核对。错误测试夹具的旧头和首败保留，
只有真实头正控制成立后的红绿算修复证据。

工具树先挂起创建、分配自有Job再恢复，避免根进程先生成子进程再归属的可行时序。
首个零等待句柄反例属于过严前提，原版有界等待可过，不拿它当泄漏证明；另以文件信号固定
先子进程、后Job归属、再根退出的外部副本探针，旧顺序红、新顺序绿。生产无退化开关。

最后一次race验收又发现Job的ActiveProcesses归零先于一个已持有后代句柄变为signaled。
620cb34在taskkill前保留该Job的进程句柄，统一截止时间内终止Job、等待计数和逐句柄退出；
获取凭据、终止、超时失败不伪装成功。同一协调者取消探针与受影响三包race转绿。
Win32依据：[TerminateProcess](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-terminateprocess)
及[TerminateJobObject](https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/nf-jobapi2-terminatejobobject)。
不把采样计数本身当作每个进程完成退出的凭据；OS故障仍显式报告。

性能按正式预算§3.2的Layout范围判定：组件Layout稳态0B/0alloc；含Gio Router.Frame的
菜单/Modal分别6/2次分配照实报告。外部Gio泛型稳定排序实验未接纳，依赖、模块缓存不动；
BUG-002、整应用性能、M1/RSS、跨平台运行不随本轮升级为通过。

最终覆盖率、门禁、准确发布五态见[接线与验收](../2026-11-08-R26-Windows接线与验收.md)。
失败尝试、旧快照、两次错误探针前提及后续修复均留在协调者交付目录，不以台账完整性
PASS冒充业务PASS。无安装、无真实性能采样；语言1/缓存8/gen-4不变。

## 发布准备记录

文档同步提交 `1bae78cac47d56aab44a2d14919b8b596632c400` 汇总原生、覆盖、性能口径与
BUG-028；`a0c8f9bce170a6de4228a2e72424dcc8656f1cb8` 将根入口标记更新为批准的Windows
基础范围。发布前docgate因前者尚未登记而拒绝，原始失败保留；本段补登记两笔，不改门禁。
最终产品源码仍为620cb34，随后只改文档，无需重跑未变的产品全套。
