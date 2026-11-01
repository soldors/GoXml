# R4 跨包输出独立复审及补齐

审查基线为 DeepSeek 冻结 c841569（含 2e30b1b/bcd416c，作者树只读）。静态阅读与独立 CLI→Go build 对照确认两个未覆盖边界：模块根 ViewModel 移入子包时生成裸 VM（generate=0/build=1）；源包名 material 与 Gio material 冲突时 importSet 改写 Gio 导入别名而已发射代码仍引用 material（generate=0/build=1）。普通 sourcepkg 对照 build=0。

最小补齐：内部输出上下文保留模块根“.”与未知空串的区别，根包导入使用模块路径本身；VM 别名在 body 导入收集完毕后避让已有限定符，并保持重复调用幂等。导入限定符使用 go/token.IsIdentifier，支持合法 Unicode 包名并拒绝关键字和空白标识符。目录首次创建的回退由 errors.Is(errPackageUninitialized/os.ErrNotExist) 判定，替代错误文案子串；无新诊断号或公开构造函数变化。

新增三个判别回归文件，覆盖根包、五类导入碰撞、Unicode、非法别名、结构化回退与根上下文。修后全新 CLI 三组真实生成和独立模块构建全0；CLI 全包覆盖89.7%、pipeline87.5%；codegen 只跑新增边界和原五项 VM 回归，三包vet0。没有原生窗口、采样、安装或未过滤全仓测试。所有 Go/CLI 进程在共享heavy.lock中串行执行，均实测release=true。

完整红绿原始证据在 D:/Codes/Goxml-agent-rounds/2026-11-02/product-batch-r4/coordinator/reviews/a。首次独立 E2E 驱动因Python默认GBK读取含中文go.mod失败，日志保留，不作产品红证据；后续仅更正UTF-8，使用新目录重跑。一次gofmt遗漏workdir在旧night-font-integration树运行，经4文件diff和历史mtime核实未改任何字节；随后在正确review树执行。作者原件和用户主仓未修改。

此修复由非作者复审提出后在隔离树实现，组合前还需协调者独立阅读该增量；不把本修复自测写成非作者审核通过。
