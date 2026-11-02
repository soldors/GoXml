package examples

// Kept alongside the same source used by the tutorial/project test. Every
// location is probed from the current state, not a stale initial layout.
const workbenchReviewSemSource = `
func reviewState(h *harness) func(*ViewModel) {
 state := *h.vm
 items := append([]Item(nil), h.vm.Items...)
 return func(vm *ViewModel) { *vm = state; vm.Items = append([]Item(nil), items...) }
}

func reviewClick(t *testing.T, h *harness, action string) {
 t.Helper()
 points := scanOnce(t, reviewState(h))
 point, ok := points[action]
 if !ok { t.Fatalf("current layout has no %s; got %v", action, sortedActions(points)) }
 before := snap(h.vm)
 h.click(float32(point[0]), float32(point[1]))
 if got := before.deltaSince(h.vm); got != action { t.Fatalf("current-state click: got %q want %q", got, action) }
}

func reviewFocus(t *testing.T, h *harness) {
 t.Helper()
 state := reviewState(h)
 for y := 0; y <= frameH; y += 8 {
  for x := 0; x <= frameW; x += 8 {
   probe := newHarness(t, state); probe.frame(); probe.click(float32(x), float32(y))
   if !probe.gtx.Focused(&probe.v.input0) { continue }
   h.click(float32(x), float32(y))
   if !h.gtx.Focused(&h.v.input0) { t.Fatal("current-state focus did not reproduce") }
   return
  }
 }
 t.Fatal("current-state editor could not be focused")
}

func TestWorkbenchSemReviewDeleteAddSaveCurrentGeometry(t *testing.T) {
 h := newHarness(t, nil); h.frame()
 reviewClick(t, h, "select:2")
 reviewFocus(t, h)
 h.r.Queue(key.EditEvent{Range:key.Range{}, Text:"review-"}); h.frame()
 if !h.vm.Dirty || h.vm.Saves != 0 { t.Fatal("edit must dirty draft without saving") }
 want := h.vm.Draft
 h.r.Queue(key.Event{Name:key.NameReturn, State:key.Press}); h.frame()
 h.r.Queue(key.Event{Name:key.NameReturn, State:key.Release}); h.frame()
 if h.vm.titleOf(2) != want || h.vm.Saves != 1 || h.vm.Dirty { t.Fatal("return did not save and reset dirty") }
 reviewClick(t, h, "remove:2")
 if h.vm.SelectedID != 0 || len(h.vm.Items) != 2 { t.Fatal("delete did not clear selection") }
 // Deletion adds status and changes page geometry; probe it again rather than
 // reversing the workflow merely to keep an initial coordinate reusable.
 reviewClick(t, h, "add:4")
 reviewClick(t, h, "select:4")
 reviewFocus(t, h)
 h.r.Queue(key.EditEvent{Range:key.Range{}, Text:"new-"}); h.frame()
 want = h.vm.Draft
 reviewClick(t, h, "save")
 if h.vm.titleOf(4) != want || h.vm.Saves != 2 || h.vm.Dirty { t.Fatal("save after delete/add failed") }
 if h.vm.titleOf(1) != "读 GXML 语言规范" || h.vm.titleOf(3) != "跑一次真实生成" { t.Fatal("unselected stable IDs changed") }
}

// keyList 返回当前行状态池的 Key 集合（排序后），用于验证 Key 跟随数据而不是位置。
func keyList(h *harness) string {
 ks := []int{}
 for k := range h.v.rowClicks0 {
  ks = append(ks, k)
 }
 sort.Ints(ks)
 parts := []string{}
 for _, k := range ks {
  parts = append(parts, strconv.Itoa(k))
 }
 return "[" + strings.Join(parts, " ") + "]"
}

// TestWorkbenchSemReviewDeleteClearsDraftAndEditor 删除**当前选中项**后草稿与编辑器都要清。
//
// 这里刻意先编辑再删除：只有"草稿与列表不一致"时删除，才能验证清理是真的发生了，
// 而不是"草稿本来就空"。
func TestWorkbenchSemReviewDeleteClearsDraftAndEditor(t *testing.T) {
 h := newHarness(t, nil); h.frame()
 reviewClick(t, h, "select:2")
 reviewFocus(t, h)
 h.r.Queue(key.EditEvent{Range: key.Range{}, Text: "dirty-"}); h.frame()
 if !h.vm.Dirty { t.Fatal("编辑后应判脏（前置条件不成立则本条无意义）") }
 reviewClick(t, h, "remove:2")
 if h.vm.SelectedID != 0 || h.vm.HasSelection {
  t.Fatalf("删除选中项应清空选择: SelectedID=%d HasSelection=%v", h.vm.SelectedID, h.vm.HasSelection)
 }
 if h.vm.Draft != "" { t.Fatalf("删除选中项应清空草稿，实际 %q", h.vm.Draft) }
 if h.vm.CountText != "共 2 项" { t.Fatalf("删除后计数应为「共 2 项」，实际 %q", h.vm.CountText) }
 h.frame()
 if h.v.input0.Text() != "" { t.Fatalf("外部值改回后编辑器应同步为空，实际 %q", h.v.input0.Text()) }
 if h.vm.Dirty || h.vm.DirtyText != "（无未保存修改）" {
  t.Fatalf("删除选中项后应回到不脏: Dirty=%v 文案=%q", h.vm.Dirty, h.vm.DirtyText)
 }
 // 观察（不作判据）：初始布局缓存下来的「新增」坐标，在删除导致布局位移后是否仍命中。
 // 这是 reviewClick 必须按当前几何重扫的原因，作为反例固定下来。
 if sx, sy, ok := posOf(t, "add:4"); ok {
  before := snap(h.vm)
  h.click(float32(sx), float32(sy))
  t.Logf("OBS 删除后复用初始布局缓存的 add:4 坐标 (%d,%d)：动作=%q（重扫才有意义）", sx, sy, before.deltaSince(h.vm))
 }
 t.Log("OBS 删除选中项：选择清空、草稿清空、编辑器同步为空、计数 2 项、回到不脏")
}

// TestWorkbenchSemReviewIdsNeverReusedAndRowKeysStable 新增永不复用已存活过的 ID，
// 且行状态池的 Key 始终等于当前 ID 集合（删除即回收，不残留、不串位）。
func TestWorkbenchSemReviewIdsNeverReusedAndRowKeysStable(t *testing.T) {
 h := newHarness(t, nil); h.frame()
 if got := keyList(h); got != "[1 2 3]" { t.Fatalf("初始行 Key=%s", got) }
 reviewClick(t, h, "remove:2")
 h.frame()
 if got := keyList(h); got != "[1 3]" { t.Fatalf("删除 #2 后行 Key=%s（被删键应回收）", got) }
 reviewClick(t, h, "add:4")
 h.frame()
 if len(h.vm.Items) != 3 { t.Fatalf("删 1 行再新增应仍是 3 行，实际 %d", len(h.vm.Items)) }
 last := h.vm.Items[len(h.vm.Items)-1]
 if last.ID != 4 { t.Fatalf("新增应取 ID=4（不复用已删除的 2），实际 %d", last.ID) }
 if got := keyList(h); got != "[1 3 4]" { t.Fatalf("新增后行 Key=%s", got) }
 reviewClick(t, h, "remove:1")
 h.frame()
 reviewClick(t, h, "add:5")
 h.frame()
 last = h.vm.Items[len(h.vm.Items)-1]
 if last.ID != 5 { t.Fatalf("再次新增应取 ID=5（不复用已删除的 1/2），实际 %d", last.ID) }
 if got := keyList(h); got != "[3 4 5]" { t.Fatalf("再删再增后行 Key=%s", got) }
 t.Logf("OBS Key 跟随数据：[1 2 3] → 删#2 [1 3] → 增 [1 3 4] → 删#1 → 增 [3 4 5]")
}

// TestWorkbenchSemReviewTwoInstancesIsolatedCurrentGeometry 两实例在当前几何下仍隔离。
func TestWorkbenchSemReviewTwoInstancesIsolatedCurrentGeometry(t *testing.T) {
 a := newHarness(t, nil)
 b := newHarness(t, nil)
 a.frame(); b.frame()
 bDraft, bEditor, bChanges := b.vm.Draft, b.v.input0.Text(), b.vm.Changes
 reviewClick(t, a, "select:1")
 reviewFocus(t, a)
 a.r.Queue(key.EditEvent{Range: key.Range{}, Text: "only-a-"}); a.frame()
 if a.vm.SelectedID != 1 { t.Fatalf("A 的选择未生效: %d", a.vm.SelectedID) }
 if b.vm.SelectedID != 0 || b.vm.HasSelection {
  t.Fatalf("B 不应被 A 影响: SelectedID=%d HasSelection=%v", b.vm.SelectedID, b.vm.HasSelection)
 }
 if b.vm.Draft != bDraft { t.Fatalf("B 的草稿被改写: %q → %q", bDraft, b.vm.Draft) }
 if b.vm.Changes != bChanges { t.Fatalf("B 不应产生回写: %d → %d", bChanges, b.vm.Changes) }
 b.frame()
 if b.v.input0.Text() != bEditor { t.Fatalf("B 的编辑器被改写: %q → %q", bEditor, b.v.input0.Text()) }
 t.Logf("OBS 隔离（当前几何）：A SelectedID=1；B SelectedID=0 Draft=%q editor=%q", b.vm.Draft, b.v.input0.Text())
}

// TestWorkbenchSemReviewDirtyTextExactBothWays 脏标记**逐字**双向断言。
//
// R-10 的教训：两态文案都含"未保存"三字，子串判据永远不红。这里改用**全等比较**，
// 任何一种文案被改成都无法通过；变异复算见交付记录的 f1-04。
func TestWorkbenchSemReviewDirtyTextExactBothWays(t *testing.T) {
 h := newHarness(t, nil); h.frame()
 if h.vm.DirtyText != "（无未保存修改）" {
  t.Fatalf("未选择时文案应逐字为「（无未保存修改）」，实际 %q", h.vm.DirtyText)
 }
 reviewClick(t, h, "select:3")
 if h.vm.DirtyText != "（无未保存修改）" {
  t.Fatalf("刚选中时（草稿==标题）文案应仍为「（无未保存修改）」，实际 %q", h.vm.DirtyText)
 }
 reviewFocus(t, h)
 h.r.Queue(key.EditEvent{Range: key.Range{}, Text: "x"}); h.frame()
 if h.vm.DirtyText != "（草稿有未保存的修改）" {
  t.Fatalf("输入后文案应逐字为「（草稿有未保存的修改）」，实际 %q", h.vm.DirtyText)
 }
 reviewClick(t, h, "save")
 if h.vm.DirtyText != "（无未保存修改）" {
  t.Fatalf("保存后文案应回到「（无未保存修改）」，实际 %q", h.vm.DirtyText)
 }
 t.Log("OBS 脏标记逐字：无未保存 → 无未保存 → 有未保存 → 无未保存")
}
`
