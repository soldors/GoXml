# R24-A 返修独立复核与最终补正

主基线bd874d7；原A43caac7，返修17a8e6a（a74b33b+c5776f8谱系）；组合4f605082b38e81a5caac4058ca1b1cd00a9ecd6b。

F-A1~F-A3独立同fixture验证：健康rc0/accepted2/rejected0/groups2，forged duration999入口rc1/rejected2，非交替/错mode/错base均rc1，合法invalid(rc5)保留且入口rc0。未改collector/summarizer，不设正式阈值，不把tool rc0当验收成功。

F-A4作者持久测试仍有“观察坏行为后整套PASS”的反向断言，且合法零fixture未启用。协调者补同一外部可执行健康守卫：fatal和unique两组均健康0→变异实际1(CONTRACT-FAIL)→恢复0；重复ID变异被真实summary拒收3条再触发健康守卫失败，不是语法/构建错误。补真实合法零及统计中位数恰0、OrderFile、奇数/平衡但不交替、缺记录、真实collector未知SHA前置拒绝；35 checks PASS，原test-comparison52 checks PASS。

协调者补测两次首败保留：未知SHA用例误传scripts/font而非repo根，先被缺probe拒绝；改用真实根后到达baseline mismatch。PS5健康守卫对预期fatal stderr因Stop提前抛NativeCommandError，局部Continue只包原生子进程调用，随后按真实rc恢复Stop并断言。不更改系统/策略/产品错误处理。

这次只改字体编排脚本/测试和方案/日志。Go/module字节不变，B/C已发布内容不重测、不回滚；最终跑docgate、diagdoc、diff-check与台账校验。主库9项WIP不重叠、原字节/状态保持。实际合并/推送以本机receipt和远端回查SHA为准。无窗口、无性能采样、无安装；整体字体正式性能验收、ThemeRef、Router预算仍未批准或未完成。
