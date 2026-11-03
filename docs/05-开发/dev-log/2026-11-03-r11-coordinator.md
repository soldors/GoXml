# R11 组合复核与集中补修

基线9dfce78。接收A c263af0、B dd3d3d9、C de814a0，作者冻结树/原证据保持只读。
组合承载8398f6c、3e90c40、5d7e8e6、facdda3、c64675a、cdd87b4、95e1750、515a8c5、6e790f2。

## 独立审核与授权集中补修

A：用户有效GOFLAGS/GOENV overlay原会被候选overlay覆盖；全部候选（含命中读盘）现显式加入，
合并用户映射且候选优先。Go真实overlay支持虚拟不存在目录，因此取消临时shadow包，按真实最终导入身份检查，
闭合新目录的跨候选import；保持moduleRoot cwd以维持相对overlay/modfile语义，逐包显式-o到自有临时目录。
启动和超时错误保留cause。原shadow错误分支残留为内部故障注入事实，不冒称常规CLI可传NUL路径。
独立原TestCheck四项+新增TestReviewCheck六项均通过；最终全包另行核验。

B：原假“两文件”测试只有一次Generate，补成两个SourcePath真实同包build。200轮补实际绑定槽显示值、
Then/Else独占重录与Locale变化。注入锚点五形态、两类可编译变异保持判别力。
录制计数和有限轮几何一致不能证明“无不可达Ops/内存累积”，本轮撤销该过强主张。
参考是同一生成代码的新View，能验证历史缓存失效问题，不是独立布局数学oracle。

C：拒绝用户输出目录后finally仍可能覆盖外部log-dir/report.json，真实纯Python反例成立。
日志目录改为独占创建，只有本次持有logDirOwned才落报告；报告排他创建，自有文件更新核inode身份，
不再使用可能覆盖其他文件的固定.tmp路径。父重解析/悬空报告链接拒绝，报告失败不覆盖原业务失败码。
健康判据要求11个PASS及末尾唯一CROSS-DONE集合；可编译提前返回旧守卫假绿的反例保留。
--check报告不再硬编码“A未冻结”：仅每个生成子进程实际成功且有检查成功标记才passed=true；
无标记保持None，失败False。只凭解析flag不构成检查通过。

## 检查边界

采用常规GOFLAGS=-mod=readonly、CGO0离线检查。go build不执行用户程序/测试初始化；Go自身依赖元数据行为
仍由调用方有效参数决定，不承诺用户显式-mod=mod时整个模块逐字不变。
Windows原生窗口、跨目标运行、性能M1、内存无泄漏均未验收。Input/For/缓存格式保持既有语义。
新脚本只服务现有crosspackage-preferences，不增加第六个Example。
集中补修提交ded68e0后，协调者发现专属测试runner的最终报告也使用固定.tmp并覆盖旧文件，
改为排他创建最终报告，追加自身报告/同名.tmp保留的纯Python回归通过；专属套件因此为25项。
最终组合测试、文档门、主仓精确blob合入和普通快进推送以协调者真实记录为准。

d4ca524后最终组合检查：CLI覆盖86.4%，Codegen88.7%，示例全包/vet/CLI构建均rc0。
一键脚本25项全过且0skip，最终原生--check工程6阶段及11语义case通过，check.passed=true。
Go源码检查后只更新文档/教程；完整证据及边界见R11组合验证记录。
