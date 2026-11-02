# R7 协调者回收与补修记录

## 1. 三线身份与接纳范围

基线 `4617f274b53870550443e54d4c83acf0ad58ba4b`。B作者最终 `94f017e`（handoff开头仍为ae701b9，增量仅测试注释gofmt）；C最终 `4c3826a`（相对已审83d588c只增加开发日志）；A候选 `e25828e`，没有可接纳的v1算法改动。

组合树从已发布基线迁入B/C，原SHA与组合SHA映射如下。只迁入CLI构建上下文和preferences，不迁入Input候选。

| 原提交 | 组合提交 | 内容 |
|---|---|---|
|79ec275|6c5ddfc|有效构建上下文及包目录错误|
|65c1595|1b12e41|B1/B2/B3回归|
|902b5cf|4f72a22|GOFLAGS测试环境隔离|
|ae701b9|e35801d|B开发日志|
|94f017e|937ef27|测试注释gofmt|
|a6bf3ed|d2fc89f|preferences材料与注册|
|a1f44b3|9fdd9a8|preferences真实事件回归|
|83d588c|7cf2444|教程及选择器|
|4c3826a|6a46c8b|C收口日志|

## 2. B独立复核发现与修复

P1：原来go.work只让单模块缓存保守miss；94f把缓存不可建模当成上下文不可测，在runPipeline前置硬拒绝。独立真实CLI证明合法go.work下旧4617生成rc0，新94f rc2，GOWORK=off对照rc0。作者“GOWORK未动”不成立。

最小修复：measureContextValues只负责可信测量，返回GOWORK实际值；cacheString单独拒绝单模块快照无法建模的workspace，管线对此设置dependencyUnsafe强制miss，继续按测得上下文选包与正常分析。真实测量失败/包选择失败仍fail-closed，没有引入跨模块缓存保证。

新增两项回归：合法workspace可测且包上下文可用、缓存键拒绝；连续两次generate实际生成且均不命中，坏输入保留旧gen/Manifest字节与gen mtime。旧实现行为红、补修绿。首轮补修测试夹具只含空go.mod，第二次分析生成物时因缺Gio依赖失败；这是协调者夹具问题，已复用仓库锁定依赖的独立模块助手修正，首次失败raw保留。

## 3. C独立复核与补强

最终4c产品/测试与已审83d相同。非作者补核69项及15case/30raw哈希，零不符，未声称重新跑了所有PS宿主矩阵。

两项测试证据不足已补强：G2缓存命中显式检查Manifest字节不变；G3使用旧mtime哨兵证明--no-cache确实重写同字节产物。移除两次固定sleep，避免时钟分辨率掩盖“跳过”和“重写”的区别。业务事件及三项VM变异判据未放宽。

作者commands.jsonl的28行中23–28共6行不是合法JSON；原件冻结保留，不伪补起止时间或重写成作者台账。新组合验证使用协调者真实argv/cwd/起止/rc与双通道raw，分别注明证据来源。

## 4. A候选继续阻断

独立同材料Router基线3/3通过，候选2项行为失败。同帧外部写NEW加用户输入X，候选quiet帧3–6显示XNEW、模型NEW、applied=NEW，Change回调delta0；加Return仍Submit=1，但载荷XNEW与模型NEW不一致。整帧didSync门控丢了真实Change，并未实现其宣称的外部覆盖本地。

这不只是4条旧测试的echo语义变化，不能先批准“外部写不派发”再把4条测试改绿。WorkBuddy交叉探针未在“同帧Return”中同时外部同步，所谓MaxLen追加用例也不是Append处理器；它的通过不覆盖这次反例。现有UpperText显示/模型分歧在候选中仍保留，不称完整解决。

A不迁入、不改正式Input契约/旧断言。本轮独立证据及决策摘要在仓外 `night-product-r7/coordinator/review/a-input/`，候选保持冻结。Background B已获批与Input批准无关。

## 5. 组合与发布边界

修复后需独立增量复核，再在B→C组合树验证CLI覆盖率、examples真实生成/离屏事件、P选择器all五示例、build/vet/docgate。正式主仓只接纳审核blob；重新快照93份用户WIP及1份协调者未提交R7计划，计划由本轮转为已接收/实际状态，不把它算作用户未提交内容。

所有Go重负载同宿主持公共锁finally释放；零原生窗口/采样/profile/trace/安装。示例入口只构建不执行。主仓/远端最终SHA由仓外发布回执绑定，后续实测追加到测试记录。

补修提交 `e5c181d` 获B/C独立复核通过（12/12、13/13）。B→C组合实测CLI覆盖率87.9%，examples全包、vet/build均rc0；P1–P5五例退出0/0/2/7/2，all五示例齐备，raw哈希独立核验零不符，锁与进程已收口。详见[组合验证](../../06-测试/2026-11-02-R7上下文与设置工程组合验证.md)。

文档归并 `41de20b`：更新计划实际状态、索引/CHANGELOG和Bug/踩坑/测试记录。补充ToolTags/ReleaseTags继承宿主的源码边界，未验证CPU特性标签与跨工具链版本；不以此泛称所有Go构建选项已完全一致。最终门禁与发布仍针对同一生产源码e5c181d及其文档后继。
