package theme

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

// TestFontFallbackChain 已由 TASK-FONT-001-C 实现：占位用例删除，真实断言见
// fallback_test.go（TestFontFallbackChainGlyphIDDecode / Notdef / MixedScript /
// OrderSemantics / SupplementsMissingGlyph / Coverage / Emoji /
// SupplementOnDemand / CoverageInsufficient / ProbeCost）。
// 前缀 TestFontFallbackChain 保持不变，任务卡的验收命令才不会退化为空转。

// TestFontLazyLoad 已由 TASK-FONT-001-D-IMPL-1 实现：占位用例删除，真实断言见
// lazy_test.go（TestFontLazyLoadStateMachine / NoIOOnConstruct / Coalesce /
// NonBlockingOnLoading / NoAutoRetry / ExplicitRetry / ThemeHasNoSideEffects /
// TakeOnce / FailureKeepsTemp / NotifyBeforeRequest / NotifyAfterRequest /
// TempOnlyBuiltOnce / TempBuiltOnceConcurrent / TempDoesNotPolluteProbe /
// SetNotifyNil / FailureCompletedAt / ReportIsCopy）。
// 前缀 TestFontLazyLoad 保持不变，任务卡的验收命令才不会退化为空转。
