# R8 Input 正式规则接纳

2026-11-02，用户在完整行为说明后选择“采用该方案，更新契约和回归后再验收”。
基线e72c478，移植候选277e6fd的事件先行与A=事件后Editor镜像两处改动，
同步Input契约、ADR-0011、语言规范和状态更新交叉引用；不改Schema/API/诊断码。

保留历史候选全包rc1/10旧断言红事实；批准后才更新旧期望。回归覆盖程序化静默、
归一化追随、不回写时模型恢复、同帧XOLD用户优先、分帧Submit归一化文本，
另持久化协调者四个独立Router探针。共享字段后渲染input1实证前控件下一帧收敛，
Upper+Return同帧实证payload=abc、帧末E/V=ABC。所有改动保留原测试数量和行为检查，
不以编译失败冒充判别红，不删除历史原始日志。

首轮窄测rc1是本轮自己的Disabled探针适配错误：把共用失焦Skip强化为Fatal后，
原Disabled专用删除Skip文本不再匹配，导致Disabled错误要求焦点。修复专用删除后
narrow-r2真实rc0；其余已通过场景仍全部保留。原失败raw不覆盖。

完整包/覆盖率/vet结果在外部交付coordinator/input-adoption-r1/full/result.json，
最终值由协调者汇总。原生窗口、物理焦点/IME、race和跨平台运行均未执行。

最终受影响codegen全包（显式排除真实窗口）rc0，ok  	github.com/goxml/goxml/internal/compiler/codegen	267.068s	coverage: 88.5% of statements；vet rc0，锁已按自有token释放。
全包之后仅修正失败文案和恒等字符串拼接（语义与断言不变），gofmt完成。
