# 2026-11-06 R25-C：组件布局实验台工程模板（component-layout-lab）

> 父提交：`95173db`（`95173db73c9a0dc4d2d204a8aec4f808e79dead3`，R25 固定基线。
> 该提交本身的内容属字体线，本线只以它为基线，不代替字体线记录其变更内容）。
> 分支：`codex/r25-workbuddy`（独立工作树 `C:\Users\admin\.codex\worktrees\r25-workbuddy\Goxml`，
> 主库工作树只读，未合入、未推送）。
> 完整交付：`D:/Codes/Goxml-agent-rounds/2026-11-06/product-r25/workbuddy/closeout/`。

## 做了什么

两笔提交，按切片分开：

| 提交 | 内容 |
|---|---|
| `656aa2f` | **C1 同源工程**：`examples/gxml/testprojects/component-layout-lab/` 10 个文件（model 独立包、两个生成视图源、两套离屏语义测试共 12 例、两个 `doc.go` 占位包、窗口入口、工程 README） |
| `0c5308c` | **C2 真实验收 + 登记**：`examples/gxml/componentlayout_project_test.go`（工程级全流程）、`scripts/verify-projects.py` 新增工程白名单、`scripts/test-verify-projects.py` 新增「登记与材料一致」专属断言 |

工程只使用基线已支持的静态子集：`<Def>` / `<Slot>` / `<Grid>` / `Text` / `Input` / `Button`；
不引入 `ThemeRef`、不使用 `<Expanded>`、不用 `For`/`If`；颜色全为静态字面量。
未新增第六个内置 Example，`examples/gxml/testdata` 的五个 Example 与注册项未动，
CLI / compiler / runtime 生产代码未改。

## 实测出来的四条文法边界（写进了工程 README）

1. **`<Def>` 的 props 是生成期替换，不是运行时变量。**展开时 `{Badge}` 已被实参或默认值
   替换成静态文本，因此**不占绑定槽位**：`form` 产物只有 2 个槽位（两个 `LabCell` 的
   `Value="{SavedA|SavedB}"`），两个徽标都是 `material.Body1(th, "…")` 字面量。
   这直接决定了「默认值被覆盖」的运行时可观测形态只能是**几何**——所以 `LabCell` 把徽标与
   探针按钮放进同一个 `<Row Gap="0">`，实测两个实例的按钮左界 53dp → 117dp（差 64dp）。
2. **尺寸类属性只接受静态值。**`Padding="{Tone}"` 报 `GXML-ATTR-003`；只有**颜色**属性的
   绑定形式由 Expander 做 prop 替换，`Tone:color` + `Background="{Tone}"` 才可用。
3. **`<Def>` 不能跨文件导入。**`report.gxml` 要用 `LabPanel` 只能自带一份同名定义；
   两个文件里的同名 `Def` 各自展开、各自克隆（`form` 里 `Probe` 克隆 2 处，`report` 里 0 处）。
4. **`<Slot/>` 只能被一个子节点替换；不传子节点是合法的空插槽**，整个节点被移除。
   多子节点报 `GXML-DEF-009`；给没有 `<Slot/>` 的组件传子节点报 `GXML-DEF-007`。

另一条与产物结构有关的实现事实：**槽位按节点分，不按值合并**——`report` 的槽位 0 与 6
都绑 `SavedA`，产物是 `len(v.slots) == 7` 且两处各自 `bindText`，没有去重。

## 验收结果（全部实测，命令与原始输出在交付目录的 `commands.jsonl` 与 `raw/cmdlogs/`）

| 项 | 命令要点 | 结果 |
|---|---|---|
| 工程级全流程 | `go test -run '^(TestComponentLayoutProject\|TestLayoutLabModelCreationsGuard)$' ./examples/gxml/` | PASS，22.4s；5 类负例逐条命中实测诊断码 `GXML-DEF-007 / DEF-009 / BIND-001 / ATTR-003 / DEF-004` 且磁盘零变更；2 条**可编译**变异（M1 参数串实例、M2 Slot 错接）都真变红；10 个模板文件指纹未变 |
| 标准入口 | `python scripts/verify-projects.py component-layout-lab --out-dir … --check` | 5 阶段全 `rc=0`；`generate:view` 的 `checkTargets=2`；离屏 12 例 pass=12 / skip=0 / completed=12；锁按自有 token 释放 |
| 红绿对照 | 同一份材料，真实 `go test` 输出喂给 `test_verdict` | 绿例成立；零用例（`go-rc=0`）、`t.Skip`（`go-rc=0`）、缺一个 `LAB-DONE`（`go-rc=0`）三种都被拒绝——证明放行不是靠退出码 |
| 登记一致性 | `python scripts/test-verify-projects.py --case component-layout-lab-registration` | PASS；漏登一例 / 多登一例 / 完成标记抄成 `CROSS-DONE` 三种漂移都能翻红 |
| 既有工程回归 | `go test -run '^(TestCrossPackageProject\|TestTaskboardProject)$' ./examples/gxml/` | PASS，34.7s（preferences 与 taskboard 的复制/生成/构建/离屏链路未受影响） |

## 合同空白（交协调者裁决，本线不改规范正文）

| 编号 | 空白 | 建议 |
|---|---|---|
| G1 | props 默认值被覆盖后没有文档化的可观测量（静态文本不入槽位） | 在 `GXML语言规范.md` `Def` 段写明「props 为生成期替换，替换结果按静态值参与布局」 |
| G2 | 空插槽是否合法未成文 | 写明 `<Slot/>` 允许零子节点、节点被移除；与「多子节点 → `GXML-DEF-009`」是同一维度两端 |
| G3 | 「一节点一槽位」未成文，易被理解成「同值合并」 | 在 `代码生成输出契约.md` 绑定一节写明槽位数 = 绑定节点数、按源码顺序编号 |
| G4 | 尺寸属性与颜色属性的绑定能力不同未成文 | 属性表加「是否接受绑定」列，或统一说明「仅颜色属性接受绑定」 |

另有命名/文案不一致（仅登记，不是语义缺陷）：`GXML-DEF-009` 的符号名是
`expander.CodeSlotNotAllowed`，实测文案讲的是「被 2 个子节点替换」。

## 未证边界

- **原生窗口、真实显示器 DPI、输入法**未验证：自动流程只**构建** `cmd/lab`，从不执行；
  本文的 DPI 结论是离屏 `unit.Metric` 缩放（`PxPerDp=2`，内容高度 542dp → 1075px ≈ 2×），
  不是真实缩放。
- **跨视图端到端只到「处理器写入」这一跳**：跨包读另一个视图的未导出槽位做不到，所以
  「点击 → 处理器 → 模型」在 `form` 包里用真实 `input.Router` 事件证，「模型 → report 槽位」
  在 `report` 包里用同一个公开方法（`ApplyA`/`SlotA`，即被绑定的处理器）证。
- **字体度量未固定**：探针左界差只断言「≥ 一个按钮宽且落在合理区间」，不断言具体像素。
- **`Grid` 只覆盖 `72 * 96` 与 `120 *` 两种列规则**以及一维宽度变化；多列弹性、`Gap` 为 0/负值、
  极小视口压缩未覆盖。
- **`Slot` 只覆盖「一个子节点」与「零个子节点」**；多子节点只在负例里验证会被拒绝，不验证渲染。
- `routed-gallery` 的验收脚本未跑（本线未改它的登记项与验收工具），只回归了 preferences 与 taskboard。
