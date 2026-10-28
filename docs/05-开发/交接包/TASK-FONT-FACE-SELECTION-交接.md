# 显式字体选面：实现与采样收口

生产实现`97c0f7b645f1b2bb596df8382fd4693cc09e8d11`与探针快照`2ed565c8b277e54b75b061f3cb283f9a83bccda2`已完成代码复审、集成验证、真实窗口smoke和240固定样本双重复算，无阻断，进入主仓库本地安全快进。实际合入以主仓库HEAD及本机final-integration收口记录为准；不推送、不升级FONT ACCEPT/WBS或预算。

入口为`theme.LoadSelectedFaces`和`theme.NewLazyWithSelection`；后者在既有worker内读取、解析并构造最终主题，经pending/Take帧内交接。默认NewLazy/Provider不改变。同步取得的SelectedFaces可显式交给NewApp；不得先同步解析，再用LazyOptions.Temp冒充异步最终加载。

Path每次显式失败重试重新读取；Bytes构造时复制冻结、Ready后释放源快照；字体对象仍可持有表缓冲，RSS未验证。普通解析错误与recover的parser-panic按类型区分；Lazy.Report每次读取复制自有SelectionError及切片，默认路径不经过新增快照分支。上游go-text的panic未全局修复。

全仓测试/覆盖率、vet、Windows amd64/arm64 build已经执行，新facesel.go/lazy_facesel.go覆盖率86.2%/85.6%。独立协议探针28顶层/71子测试及vet/build通过。源文件自固定快照后不变，采样后文档收口不重复全仓。

真实窗口8次smoke通过，随后240/240固定样本有效。协调者从raw独立核验41,065项，ZCode另行复算一致。单面选择缩短最终字体Ready中位数约33%–35%；M1未呈现稳定改善，八格median均超过53ms，不能声称50ms冷启动达标。239条首Layout用临时主题，seq217首Layout已用最终Shaper且changes=0合法，保留未剔除。计时、逐格表和证据边界见[窗口采样记录](../../06-测试/显式字体选面窗口采样记录.md)。

本轮有限链条收口，不继续安排Agent或新批次。M1根因profiling、完整冷启动/呈现、RSS/race/跨平台为未验证事项，后续另行确定范围，不改变本轮合入的显式opt-in定位。

本机证据根`D:/Codes/Goxml-agent-rounds/2026-10-27/face-selection/`，collector/raw/exe未复制入Git。`window-sampling-r1/`保存采样与独立复核，`final-integration/coordinator/`保存主仓库既有文件SHA/状态的前后对照、真实命令和本地合入结果。保留主仓库20条既有改动；提交与快进每次临时空hooksPath并禁自动push。
