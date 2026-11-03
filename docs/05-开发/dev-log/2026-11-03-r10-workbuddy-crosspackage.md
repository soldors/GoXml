# R10-C 跨包偏好设置工程模板（WorkBuddy 线）

分支 `codex/product-r10-workbuddy-20261103`，基线 `29a0e3b3ce61ece7ba0807ee97a73d595ce8a0db`，
工作树 `C:/Users/admin/.codex/worktrees/product-r10-workbuddy/Goxml`。
本线只写 `examples/gxml/` 下的新工程材料、新专项测试与本文件；未改 CLI / Compiler /
runtime / scripts / 正式契约。

## 1. 三片提交

| 片 | SHA | 内容 |
|---|---|---|
| C1 | `020dbb0` | 工程材料：`examples/gxml/testprojects/crosspackage-preferences/`（model / edit / summary / 入口 / go.mod 模板 + README），8 文件 552 行 |
| C2 | `543d2b5` | 工程级用例 `examples/gxml/crosspackage_project_test.go` + 模板自带的两个离屏语义测试（作为材料），3 文件 1093 行 |
| C3 | `ecb83d2` | 缓存命中/失效/保旧/负例/两类变异判据 + README 重写为完整教程，2 文件 +331/−19 |

## 2. 结构与被测事实

```
model/preferences.go       唯一手写状态（草稿 / 已保存值 / 派生串 / 计数）
view/edit/edit.gxml        ViewModel="model.Preferences" → 产物落在 package edit
view/summary/summary.gxml  同一个 ViewModel，只读，无事件无输入
cmd/preferences/main.go    NewPreferences() 只调一次，指针交给两个生成视图
```

两个 `.gxml` 都声明**跨包** ViewModel，所以生成产物里有 `import ".../model"`，
产物包名与 ViewModel 包名不同——这就是本模板要钉的接线。

## 3. 实测读数（命令 + 值 + 是否全量）

全部为 `go test ./examples/gxml/ -run '^TestCrossPackageProject$' -count=1 -v -timeout 30m`
（单测入口，非全仓）：

- C2 首次通过：`PASS 8.26s`；C3 两次通过：`16.83s` / `16.61s`（C3 加了缓存与变异相位）。
- 生成产物：`view/edit/edit_gen.go` 13787 B，sha256 `245e01e9f86ca07fdf8af7592444377876780ede9333d2b8fb4d4c56cf4d16bd`；
  `view/summary/summary_gen.go` 9386 B，sha256 `d666310b128d24bfc4d987031de084c981d95b47eebef0402ded2f8f7a269f43`。
- Manifest：模块根 `.goxml/generate-manifest.json`，468 B，edit/summary 各一条；缓存命中时字节未变。
- 离屏语义用例 11/11 PASS（edit 9 + summary 2），外层用例逐条核对 `--- PASS: <name>` 与计数，
  并拒绝 `--- SKIP` / `no test files` / `no tests to run`。
- 窗口入口 `cmd/preferences` 构建产物 12041216 B（只构建，未运行）。
- 缓存：第二次生成打印「跳过 …（缓存命中）」，产物 mtime 仍是哨兵值 `2001-01-01`；只改 model
  一行注释后两个视图都重写（mtime 更新为实测时间）而字节不变；`--no-cache` 重写且字节等价。
- 两类可编译变异各自只让目标用例变红（输出无 `[build failed]`）：
  `M1 m.SavedName = m.Name → _ = m.Name` → `TestCrossEditConfirmWritesSaved`；
  `M2` 确认后不调 `refresh()` → `TestCrossSummaryFollowsSharedModel`。
- 模板 7 个材料文件指纹 before/after 一致（用例只写临时目录）。

## 4. 两条反直觉的实测结论（已写进模板注释）

1. **`EditEvent` 的回写晚于同帧渲染**：事件由编辑器在**自身 Layout 时**消费，而草稿上方的
   脏标记 `Text` 节点在它之前就渲染完了，所以脏标记槽位要**下一帧**才更新。第一次写用例时
   就是按「同帧一致」断言的，实测红，改为加一帧「沉降」并在注释里写明原因。
2. **按钮点击在 Layout 开头被消费**，因此摘要视图**同一帧**就能看到新的已保存值——
   这一点是断言出来的（确认后不等帧直接读 `slots[0].text`），不是推测。
3. 附带一条几何结论：「清除提示」在提示区出现后才存在，坐标与初始态不同（实测初始态扫描
   与确认后状态相差 144 dp），按钮必须按状态重扫，缓存不能跨状态共用。

## 5. 发现的真实缺陷（未在本树修，交 CLI 所属线）

跨包 ViewModel 的 import **别名取的是 `.gxml` 源目录的包名**，而不是 ViewModel 所在包的包名。
产物顶部因此是 `import edit "example.com/<mod>/model"`，类型表达式写成 `*edit.Preferences`——
类型同一个、`go build`/`go vet` 全绿，但读代码的人会以为 `Preferences` 定义在 `edit` 包里。
最小反例见交付的 `findings/cross-package-vm-alias.md`。本线不修 CLI，也不靠改包名绕过去。

## 6. 边界与未验证项

- 原生窗口、IME 预编辑、真实 DPI：**未自动验证**；自动测试只 build 入口。
- 不做路由：入口两栏并排是 Go 布局代码，`Page Route` 当前不驱动导航；无滚动。
- 未改 `Input` 既有语义（`MaxLen` 按 rune、`Filter` 后无 `ValueChanged`、`Submit` 不回写草稿）。
- 稳态：模板材料全部是 `*.go.txt` / `go.mod.txt`，不参与仓库根模块构建；
  `testdata/` 下五个示例一字未动，未新增第六个 `-Example` 注册项。
- 审核 B 线：`product-r10-zcode/Goxml` 工作树 HEAD 仍是基线 `29a0e3b`，
  `product-r10/zcode/` 下只有 `prompt.txt`/`task.md`，**无冻结新 SHA → 记 PENDING**。

## 7. 下一条命令（续接用）

```bash
cd C:/Users/admin/.codex/worktrees/product-r10-workbuddy/Goxml
CGO_ENABLED=0 GOFLAGS=-mod=readonly go test ./examples/gxml/ -count=1 -timeout 30m
```
