# 组件布局实验台工程模板

一个可以**真的复制出去、真的生成、真的编译、真的跑测试**的最小工程，专门把
`<Def>` / `<Slot>` / `<Grid>` 与绑定、事件放在一起用：状态在一个独立的 `model` 包，
两个视图由 Goxml 生成到**另外两个包**，窗口入口用现有 Go 代码把两个视图上下组合，
并且**共享同一个模型指针**。

它示范的是「组件复用 + 网格布局 + 跨包绑定」这条组合，不是新的 GXML 语法：

```
                     model.NewLab()   ← 只调用一次
                            │  *model.Lab
              ┌─────────────┴─────────────┐
              ▼                           ▼
   form.NewGoxmlFormView(m)      report.NewGoxmlReportView(m)
   （package form，可编辑）        （package report，只读）
              └──────── 同一个指针 ────────┘
```

## 目录图

```
component-layout-lab/
├── README.md                       本文件（不参与构建）
├── go.mod.txt                      → go.mod            模块定义（占位符 __MODULE__ / __GOXML_DIR__）
├── model/
│   └── lab.go.txt                  → model/lab.go                  唯一手写的业务类型
├── view/
│   ├── form/
│   │   ├── doc.go.txt              → view/form/doc.go              先有包，才能生成（见下）
│   │   ├── form.gxml               → view/form/form_gen.go         表单视图源（Def×2 + Grid + Slot）
│   │   └── sem_test.go.txt         → view/form/sem_test.go         表单视图离屏语义测试（8 例）
│   └── report/
│       ├── doc.go.txt              → view/report/doc.go
│       ├── report.gxml             → view/report/report_gen.go     只读汇总视图源（自带同名 Def）
│       └── sem_test.go.txt         → view/report/sem_test.go       汇总视图离屏语义测试（4 例）
└── cmd/
    └── lab/
        └── main.go.txt             → cmd/lab/main.go               窗口入口（只构建，不运行）
```

`*_gen.go` 不在模板里——它们是**生成结果**，由你在自己的目录里生成。

为什么是 `*.go.txt` / `go.mod.txt`：如果它们叫 `.go` / `go.mod`，这些目录就会成为
Goxml 仓库根模块里的普通包，跟着一起被构建、被 vet；改名后它们只是**模板文本**。
本仓库的工程级测试（`examples/gxml/componentlayout_project_test.go`）读的就是这份材料，
测试与教程不各写一份。

## 一条命令：复制 + 填占位符 + 生成 + 构建 + 离屏测试

```bash
python scripts/verify-projects.py component-layout-lab \
    --out-dir "/d/work/我的布局实验台" \
    --lock-path "D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock"
```

- 目标目录**必须不存在**：本工具只写「本轮独占创建」的新目录。已有目录（含空目录、
  别人的标记文件、符号链接）一律拒绝，退出码 3，**一个字节都不动**。
- 公共锁必须显式传入（`--lock-path`）：他人持锁时直接退 8，**连输出目录都不创建、
  一个 Go 进程都不启动**。
- 阶段顺序与 preferences / taskboard / gallery 三个工程**同一套**：`copy` →
  `build-cli` → `generate:<dir>` → `build-entry` → `test`。本工程不另造入口脚本。

| 阶段 | 干什么 | 产物/证据落点 |
|---|---|---|
| `copy` | 复制模板、`*.go.txt`→`*.go`、替换占位符、补 `go.sum` | 工程目录；模板源码不改 |
| `build-cli` | `go build ./cmd/goxml` | `<工程>/.verify-projects/goxml.exe` |
| `generate:<dir>` | 原生 CLI **一次目录生成**覆盖本批两个 View | `view/form/form_gen.go`、`view/report/report_gen.go` |
| `build-entry` | **只构建**窗口入口 | `<工程>/.verify-projects/lab.exe` |
| `test` | 离屏语义测试（`-run ^TestLab`，要求 12 个唯一 `LAB-DONE`） | `<工程>/.verify-projects/test/` |

每个阶段的实际 argv / cwd / 退出码 / 收口结论 / stdout·stderr 原始字节与 sha256
都在 `<工程>/.verify-projects/<阶段>/`，汇总在 `<工程>/.verify-projects/report.json`。

退出码：`0` 全过 · `2` 用法错误 · `3` 拒绝（目录/模板/模板被改）· `4` 业务失败 ·
`5` 超时 · `6` 进程树收口未证实 · `7` 记录不完整 · `8` 锁忙 · `9` 锁未按自有 token 释放。

## 三个包各管什么

| 包 | 来源 | 职责 |
|---|---|---|
| `model` | 手写（`model/lab.go.txt`） | 唯一状态所有者：两份草稿 `DraftA`/`DraftB`、已保存值 `SavedA`/`SavedB`、计数 `CountA`/`CountB`/`SlotAClicks`/`Probes`、回车计数与载荷、派生串 `CountAText`/`CountBText`/`SlotText`/`SummaryText` |
| `view/form` | 生成（`form.gxml`） | `LabCell`（typed props + 默认值，内部徽标 + 探针按钮同一行）、`LabPanel`（color prop + 默认值 + 默认插槽）、`Grid Columns="72 * 96"`（两组 文本/输入框/按钮）、面板甲传入插槽子节点、面板乙空插槽、两个 `LabCell` 实参不同 |
| `view/report` | 生成（`report.gxml`） | 只读：`Grid Columns="120 *"` 展示两份已保存值、4 个 `LabStat` 展示派生串、`LabPanel` 里再渲染一次 `SavedA`；没有任何事件与输入框 |

两个 `.gxml` 都写 `ViewModel="model.Lab"`（**相对模块根**的包目录 + 类型名），所以产物
`package` 子句是各自目录的包名（`form` / `report`），并显式导入 `.../model`。
入口 `cmd/lab/main.go` 调用 `model.NewLab()` **一次**，把同一个指针交给两个构造函数。

`main.go` 用 `layout.Flex` 把两个 View 上下组合：**Goxml 目前没有路由**
（`pkg/runtime/router` 仍是规划项），所以这里不做页面切换。上下排列的好处是
「共享同一个模型」肉眼可见：上面点「应用甲」，下面下一帧就是新的已保存值。

## 现行文法边界（都是本工程实测撞出来的，不是推测）

写这一页时踩到的四条边界，全部按**现行实现**记录，模板本身不做绕过：

1. **`<Def>` 的 props 是生成期替换，不是运行时变量。**`Props="Label:string; Value:string;
   Badge:string=默认"` 里的 `{Badge}` 在展开时就被实参或默认值替换成**静态文本**，
   于是它**不占文本缓存槽位**。实测：`form.gxml` 两个 `LabCell` 里的徽标、`Label`、
   面板标题全是静态串，产物里只有 `SavedA` / `SavedB` 两个绑定 → `if len(v.slots) != 2`。
   这条直接决定了「默认值被覆盖」在运行时的可观测形态：**只能看几何**，不能看槽位文本。
   所以 `LabCell` 把徽标与探针按钮放进同一个 `<Row Gap="0">`，徽标宽度推动按钮左边界。
2. **尺寸类属性只接受静态值。**给 `LabPanel` 加 `Inset:insets` 再写
   `<Column Padding="{Inset}">` 会被拒：
   `GXML-ATTR-003: ... must be a static value, not a binding`（退出码 2）。
   只有**颜色**属性的绑定形式由 Expander 做 prop 替换（`parseColorStatic`），
   所以本页 `Tone:color` + `Background="{Tone}"` 可用、`Padding="{Inset}"` 不可用。
3. **`<Def>` 不能跨文件导入。**`report.gxml` 需要用 `LabPanel` 时只能**自带一份**同名定义。
   两个文件里的同名 `Def` 是隔离的——各自展开、各自克隆，互不影响。
4. **`<Slot/>` 只能有一个子节点、最多出现一次。**面板甲传一个 `<Button>`（替换）；
   面板乙**不传子节点**（空插槽，合法，整个 `<Slot>` 节点被移除）。
   `LabCell` 没有 `<Slot/>`，所以传子节点会报 `GXML-DEF-007`（无此属性/事件）。

另外两条与本页直接相关但属于既有限制的：

- `Grid` 的子项**不得设 `Width`**（列定义拥有单元格宽度）；`<For>` / `<If>` 在 `Grid` 内
  不支持。所以本页的网格是**静态三行**，不做循环生成。
- 生成之前每个产物目录必须能解析为明确的 Go package（`代码生成输出契约.md` §3.2）：
  先有 `doc.go`，才有 `_gen.go`。

## 模板自带的测试在测什么

`view/form/sem_test.go` 与 `view/report/sem_test.go` 是离屏语义测试：用真实
`input.Router` 投递 pointer / key 事件，不创建窗口。坐标一律**实测扫描**，不硬编码像素。

| 用例 | 判据 |
|---|---|
| `TestLabFormDefDefaultAndOverrideAreDistinct` | 生成物里两个徽标各是一处**静态** `material.Body1(th, "…")`；槽位数恰为 2；两个实例的探针按钮左边界差 ≥ 24dp（徽标宽度差） |
| `TestLabFormGridFlexibleColumnAbsorbsWidth` | 视口加宽 160dp，「应用甲」左边界**精确**右移 160dp（弹性列吃掉全部增量）；弹性列起点不动（同一焦点坐标仍聚焦 input0）；原按钮位置不再有任何动作 |
| `TestLabFormDpiScalesHitTargets` | `PxPerDp=2` 下内容像素高度≈2×（±2%）、命中点坐标精确翻倍、绑定槽位内容不变 |
| `TestLabFormSlotReplacedAndEmptySlotVanishes` | 全页只有**一带**能触发 `slot-a`（空插槽没有可点内容）；连点 3 次即记 3 次；不串到「应用」接线 |
| `TestLabFormEventAttributionSeparatesInstances` | 点「应用甲」只动 `SavedA`/`CountA`；改草稿不写已保存值、不影响槽位；两个实例互不串值 |
| `TestLabFormReturnPayloadStaysOnItsOwnInput` | 两个输入框各自接 `Submit`，回车只记自己那侧载荷、不回写草稿 |
| `TestLabFormInstancesIsolated` | 两份生成的 View 各持独立模型 |
| `TestLabFormTwoViewsShareOneModel` | 两个视图绑同一指针：真事件写入后，另一个视图下一帧同值（不是复制同步） |
| `TestLabReportSlotsArePerNodeAndFollowModel` | 7 个槽位逐字对齐模型；`SavedA` 在两处节点上各占**一个**槽位（不按值合并）；连续推状态每帧都跟上 |
| `TestLabReportEmptyStringSlotStaysSlot` | 空串是合法绑定值：槽位不塌、渲染空串 |
| `TestLabReportFollowsFormHandlerOnSameModel` | form 侧绑定同一个处理器（`ApplyA`/`SlotA`）写入后，report 下一帧渲染出新值 |
| `TestLabReportInstancesIsolated` | 两份只读 View 各持独立模型 |

判据读的都是**模型计数与渲染串**（`CountA`/`CountB`/`SlotAClicks`/`Probes`、`slots[i].text`），
不读 `LastAction`：残留值会让「点到了哪个按钮」出现假命中。`LastAction` 只在扫描时
用来把一个点位归到某条接线上。

一条与直觉相反的实测结论：`EditEvent` 由编辑器在**自身 Layout 时**消费，回写晚于同帧
的 `Text` 渲染，所以「改草稿」之后要多跑一帧才能断言槽位；而按钮点击在 Layout 开头被
消费，同一帧就能看到模型变化。

## 缓存与维护闭环

- 缓存命中的证据不止「输出一样」：还要看**产物 mtime 没变**与
  `.goxml/generate-manifest.json` 字节没变。只看字节会把「重写了一遍同样的内容」误判成命中。
- 缓存键包含依赖闭包：只改 `model` 包（哪怕一行注释）就会让两个视图的缓存失效。
- **只改组件实参**（例如把第二个 `LabCell` 的 `Badge="…"` 换成另一个值）会改变产物字节，
  于是缓存必须失效并重写——这条在 `examples/gxml/componentlayout_project_test.go` 里是硬断言。
- 结构错误（XML 不闭合）、`Props` 里出现未声明属性、绑定到不存在的字段、两个视图声明
  冲突的同名符号 —— 都必须**保留旧产物**、退出码非 0、磁盘零变更；修好后再跑必须重新生成。
- Manifest 在**模块根**的 `.goxml/generate-manifest.json`，一次生成一条记录；两个视图各占一条。

## 本模板不做什么

- 不引入新 GXML 语法、不新增 CLI 选项、不改 CLI / compiler / runtime 生产代码。
- 不用 `<Expanded>`、不用 `ThemeRef`、不用 `For`/`If`：本模板只用基线已支持的静态子集，
  颜色全是静态字面量。
- 不注册成第六个示例：`examples/gxml/testdata/` 下的五个示例与注册项保持不变，
  本目录在 `testprojects/` 下，不参与 `-Example` 选择器。
- 不声称框架支持路由或滚动：上下组合是**入口自己**用 Go 布局代码写的，不是 `Page Route`
  生效。`Route` 只是声明，当前不驱动导航。

## 未测边界（如实列出）

1. **原生窗口、真实 DPI、输入法**不在自动验证范围：自动测试只**构建** `cmd/lab`，
   从不执行它。本文里的 DPI 验证是离屏 `unit.Metric` 缩放，不是真实显示器缩放。
2. **跨视图端到端只到「处理器写入」这一跳**：跨包读另一个视图的未导出槽位做不到，
   所以「点击 → 处理器 → 模型」在 `form` 包里用真实事件证，「模型 → report 槽位」在
   `report` 包里用同一个公开方法证。中间链接点是**同一个** `ApplyA`/`SlotA`。
3. **字体度量未固定**：探针左边界差只断言「≥ 一个按钮宽」与「在合理区间内」，
   不断言具体像素值——具体值取决于主题字体，写死会在换字体时假失败。
4. **`Grid` 只覆盖 `72 * 96` 与 `120 *` 两种列规则组合**，以及一维的宽度变化；
   多列弹性、`Gap` 为 0/负值、极小视口触发的压缩行为未覆盖。
5. **`Slot` 只覆盖「一个子节点」与「零个子节点」**两种形态；多子节点属于合同禁止项
   （`GXML-DEF-009`），只在负例里验证会被拒绝，不验证其渲染。
