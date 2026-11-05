# 2026-11-06 R23-R1 最终独立复核与归并

记录主基线 `fa0f9c5`，B返修 `dd608181a9639d7b4c06e6803b3e133048783ed2`，
C返修 `18ac2a532a2a84f27ffc2dc4ac33c3ae59a78b96`，协调者补正 `fbbc28b`。
组合提交记录：

```text
fbbc28b docs: validate font sampling aggregation and qualify test inventory evidence
cc79078 merge: accept reviewed evidence qualification and archived WIP batches
33d20fe merge: accept repaired deterministic font timing guards
```

## 复核结论

A的完全移除余量唤醒反例已独立红→绿，ready文档合入fa0f9c5；预算实现仍不进主库。
B已独立关闭固定1小时、测量置零、报告被覆盖三类可编译反例，健康/恢复均PASS。
零/非零时钟分别精确断言0和7ms，相邻构造一次用例为5ms，保留调用次数/Attempts/来源判据。
Lazy增加私有函数指针时钟接缝，在首次Request构造内使用；默认nil回落time.Now，
未改公开API或计时/失败合同。生产lazy.go源码和实例字段确有变化，不能称“生产字节不变”；
未新增公开配置，未将构造期接缝外推成GUI性能/内存验收结论。
原font_lazy_strict_test.go未改，整体字体验收、阈值、跨平台、第二成功family仍未闭环。

C的15个包真实-list清单逐包逐名与原始记录一致：1518个Test、另3个Benchmark；
空的Parser旧选择器确认无名，不用他包/全仓正则凑数。13项A6/B6/C1互斥，
历史执行方独立性未证与本次可复跑raw分开。旧12/16/13/8只作历史队列，不报完成百分比。
协调者修正“包级ok一律零执行”为“未列具体集合”，仅-list空名或-v明确空跑记零。

## 方案模板补正与验证

采集方案改用公共锁和逐样本交替。协调者又修正：汇总器不递归找样本，需新派生samples.jsonl；
每次Runs1原id可能均为0001，在派生副本加目录命名空间并附原路径/ID/SHA，原文件不动。
AllowMixed只按来源分组；收口/证据/身份/锁故障停止后续采样，Strict无效样本保留。
真实解析/统计合成回归及6个样本/2组的mock模板均PASS；模拟收口码7只调用一次即停。
以上是合成fixture，不是原生窗口/性能采样，本轮没有启动原生程序或安装工具。

## 门禁范围

ok  	github.com/goxml/goxml/pkg/runtime/theme	3.137s	coverage: 86.7% of statements
ok  	github.com/goxml/goxml/pkg/runtime/app	3.244s	coverage: 95.2% of statements

theme整包race-count3、app字体/关闭/通知/销毁无窗口族race-count3均rc0，
原生RealWindow与实际RunWindow启动用例明确排除；全部Go/module字节与已测B返修一致。
最终组合另跑docgate/diagdoc/vet/构建/gofmt；未重复产品全仓go test。
原始证据在本机product-r23/closeout-r1/coordinator/repair-review/，含变异、逐包清单、
解析fixture和模板mock；旧失败/作者冻结记录不回写。

## 主库与WIP

开工66条原字节和空索引保持。只接纳C清单的46个已审输入：43份归档最终字节不变，
3条B4主文档有声明的追加；2份治理稿按新路径历史归档，目录属性保证CRLF原件不被归一化。
主库实际归并时原件移动到独占holding并再次保留冻结副本，不接纳20条明确保留项。
目标剩20条WIP原字节/状态不变；精确结果以本机main-accepted.json为准，非清空工作区。
远端只推审定提交，普通fast-forward，不force；是否推送成功必须有真实rc与ls-remote回查，
发布完成前不改写为已发布，不将剩余WIP带入远端。

## 最终文本卫生补正

组合 diff-check 发现12张任务卡本轮追加段尾部有多余空行，协调者仅移除多余EOF空行。
不改变历史原文、归档输入或资格结论；保留首次rc2记录，修正后重新绑定提交运行门禁。
