# R26-B WIDGET-OVERLAY-FOUNDATION 开发日志（2026-11-07）

基线 `1505fd455ff90e5b50b8120eb567dbcb88775184`，分支 `codex/r26-zcode-widgets`
（原冻结 `043fb16680955377259f13fffba190a3193c709f`）；返修分支
`codex/r26-r1-zcode`。主库全程只读；真实 Go 全部走公共锁并留痕。

## 本线提交（docgate 覆盖登记）

- `a376a8f` — B1：pkg/widgets 新包（overlay/contextmenu/modal + 状态单测）。
  共享遮罩基座（clip 内 event.Op 命中区、后声明者命中优先）、ContextMenu
  锚点定位/单次动作、Modal 遮罩阻断/焦点回交；候选行为单列 scope-matrix。
- `524f6ea` — B2：离屏 input.Router 真实交互回归（无原生窗口）。过程中修正
  三个 Gio v0.10.3 语义误判：FocusFilter 才授予 focusable、焦点持有还需
  输入区（visible）、Clicked 必须先于 Layout 消费。
- `043fb16` — B3：四边/四角定位矩阵 + 开/关布局 bench（ContextMenu 关
  56ns/开 21.6μs+59allocs，Modal 关 88ns/开 2.7μs+10allocs，含 Router 帧
  登记、无 GPU/绘制）；双可编译变异红（遮罩 event.Op 移除、钳制恒等化）
  已记录并恢复。
- `0f10718` — 返修 R1：闭合协调者 review-1 的 F-B1/F-B2/F-B3/F-B4
  （详见下节）；F-B5 由本日志 + 最终态 docgate 重跑闭合。

## 返修 R1（review-1 五项）

- F-B1（跨帧点击/真实 Release）：遮罩按压状态帧局部、Release 要求
  Buttons 含 Primary——两缺陷把真实分帧点击和 Windows Buttons=0 释放
  全部漏掉。修复：scrimState 跨帧保存 pressed/pid，Release 只校验按压
  归属与指针身份（对齐 gesture.Click），Cancel 清除，拖出后下次 Press 重置。
- F-B2（逐帧抢焦点）：escapeLayout 每帧 FocusCmd 抢走内容焦点。修复：
  focusState 只在打开转换请求一次；Escape 改走约定路径（无 Focus 约束的
  过滤器，keyFilterMatch 对 Focus==nil 不做焦点比对）——焦点在内容上
  Escape 仍可达，不为保 Escape 抢焦点；焦点回交 ReturnFocusTo 保持。
- F-B3（偏移泄漏）：Modal 居中 op.Offset 裸 Add 把变换泄漏给后续同级。
  修复：Push/Pop 作用域配对；新增前后兄弟两阶段命中回归。
- F-B4（高菜单越界）：viewportClamp 只移动锚点不限尺寸。修复：测量阶段
  高度封顶到视口 + PASS 模式滚轮区（声明在条目之后，hitTest 对 pass 区
  不阻断下行，条目点击不受影响），滚动每帧按几何重钳制、重开复位；
  无滚动条视觉，滚动方向沿用事件 Scroll.Y 约定（原生平台方向未验证，
  候选 C8）。
- F-B5（日志/门禁）：本日志补登 a376a8f/524f6ea/043fb16/0f10718；
  最终提交态重跑 internal/docgate。祖先 1505fd4 的登记属窗口事实，
  非本线遗漏；门禁本身未改。

## 限定结论

- 所有验证为离屏 io/input.Router 帧循环；原生窗口/DPI/IME/真实滚轮方向
  未验证，不声称。DataGrid 未开工。返修通过不等于候选行为已批准
  （scope-matrix C1–C10 待裁决）。
- 协调者五探针（probes/b_test.go）在修复后重放全绿（台账
  r1-coord-probes-green）；历史反例与原 raw 一字未改。
