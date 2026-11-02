# 2026-11-02 R5 夜间开发日志（WorkBuddy C 线：可编辑列表与表单联动工作台）

工作树 `C:\Users\admin\.codex\worktrees\night-r5-workbuddy-20261102\Goxml`，
分支 `codex/night-r5-workbuddy-20261102`，基底
`7631a7952c25fa7cab6655008c91418eb97c198a`（开工时 clean，已核）。

## 切片 C1：同源工程 `examples/gxml/testdata/workbench/`

新增 `workbench.gxml` / `vm.go` / `main.go.txt`（教程、自动测试、一键脚本
**共用同一份**）。它是 tasks（列表与行事件）与 forms（受控输入）的组合教学示例：

| 能力 | 落点 |
|---|---|
| 稳定 ID 列表 | `<For Each="item" In="{Items}" Key="{item.ID}">`，行事件带的是 ID 不是下标 |
| 点击选择 | 行内 `<Button Text="选择" Click="Select(item.ID)">` → 填草稿 + 准备选择信息 |
| `<For>` 外的独立草稿编辑器 | `<Input Value="{Draft}" ValueChanged="OnDraftChanged" Submit="SaveSubmit"/>` |
| 保存 / 提交才写回列表 | `Save()`（按钮）与 `SaveSubmit(text)`（回车）共用一段 `save()`；空草稿 / 未选择一律拒绝 |
| 新增不复用 ID | `nextID` 单调递增，删除不回退 |
| 删除选中 / 非选中 | `Remove(id)`：删的是选中项才清空选择与草稿；删其它项不影响选择 |
| 空态与恢复 | `HasItems` 驱动 If/Else；`HasStatus` 驱动提示区；显示串全部在处理器里准备 |

`ValueChanged` **只写草稿**——这是本工程与"输入即改数据"的分界，也是断言重点。
保存一律用重求值后的 `Draft`，不用事件实参（与契约 §5.2 路径 2 同源）。

## 切片 C2：真实用户流程的离屏证据

新增 `examples/gxml/workbench_project_test.go`：真实 CLI 生成（含缓存命中 mtime
不变、合法修改产物变化、`GXML-BIND-001` 失败且旧产物保留）+ 独立 module 的内嵌
离屏语义测试 + 窗口入口 `go build`（不执行）。

事件全部走真实链路：行/按钮用 pointer Press+Release 经 `input.Router` 队列投递；
文本用 `key.EditEvent`；回车用 `key.Event{NameReturn}` Press/Release 且**与输入
不同帧**；焦点由真实指针点击获得。直接调用 VM 方法只用于**布置前置状态**。

实测（rc 0，raw `c2-05-sem-verbose.out`，8/8 PASS）：

| 用例 | 实测 |
|---|---|
| 完整流程链 | 选择 → 草稿 `"X读 GXML 语言规范"`（列表未变）→ 回车保存 → 删除选中项后 `items=2`、编辑器同步为空 |
| 无回车/保存负对照 | 后续 3 帧列表不变、草稿保留 |
| 「保存」按钮 | 与回车共用保存逻辑：`"写列表示例"` → `"Z写列表示例"` |
| 空草稿拒绝 | `Status="草稿为空，未保存 #2"`，`Saves=0`，列表不动 |
| 删其它行 | 选择仍为 #1，剩余行 ID 为 1/3（不串行） |
| 键状态池 | 删除后 `rowClicks0/1` 3 → 2，被删键已清理 |
| 空态→新增 | 空列表扫不到任何行事件；新增后 ID=4（不复用）、行状态池=1 |
| 两实例隔离 | A 选择后 B 的 `SelectedID=0`、编辑器与回写计数均不变 |

初始状态扫描命中的动作：`[add:4 remove:1 remove:2 remove:3 select:1 select:2 select:3]`。

### 三处自伤（均已修，复跑为绿）

1. **扫描键用 `LastAction` 会失效**：点「选择」改了草稿，紧随的外部值同步会再
   触发一次 `ChangeEvent`，把 `LastAction` 覆盖成 `"change"`（R4 实测行为的直接
   后果），扫描永远定位不到行按钮。改为**不被覆盖的计数器**（Selects/Removes/
   Saves/Adds/StatusClears + 对应 ID）。
2. **增量 vs 绝对**：扫描用例常用 VM 方法预置前置状态（如"已删除第 2 行"），
   那些调用本身已改过计数器；只看绝对值会把预置状态当成命中（表现为每次点击都
   返回 `remove:2`）。改为相对快照的**增量**判定。
3. **扫描范围没覆盖整幅画布**：画布 720×480，而扫描只到 x≤640，行尾的「删除」
   按钮（约 x 660+）扫不到。改为按 `frameW/frameH` 扫描。

## 切片 C3：一键入口与教程

- `scripts/verify-examples.ps1`：示例名只在 `$known` **注册一处**（`all` 列表与
  `-Example` 白名单共用），新增 `workbench`；同时同步脚本头注释。锁助手、通用
  启动、marker/退出码机制**未改**。
- `scripts/test-verify-examples-workbench.py`：专用选择测试，5 条判据
  （`-Example workbench` 只出现 workbench；`-Example all` 四个示例齐全且各有生成物
  与入口产物；`-Example bogus` rc 2 且列出 workbench；源材料缺 `vm.go` → rc 7；
  `-Example all` + `-SourceDir` → rc 2，既有规则未被破坏）。脚本自带 try/finally
  持放公共锁，释放失败即判 FAIL。
- `examples/gxml/README.md`：新增 workbench 教程段（复制 / 生成 / 构建 / 手动运行 /
  点击路线 / 预期状态 / 常见错误），并把"一键验证脚本"段的示例数改为四个。
  原生呈现、IME、物理焦点**单列为人工未验**。
- `scripts/test-verify-examples-forms.py`：本线把 `workbench` 加进 `$known` 后，
  F2（`-Example all` 的精确目录列表）判据必须同步为四者——改判据，不改工具。

双宿主实测（`-Example workbench`，各跑**一次**，均显式 `-LockPath` 指向公共
heavy.lock，日志可读到"已取得锁 / 已释放自己的锁"，两轮 run id 与 token 不同）：

| 宿主 | 版本 | rc | `workbench_gen.go` | 窗口入口 |
|---|---|---|---|---|
| Windows PowerShell 5.1 | `5.1.26100.9711` Desktop | 0 | 11056 字节 | 12001792 字节 |
| PowerShell 7 | `7.6.9` Core | 0 | 11056 字节 | 12001792 字节 |

（C3 时点的数字。C4 改材料后已重测为 11541 / 12005376，见下。）

选择器专项 5/5 PASS：`-Example all` 里 `forms_gen.go` 7212 字节与上一轮 forms
实测一致，可作跨轮一致性旁证。

### 自伤 4：锁日志判据在混合编码缓冲里检索中文

测量驱动第 1 版把"脚本确实取到了公共锁"写成**在解码后的文本里检索 `已取得锁`**。
重定向缓冲是混合编码（脚本 `Write-Host` 走 cp936、go/goxml 子进程是 UTF-8），
按任一编码 decode 后检索都会漏（实际落到了 latin-1 回退，整段变乱码），
于是两个宿主 rc 0、产物齐全却被判 `failed`。

修正：判据改为**字节层面**比对两种编码的目标串（`gbk` / `utf-8`），并**按已留档的
原始字节重算**（`work/recheck_hosts_lockline.py`），**没有重跑构建**——事实没变，
是我的判据错了。README 的"混合编码"提示也补上了这条。

## 切片 C4：派生显示（计数 / 脏标记）+ 证据稳健性加固

### 能力：两个**纯派生**显示字段

- `CountText`（"共 N 项"）与 `Dirty` / `DirtyText`（草稿 ≠ 选中行标题）
  都在 `refresh()` 里由 `len(Items)` 与 `Draft != titleOf(SelectedID)` 重算，
  **不是随事件累加的变量**。
- 为什么要派生：独立变量意味着"每条改 Draft / 改 Items 的路径都要记得维护它"，
  漏一处就是静默失真；派生量只有一处真相（`OnDraftChanged` 因此也必须调 `refresh`）。
- 页面只加了两处：标题行并入 `<Text Value="{CountText}" Width="Fill"/>`（不增高），
  以及 `Selection` 后一行 `<Text Value="{DirtyText}"/>`。**没有**为脏标记再加
  If/Else——两态都写在派生串里，本页的条件显示已由 `HasItems`/`HasSelection`/
  `HasStatus` 演示过，重复一遍只是噪音。
- 这两个字段**不引入任何新的点击事件、不改任何已有计数器**，因此对 C2 的 8 条
  判据零影响（新增用例是纯增量）。

### 稳健性加固

- **多画布扫描**：`newHarnessSized` / `clickAtSized` / `scanSized` 让画布尺寸可
  指定；720×480 / 480×360 / 1024×768 三套尺寸各自全网格扫描，六个行事件
  （`select:1..3`、`remove:1..3`）在**三套尺寸下全部命中**，且命中坐标互不相同
  （`remove:3` 的 x = 672 / 432 / 976）——证明用例测的是布局语义而非魔数。
  另观察：三套尺寸下「新增」都可达，即本页在 480×360 内也没溢出。
- **完整链重复 5 次**：同一条"选择 → 输入 → 回车保存"跑 5 遍，结果一致。
  单跑一次只证明"这次没失败"，重复 N 次才谈得上稳定。

实测（rc 0，raw `c4-02-sem-verbose.out`，**13/13 PASS**）：原有 8 条 + 计数 +
脏标记生命周期 + 多画布扫描 + 重复 5 次 + nil VM 拒绝。

### 自伤 6 / 7（均已修，复跑为绿；编号接 C3 的自伤 5「gofmt」）

5. **`newHarnessSized` 的局部变量 `h` 与高度形参 `h` 撞名**：`:=` 左侧无新变量，
   编译直接失败（`no new variables on left side of :=`）。改名 `hh`，并在注释里
   写明为什么不能叫 `h`。
6. **计数用例先删除、后新增 → 点击落空**：`add:4` 用的是"三行无提示"状态下缓存
   的坐标，而删除会让底部多出一行状态提示（`HasStatus` 的 Then 分支），页级按钮
   整体上移，旧坐标不再命中。改为**先新增后删除**（新增的行追加在末尾，不会顶开
   前面的行按钮），并在用例注释与 README 常见错误里写明：缓存坐标只在缓存时那套
   布局下有效。这不是产品缺陷，是"扫描坐标复用"的适用边界。

### 自伤 8（自审发现，已修 + 变异取证）：脏标记断言是空判据

自审时发现：脏标记用例里写的是 `strings.Contains(DirtyText, "未保存")`，而两态文案
分别是「（草稿**有**未保存的修改）」与「（**无**未保存修改）」——**"未保存"三字在两态里
都出现**，所以这条断言在"不脏"时同样通过，**永远不可能变红**。这正是本仓库明令禁止的
空判据（"每条断言先证明能变红"）。

修正：锚到能区分两态的子串（"有未保存" / "无未保存"），并**双向**断言——脏时要求含
"有未保存"且不含"无未保存"，不脏时反之。

变异取证（不是"改完就绿"就算完）：把 `refresh()` 里脏态文案改成不脏文案后实跑，
用例确实报红 `脏时应提示未保存，实际 "（无未保存修改）"`（raw
`c4-04-mutation-dirtytext.out`，rc 1）；随后按字节 SHA-256 恢复 `vm.go`
（`git diff` 为空、无 MUTATION 残留）并复跑为绿。

### 自审发现清单（作者自审，**不替代非作者复核**）

| # | 严重度 | 问题 | 处置 |
|---|---|---|---|
| R-10 | 高 | 脏标记断言用 `Contains("未保存")` ⇒ 两态都通过，空判据 | 已修 + 变异取证（自伤 8） |
| R-1 | 中 | C2 段标题"两处自伤"却列 3 条 | 改为"三处" |
| R-2 | 中 | 自伤编号冲突：本文件 5/6 vs slices.json 6/7 vs README 6 | 统一为 6 / 7 |
| R-3 | 中 | C3 双宿主表是 C4 前的旧数字 | 标注时点 + 指向 C4 重测值 |
| R-4 | 中 | README"一条完整链与五条负面对照"与实际 12 条不符 | 改为如实描述 |
| R-5 | 中 | "重复 5 次"被写成稳定性证据 | 收敛为"跨轮共享缓存不污染"，明说它不是 flaky 证据 |
| R-6 | 轻 | `Dirty` 不被页面绑定、只被断言消费，未注明 | 字段注释写明用途 |
| R-7 | 轻 | 草稿仅空白时"脏却保存不了"属刻意行为但未说明 | README 常见错误补一行 |
| R-8 | 轻 | `empty` 前置闭包在两处重复 | 提取 `emptyItems` |

## 提交记录（本线）

| 切片 | SHA |
|---|---|
| C1 同源工程材料 | `e6c0947dc564b161403e92cd6a56416e9c8932c4` |
| C2 离屏事件链证据 | `572f39ee91c96b57a73be00c328ece68ed7cb068` |
| C3 一键入口与教程 | `e921e83c1b2a3c62fec7c3e41170863fc58f562f` |
| C3 跟随：gofmt 修正 | `646c81440f4d14ea6a5c08646a3182c3a72f6103` |
| C4 派生显示 + 稳健性加固 | `ad0141c42c737fd4735628ab0a78ac570e5c5316` |
| C4 自审修正（空判据 + 变异取证） | `2b66610e1ca20954ea274dd44c75b2363860aa61` |
