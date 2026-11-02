# 2026-11-02 R6-B Input 同步回归与候选算法（ZCode）

任务书：`night-product-r6/zcode-task.md`（R6-B-INPUT）。基线 455da26。

## Ref

- readyRef：`codex/night-r6-zcode-20261102` @ `334f55e` —— 片1 八例现行行为回归，仅测试。
- candidateRef：`codex/night-r6-candidate-20261102` @ `1925d1d` —— 候选算法三提交（09b612f 字段 / 6fa330f 算法 / 1925d1d 探针重期望）。

## 片1 事实（现行行为实证，全部经真实 UpperText/AppendText 处理器全链）

新链路 rtProbeVM：探测自带 vm 子包，使 fixture 没有的标准化/非幂等处理器
真实走 Analyzer→Codegen→go test，不在探针里手抄 reapply。八例（≤6 帧 +
固定安静帧）：

1. 用户 Edit + UpperText → `editor=abc / model=ABC / applied=ABC / count=1`，
   3 安静帧不自愈（R5 观察的分歧正式入回归）。
2. 预设值 + UpperText → 首帧同步即分歧（SetText 的回显 ChangeEvent 触发
   处理器改写模型，reapply 后无人追赶显示），无需用户输入。
3. 恒等回写锚点 → 无分歧（证明分歧源于处理器改写模型，非探针伪影）。
4. 非幂等追加 → 首帧双重应用（model=AA/editor=A）；编辑后 model=AAAb/
   editor=Ab 永不收敛；有界（基线不无限追赶）。
5. 外部写入 → 基线向处理器通知程序化写入（count=2），契约事实。
6. MaxLen=5 + UpperText → 载荷为截断后文本，分歧同样持续。
7. Submit → Edit/Return 分帧各触发一次；载荷为标准化前编辑器原文。
8. 原双Input条目的首次测试用了没有IdentityText方法的fixture，GXML-EVENT-003实际是缺方法，并非禁止共享绑定；独立复核改为真实probeVM，双Input共享同字段可生成并同步，另有两字段隔离和单Input缺方法对照。

Gio 机理（源码级）：`editBuffer.Changed()` 消费即清，SetText 与用户
EditEvent 共用 Changed 状态；实际通知次数还依赖 Update 轮询顺序，不能由共用标志推定所有同帧事件合并。程序化写入可产出回显，这是
「程序化回显」的来源，也是候选算法的切入点。

## 片2 候选（candidateRef，未批准）

- 路径1 判据 `V != A || V != expect<n>`：显示追随模型；expect 镜像取
  SetText 后实际内容（截断感知）；稳定帧判据纯字符串比较，0 分配。
- `prog<n>` 回显抑制：程序化写入不进业务处理器（非幂等不再双重应用）；
  同帧混合击键因内容 ≠ applied 照常通知；Submit 永远送达。
- 保留不变式：路径2 reapply、SetText<Update<Layout、非受管槽位零变化、
  Analyzer 门禁、诊断号。只改 input.go + 测试 + codegen.go 字段声明段。

## 片3 对照与验证

- 候选下：8 例分歧全部转为一致（CONSISTENT* 观测行），回调次数仅程序化
  路径 1→0；用户击键路径不变。负对照 MERGED-FRAME-OK / SUBMIT-NOT-SWALLOWED。
- 契约差异 D1-D6 与诚实边界 B1-B3：交付目录
  `night-product-r6/zcode/candidate-contract.md`（非正式 ADR）。
- 收口：heavy.lock（token 9a786d84…，已验证释放）内
  `go test -count=1 -cover -skip '^TestButtonInRealWindowRuns$'
  ./internal/compiler/codegen/` → rc=0，coverage 88.3%；vet/gofmt 干净。

## 诚实声明

- 两次中期全量 codegen 运行未加 real-window skip（约束复核滞后）；
  最终收口已按约束排除，探针证据从未包含窗口测试。
- 前期探索命令未逐条实时留档；决定性验证为重放捕获（commands.jsonl）。

## 移交

外审、主仓接纳、推送、语义批准归协调者。候选未批准前不合入 readyRef。

## 独立复核补正

候选1925d1d不获无条件通过：MaxLen=1与非幂等AppendText叠加后，一次用户Edit之后的quiet帧4至8仍每帧触发业务回调（count从1到6），模型继续增长，实际已构造有界性反例。该候选维持隔离，不进入ready。两次历史未过滤全包且未持共享锁、前期未设空hooks/GOXML_AUTO_PUSH的过程偏差保留；无历史env/原始窗口日志，不能证明零窗口或无hook触发。ready分支仅测试/事实文档，不改变生成算法。
