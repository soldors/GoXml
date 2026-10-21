package theme

import "testing"

// ============================================================================
// TASK-FONT-001-A..D 的**占位用例**（THEME-SEAM-001-6）
//
// 为什么不是删掉、也不是留空：
//
//   - 任务卡里四条验收命令形如 `go test -run TestFontProbe ./pkg/runtime/theme/...`。
//     在包不存在时，这类命令返回 ok 但什么都没跑——即「空转报绿」。
//     包建起来之后，如果 `-run` 找不到同名测试，Go 依然打印
//     "no tests to run" 并返回 0，**仍然是空转报绿**。
//   - 因此这里为四项各放一个同名、`t.Skip` 的占位用例：命令会真正命中
//     一个测试，输出 `--- SKIP: ...`，任何人都能一眼看出「未执行」，
//     而无法被误读为通过。
//
// 接线纪律：实现某一项时，把对应的 `t.Skip` 换成真实断言（函数名保持不变，
// 任务卡的验收命令才不会失效）。若改名，必须同步改 `docs/04-WBS/任务卡/TASK-FONT-001.md`。
// ============================================================================

// TestFontProbe 已由 TASK-FONT-001-A 实现：占位用例删除，真实断言见 font_test.go。
// TestEmbeddedFallback 已由 TASK-FONT-001-B 实现：占位用例删除，真实断言见
// embedded_fallback_test.go（TestEmbeddedFallbackMetadata / SubsetCoverage /
// ParseAndCorrupted / SelectionOrder / SizeBudget / FailureSemantics）。
// 前缀 TestEmbeddedFallback 保持不变，任务卡的验收命令才不会退化为空转。
// 另外两项（C/D）仍是显式 skip 占位，未实现。

// TestFontFallbackChain 占位：TASK-FONT-001-C 按字形覆盖的 fallback 链。
func TestFontFallbackChain(t *testing.T) {
	t.Skip("未执行：TASK-FONT-001-C 未实现。需要覆盖：中英混排、emoji、" +
		"按**字形是否存在**（而非字体族是否存在）选择 fallback，以及缺字形时的行为")
}

// TestFontLazyLoad 占位：TASK-FONT-001-D 懒加载与加载失败诊断。
func TestFontLazyLoad(t *testing.T) {
	t.Skip("未执行：TASK-FONT-001-D 未实现。需要覆盖：不在 init/main 同步加载、" +
		"首次 shaping 时加载或异步加载后 Invalidate、加载不阻塞首帧、" +
		"失败时给出诊断而非静默空白")
}
