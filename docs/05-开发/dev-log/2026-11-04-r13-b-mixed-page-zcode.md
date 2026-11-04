# 2026-11-04 R13-B MIXED-PAGE-CACHE-CORRECTNESS（ZCode）dev-log

基线 `ed76cbbb353511cc3de94c20b5f6726104ac9842`；树 `C:/Users/admin/.codex/worktrees/product-r13-zcode/Goxml`，
分支 `codex/product-r13-zcode-20261104`。
改动范围：internal/compiler/codegen（仅新增测试）+ 本线 dev-log。**未改任何生产代码**。
未改 Input 规则、For 体缓存范围、诊断、公开契约、CLI；生成字节零变化（不涉及 generatorOutputRevision 提升）。

## 结论

混合页面（静态岛 × 绑定 Text × If 分支 × For 行事件同页）在基线 **六场景全绿**：
局部缓存回放与动态更新互不干扰，R10 局部静态缓存与 For 行状态池的既有语义
在组合形态下正确。按任务书「基线正确则交判别回归与证据」执行，
唯一提交 `1d8ab36` 是判别回归（582 行测试），无生产改动。

## 判别矩阵（mixed_page_cache_matrix_test.go，真实链路）

页面形态：Row[ {Title} + 岛0 ] / {Note} / 岛1 / Button / If{Then[{Detail}+岛2] Else[岛3]} /
For Key=row.ID 行按钮。4 个静态槽位计数经 `injectProbeRecs` 外部副本注入
（R11 生产分离），`assertProbeHealth` 拒内部 SKIP/缺 SCEN-DONE。

| 场景 | 判别点 | 基线结果 |
|---|---|---|
| mix-fixed-text (M2) | 固定约束下只改绑定文本 → 岛1 不重录、几何逐帧一致 | 绿 |
| mix-sibling-width (M1) | 水平 Row 中前序 {Title} 变宽 → 岛0 重录（recs 1→2）；恢复 → 再重录（按值比较 2→3）；稳定帧不再重录；同帧参考 View 几何一致 | 绿 |
| mix-if-reuse (M3) | If 隐藏→再显示键未变 → Then 岛复用（recs2 恒 1）；分支内动态文字变化不牵连 | 绿 |
| mix-for-events (M4) | For 重排/删除后点击同一位置命中当前行 ID（B→C→A→B）；行高与行起点由真实布局总高差推导，不靠猜 | 绿 |
| mix-context-items (M5) | 主题指针 / Locale / DPI 逐项变化 → 逐项重录（recs 1→2→3→4→5），未变项不触发 | 绿 |
| mix-two-instances (M6) | 双实例：B 换 Locale 只重录 B；B 动态文字变化两实例岛都不动；同帧参考实例几何一致 | 绿 |

手段：`rtMixedProbe`（rtProbeVMMutated 同链路 + 带 `gioui.org/io/system` 导入的
探针头，M5 需要构造 `system.Locale` 值逐项变化）。

## 事实发现（不改判据，写入测试注释）

1. **Gio Flex 按剩余空间传导约束**（`flex.go:118`：刚性子节点收到
   `constraints(0, remaining, …)`，remaining 随前序兄弟递减）。因此
   「前序兄弟文字换行增高」会改变后续静态岛**实际收到**的约束——此时重录是
   契约正确行为（§16：键=实际收到的 Constraints/Metric/Theme/Locale）。
   M2/M6 场景以 600px 宽画布保证「文字不换行 → 兄弟高度不变 → 约束确实固定」
   的前提；M1 场景正是利用该语义构造「约束真实变化」的重录判据。
2. **Exact 画布会钳制总高**：For 行高差分推导必须用不封顶高度
   （`Constraints{Max: image.Point{X: 300, Y: 100000}}`），否则总高差恒 0。
3. 等宽文字前提依赖字体度量（CJK 回退字体进距未必均匀）——放弃「等宽文本
   不重录」的脆弱断言，等价的「内容变、约束不变 → 不重录」由 M2 在竖直流
   上稳定证明。

## 两条可编译变异红（独立复现后还原；还原后 `git diff` 为空，字节与基线一致）

| 变异 | 手段 | 红 | 特异性 |
|---|---|---|---|
| 失效判据 MUT-A | 岛键字面量去掉 `constraints: gtx.Constraints` | mix-sibling-width 红：前序兄弟变宽后未重录（recs0=1, want 2） | mix-context-items 仍绿——红只落在约束判据 |
| 事件归属 MUT-B | pollCode 实参 `s.args` 中 `rowItem` 首次出现替换为 `rowItems[0]` | mix-for-events 红：点第2行 LastTapped="A", want B（键与点击状态照常，纯归属破坏） | — |

变异首版教训：把 pollCode 的 `rowItem := rowItem` 遮蔽改成 `rowItems[0]` 会让
range 变量「declared and not used」——**编译失败不算行为红**；改为钉实参。

## 边界与未测

- For 体内静态子树缓存：延续 R10 边界（§6.2 既有路径），本轮不新增。
- Windows race / 原生 IME / 物理焦点：延续未测。
- 像素级比对：按任务书用同帧新建参考 View 的几何对照（Dimensions 相等），
  不引入截图/单像素容差。

## 门禁与交付

- codegen 全包 `-skip '^TestButtonInRealWindowRuns$' -cover`：见 handoff
  （锁内、record-command 台账）。
- 交付：handoff.md / slices.json / commands.jsonl / resume.md / raw/ 于
  `D:/Codes/Goxml-agent-rounds/2026-11-04/product-r13/zcode/`。
