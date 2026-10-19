# Goxml 多 AI 协同开发宪章

## 1. 启动接手七步法（任何 AI Agent 启动必读）
1. 查阅 docs/00-文档索引.md 与 docs/03-架构/架构说明.md，明确五大架构红线（Windows 侧 Zero-CGO、编译期零反射 Diff、无运行时 XML/CSS 解析、异步 OS 弹窗、帧热路径零分配）。Linux/macOS 按平台矩阵使用 Gio 所需的 CGO，不把全平台 Zero-CGO 当作现行约束。
2. 读取目标任务卡 docs/04-WBS/任务卡/TASK-xxx.md 第 1 节“背景与上下文”及对应交接包。
3. 读取 docs/08-踩坑/ 关联踩坑点。
4. 终端执行 make setup && make test，确保基线代码 100% 绿色通过。
5. 依照任务卡中的 Subtask 清单按序实现，每条必须跑通绑定的单行验收命令（退出码为 0）。
6. 完成后同步更新任务卡状态、开发日志与对应踩坑记录。
7. 编写交接包，确保后续 AI 可在 30 秒内恢复上下文。

## 2. 契约先行原则
任何 AI 在修改编译器与运行时交互结构时，必须先修改 contracts/ 并跑通 make contract-lint，严禁私自定义平行数据结构。
