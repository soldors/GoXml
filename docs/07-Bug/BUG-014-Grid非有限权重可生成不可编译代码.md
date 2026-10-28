# BUG-014：Grid非有限权重可生成不可编译代码

- 状态：Parser/IR放行问题已运行复现并修复，受影响包回归通过；已随d2feec7合入及推送；P2。
- 基线：35dc6a1；Parser/IR问题早于Grid生成实现，65a5202使该输入进入生成路径。
- 影响：非法列权重输入；正常有限权重不因本发现被否定。

## 输入与源码链

`Columns="Inf*"`或`+Inf*`经internal/compiler/parser/layout.go:197附近ParseFloat与非正数检查，正无穷可能被放行；contracts/ir_schema.go:470附近有效性检查未排除此值。Codegen格式化后可能输出layout.Flexed(+Inf,...)，其中Inf是未定义标识符。NaN可能延后到Codegen才拒绝，诊断层级也需核查。

## 修复与验证计划

先建立Inf、+Inf、NaN的负向Parser/IR测试，以及正常有限权重生成物的真实Go编译验证；拒绝阶段与错误码按现有契约决定。直接构造IR必须有防守；巨大有限值的float32溢出属于邻接风险，未验证前不新增任意上限。

关闭条件：旧行为反例、最小修复、正常输入回归、生成编译证据、受影响包测试/vet与独立复核。当前没有运行证据，不标已确认或已修复。

来源：[代码审查记录](../05-开发/2026-10-29-代码审查记录.md)。

## 10月29日续接实测

新增回归在35dc6a1实现上真实退出1：IR接受正无穷，Parser接受Inf*/+Inf*/NaN*。随后在Parser和GridColumn.IsValid增加非有限判断；没有新增公开字段或任意有限权重上限，修正注释为有限且大于0，0.5*保持合法。

contracts、parser、codegen三包完整测试通过，codegen耗时144.088秒，三包vet通过。原始红绿台账在本机night-resume/coordinator/commands.jsonl。不可编译生成物的原始失败未单独执行，修复关闭依据是前端拒绝和IR守卫；合法生成编译另有已有测试。真实窗口与性能不属于本修复证明范围。
