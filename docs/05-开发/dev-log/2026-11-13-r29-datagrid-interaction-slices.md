# 2026-11-13 · R29-B DataGrid 交互扩展实现日志（S1/S2/S3）

- 基线 ddef3a4d52588c3edebcb0dfa999c048addecd15（R28 发布后），分支
  `codex/r29-glm-datagrid-interaction`，工作树
  `C:/Users/admin/.codex/worktrees/r29-glm-datagrid-interaction/Goxml`。
- **S1 双轴视口**（d5774b5）：`EnableHorizontalScroll()` 显式 opt-in 开启
  横向滚动；滚轮 X/Y 按轴独立，表头与表体共享同一列偏移，越界按列宽
  总和与视口宽钳制；`pointer.Filter` 以 `ScrollX/ScrollY` + `ScrollRange`
  声明；关闭路径不新增事件监听。测试 6 项（默认关、共享偏移、轴独立、
  双轴钳制、滚动后命中、空表边界、自定义 Cell 含偏移）。
- **S2 排序**（a4e3002）：`SortRows(less)` 显式更新阶段 `sort.SliceStable`
  稳定排序——等值行保持原相对顺序、RowID 身份不变；排序后重建
  RowID→索引映射并清文本缓存，键盘邻接与点击命中跟随新顺序。
  测试 4 项（键盘邻接、命中、缩短清选、等值稳定）。
- **S3 多选**（本提交）：`EnableMultiSelect()` 显式 opt-in。Ctrl+点击
  切换、Shift+点击/Shift+方向键按锚点范围**替换式**选中（锚点=最后一次
  非 Shift 选择的 RowID）、普通点击/方向键收拢为单行；选择集按 RowID
  表示，`SortRows` 重排天然保留，`SetRows` 缩短/清空剔除失效项并在锚点
  失效时退化。实现分三处：`handleEvents` Release 分支在身份判定通过后
  按 `key.Modifiers` 分派；`handleKeyboard` 提交选择统一走
  `commitKeyboardSelection`；`drawRow` 在多选模式下由选择集驱动高亮
  （单选模式 selID 驱动，原行为不变）。导航按键过滤器补
  `Optional: key.ModShift`——Gio 过滤器要求事件修饰键落在
  Required|Optional 之内，不声明则 Shift+方向键不匹配。
  测试 8 项（默认关、Ctrl 切换、Shift 范围、键盘扩展、Ctrl+点击聚焦、
  排序保留、缩短失效、空表与关闭）。
- 回归口径：`go test ./pkg/widgets/ -count=1` 94 PASS / 0 FAIL / 0 SKIP；
  `go build ./...` 全树通过；`go vet ./pkg/widgets/` 干净。
- 候选边界：S1–S3 均为默认关闭、显式 opt-in 的候选语义，未批准不入
  规范；原生窗口下的实机连发/IME/DPI 验收仍待协调者组织。
