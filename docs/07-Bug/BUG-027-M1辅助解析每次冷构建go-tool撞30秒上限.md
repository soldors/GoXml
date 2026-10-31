# BUG-027：M1辅助解析每次冷构建go tool撞30秒上限——R4夜批3/24停批根因定案

## 元信息

- 状态：Fixed（修复随 R5.1→R5.2 落地并经真实工具准备/解析 smoke 验证；旧批次台账不变）
- 发现日期：2026-10-30（R4 夜批停批）；根因定案：2026-10-31
- 影响范围：R3/R4 时代 M1 诊断宿主的辅助解析路径（`go tool pprof/trace`）；不影响诊断 exe 与样本数据本身
- 关联：[BUG-015](BUG-015-Go解析子进程缺缓存环境导致诊断停批.md)、[BUG-016](BUG-016-M1解析异常导致已启动样本漏记.md)、踩坑-019（当前在 integration 树，合并后可链接）、定案报告 `night-batch-2026-10-30/zcode/r52-next-analysis-r1/final-report.md`

## 现象

R4 夜批（2026-10-30）：24 计划/3 执行/2 有效/1 无效/21 未执行，rc5 首败停批。b00-u02 的诊断 child 退出 0、profile 保留，但辅助 `go tool pprof` 在 30 秒界限内未退出，样本记 invalid 后按规则停批。**矛盾点：该次解析的 stdout（864B）包含完整 top 报告（含结尾行 `Showing top 5 nodes out of 192`）——输出早已产出，进程仍被墙钟杀掉。**

## 根因（定案，五条证据线交叉）

1. **输出逐字节等价**：被杀样本的 parse.stdout 与 2026-10-31 冻结工具直调同输入的输出 SHA 相同（`f4ab2e6b…`）——解析本身从未失败。
2. **代码结构**：R3/R4 `parse_profile` argv 为 `go.exe tool pprof/trace`（两代源码同构），硬上限 30s+5s grace；且**每个样本目录独立 GOCACHE**（b00-u01/u02 各自的 `go-cache`）→ 每次解析都是冷构建。
3. **量化**：授权内 4 次离线实验——冷 cache 首调 28.515s（首字节输出在 25.297s，cache 0→1738 文件/146MB，子进程抓到 compile/asm/link.exe）；暖 cache 2.125s；`-symbolize=none` 2.219s 与 `-local` 2.063s 无差异（排除符号化）；`go tool -n pprof` 冷 cache 亦耗 23.291s（独立旁证）。
4. **刀锋效应**：u01 冷构建压线通过（rc0）、u02 越线被杀（exit1）——同一结构不同抖动。
5. **修复对照**：R5.1 冻结工具直调 0.122s/0.047s，构建成本整体消除，30s 上限余量约 200 倍。

**一句话因果链**：`go tool` 包装现场构建（每样本冷 cache 使其必现，~23–28.5s）→ 报告在 ~25–29s 产出 → 30s 墙钟杀进程 → timedOut/exit1 → stop-on-first-invalid 终止批次。

## 与 BUG-015 的关系（两个不同失败模式）

- BUG-015（第一停批，2026-10-29）：解析子进程**缺缓存环境**（GOCACHE/LocalAppData 未定义）→ rc1 无法定位 build cache。
- BUG-027（本条，第二停批，2026-10-30）：环境修复后 cache **存在但每样本独立且冷** → 每次冷构建撞 30s 墙。
- 两者共同指向踩坑-019 的结论（准备与解析应分阶段），但失败机制不同，不能互相归并。

## 处置与验证

R5.1 引入独立工具准备（冻结 pprof/trace bytes + 不可变回执）与直调解析；R5.2 修复三项消费缺口后经 86/86 独立复审；2026-10-31 真实工具准备（1 producer/3 顶层 Go，构建成本移入独立阶段）与两次解析 smoke（2/2，0.122s/0.047s）闭环。30 秒解析上限未放宽；旧批次 valid 台账不追改。

## 数据来源

- 根因定案与验证器（26 项）：`night-batch-2026-10-30/zcode/r52-next-analysis-r1/`
- 双样本 M1 CPU 归因：`night-batch-2026-10-30/zcode/m1-facts-consolidated-r1/`、`m1-second-sample-r1/`
- 离线实验 E1–E4：`night-batch-2026-10-30/workbuddy/parser-offline-r1/`
- 真实准备与 smoke：`night-batch-2026-10-30/workbuddy/tools-prepare-r52-r1/`、`parser-smoke-r52-r1/`
