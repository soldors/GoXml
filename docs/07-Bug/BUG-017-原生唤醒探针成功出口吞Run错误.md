# BUG-017：原生唤醒探针成功出口吞Run错误

- 级别：P1，独立探针成功判定问题；不属于App生产实现变更。
- 状态：R1.1最小修复、作者真实红绿和独立原反例通过；原生窗口仍未执行。

## 反例与根因

原工具supervise在settle分支读取snapshot后直接end(0)。如果App.Run错误在该快照读取期间入队，select此前已选择settle，成功出口未再次消费runErr，就会finish0并输出exitCode0/nativeWindowVerified=true。

独立overlay只在既有snapshot入口插入私有阻塞观测点，用channel固定“settle进入快照→原始Run错误入队→放开快照”。红测试真实rc1，原因是被审工具实际选择0；不靠sleep猜调度，不运行GUI或候选exe。

## 最小修复和独立复核

作者在validateFacts成功后、end(0)前非阻塞读取已就绪runErr；有错误则走原end(1)，保留原reason。native只加nil默认的私有测试hook，测试使用作者真实main/native/protocol。schema、protocol、7个合成fixtures逐字节不变。

作者R1原码加新测试红rc1→修复后全包绿rc0，vet/build均0；原审查者再次用原受控反例、只追加测试overlay而不替换作者实现，rc0。85项身份核验及原R1/旧review/新作者目录前后保护通过，超时/输出失败/静默gate等机制无回归。

成功判断仅截至最终非阻塞检查时点，不等待未来Run返回；250ms静默不能证明没有任意OS重绘。优雅关闭、屏幕呈现、关闭后严格失败和独立原生wake计数仍未验收。不得用此修复关闭正式FONT验收。

关联：[BUG-013](BUG-013-Lazy提前请求可能丢失终态唤醒.md)、[夜间复审记录](../06-测试/2026-10-30-夜间准备与独立复审记录.md)。
