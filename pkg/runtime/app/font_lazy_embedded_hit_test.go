package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/goxml/goxml/pkg/runtime/theme"
)

// countingEmbeddedLoader 记录嵌入兜底加载器被调用的次数（D-ACCEPT-03 的观察点）。
//
// 为什么必须靠注入计数器：`ProbeReport.Embedded.Attempted` 只说明"诊断是否记录"，
// 无法证明加载器**真的没被调用**——只有计数器能。
type countingEmbeddedLoader struct {
	calls int
	err   error
}

var errUnexpectedEmbeddedLoad = errors.New("diag: 本用例不期望嵌入加载成功")

func (c *countingEmbeddedLoader) Load() ([]byte, string, error) {
	c.calls++
	if c.err != nil {
		return nil, "", c.err
	}
	return nil, "", errUnexpectedEmbeddedLoad
}

// waitTerminal 等到加载进入**终态**（Ready 或 Failed）。
//
// 为什么不复用 `requireReady`：它只在 Ready 时返回，失败场景会一路等到超时再 Fatal，
// 那样既慢又会把"预期的失败终态"报成超时。
func waitTerminal(t *testing.T, lz *theme.Lazy, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s := lz.State(); s == theme.LoadReady || s == theme.LoadFailed {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%v 内未进入终态（state=%s）", timeout, lz.State())
}

// TestFontLazyLoadSystemHitSkipsEmbeddedAsync `D-ACCEPT-03` 的专用证据。
//
// # 为什么需要本用例（既有两条入口各覆盖一半）
//
//   - `TestEmbeddedFallbackSelectionOrder/系统命中时不得解析嵌入字体`（theme 包）
//     断言 `cl.calls == 0`，证据最硬，但它**直接调 `ResolveWithReport`**
//     （Provider 直调、同步形态），**不在 Lazy 异步路径上**；
//   - `TestFontLazyLoadTempDoesNotPolluteProbe`（theme 包）在 **Lazy 异步路径**上，
//     但**前提就是 `cl.calls == 1`**（临时主题确实解析过嵌入一次），
//     断言的是"这次解析**不写进** `ProbeReport`"——属**诊断隔离**。
//
// ⇒ 缺的正是：**Lazy 异步路径 + 行为侧（最终链有没有追加一次嵌入加载）**。
//
// # 计数基线（关键，不能直接断言 0）
//
// 临时主题本身就是**嵌入子集**，构造时必须解析一次嵌入 ⇒ **正常的系统命中场景下
// `calls == 1` 是预期值，不是缺陷**。若直接断言 `calls == 0` 会误判为失败。
// 真正要证明的是：**最终链有没有在那 1 次之外再追加一次** ⇒ 判据是 `calls == 1`。
//
// # 对照实验（防止 `calls == 1` 是平凡真）
//
// 子测试 2 把系统候选全部指向不存在的路径，让最终链必须尝试嵌入兜底：
// 此时 `calls` 必须变成 **2**（临时 1 + 最终链 1）。这证明计数器**确实能观测到
// 最终链的追加调用**，从而让子测试 1 的 `calls == 1` 具备判别力。
func TestFontLazyLoadSystemHitSkipsEmbeddedAsync(t *testing.T) {
	t.Run("系统命中的异步路径：最终链不追加嵌入加载", func(t *testing.T) {
		cl := &countingEmbeddedLoader{}
		p := theme.NewProvider(theme.ProviderOptions{Embedded: cl})

		lz, err := theme.NewLazy(theme.LazyOptions{Provider: p})
		if err != nil {
			t.Fatalf("构造 Lazy 失败: %v", err)
		}
		lz.Request()
		requireReady(t, lz)

		rep := lz.Report()
		if rep.State != theme.LoadReady {
			t.Fatalf("本平台未产生可用的系统字体命中（state=%s, err=%v）："+
				"本用例前提不成立，**不得**当作通过——需在具备 CJK 系统字体的平台重跑",
				rep.State, rep.Err)
		}
		if rep.Probe == nil {
			t.Fatal("Ready 后 Probe 为 nil：无法判定本轮是否执行了探测")
		}
		// 前提守卫：必须是**本轮真探测**，否则"没追加加载"可能是缓存命中造成的平凡真。
		if !rep.Probe.ProbeRan {
			t.Fatal("本轮 ProbeRan=false：Provider 未执行探测（疑似命中缓存），判据失效")
		}

		// 关键断言：1 次＝临时主题那一次；最终链**没有**追加。
		if cl.calls != 1 {
			t.Errorf("嵌入加载器被调用 %d 次，期望 1（仅临时主题）；"+
				"若为 2 说明系统命中后最终链又解析了一次嵌入", cl.calls)
		}
		if rep.Probe.Embedded != nil && rep.Probe.Embedded.Attempted {
			t.Errorf("系统命中时 ProbeReport.Embedded 不应记录已尝试: %+v", rep.Probe.Embedded)
		}
		t.Logf("异步系统命中：嵌入加载器调用 %d 次（临时主题）；ProbeRan=%v；Scanned=%d",
			cl.calls, rep.Probe.ProbeRan, rep.Probe.Scanned)
	})

	t.Run("对照：系统全失败时最终链必须追加 1 次（证明计数器有判别力）", func(t *testing.T) {
		cl := &countingEmbeddedLoader{err: errUnexpectedEmbeddedLoad}
		p := theme.NewProvider(theme.ProviderOptions{
			Candidates: []string{
				filepath.Join(t.TempDir(), "nope-a.ttc"),
				filepath.Join(t.TempDir(), "nope-b.ttc"),
			},
			Embedded: cl,
		})

		lz, err := theme.NewLazy(theme.LazyOptions{Provider: p})
		if err != nil {
			t.Fatalf("构造 Lazy 失败: %v", err)
		}
		lz.Request()
		// 本子测试期望**失败终态**，因此不能用 requireReady（它只在 Ready 时返回）。
		waitTerminal(t, lz, 10*time.Second)

		rep := lz.Report()
		if rep.State != theme.LoadFailed {
			t.Fatalf("系统与嵌入全失败时应为 failed，实际 %s", rep.State)
		}
		if cl.calls != 2 {
			t.Errorf("嵌入加载器被调用 %d 次，期望 2（临时主题 1 + 最终链兜底 1）；"+
				"若为 1 则说明本计数器观测不到最终链，上面的 calls==1 断言就没有判别力",
				cl.calls)
		}
		t.Logf("对照组（系统全失败）：嵌入加载器调用 %d 次（临时 1 + 最终链 1）；state=%s",
			cl.calls, rep.State)
	})
}
