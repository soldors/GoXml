package app

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

// 本文件补强「后台终态通知 与 事件泵退出」的交错语义（R2 切片1）。
// 只用受控 Provider / barrier / 假 Done 通道；不调用原生 Window.Event、不起真实窗口。
//
// 契约要点（复用之，不另造通知机制）：
//   - 绑定了 notify 时，终态（Ready 或 Failed）在后台 goroutine 内**恰好触发一次**；
//   - SetNotify 失败（晚绑定）时由 App 用 Done 补偿（watchLateCompletion）；
//   - 事件泵先退出（stopLateCompletion 关闭 stop）后，等待者必须退出且**不得**再唤醒已结束的事件泵；
//   - 失败不能被"只唤醒成功"吞掉：Failed 同样要唤醒。

// gatedProvider 的 Read 在放行前阻塞，用于精确控制"完成 vs 退出"的先后。
type gatedProvider struct {
	entered chan struct{}
	gate    chan struct{}
	once    sync.Once
	font    []byte
	fail    bool
	reads   int
}

func newGatedProvider(t *testing.T, fail bool) (*theme.Provider, *gatedProvider) {
	t.Helper()
	font, err := os.ReadFile(filepath.Join("..", "theme", "embedded", "GoxmlSansSC-Regular.otf"))
	if err != nil {
		t.Fatal(err)
	}
	g := &gatedProvider{entered: make(chan struct{}), gate: make(chan struct{}), font: font, fail: fail}
	var once sync.Once
	p := theme.NewProvider(theme.ProviderOptions{
		Candidates: []string{"gated.otf"},
		Embedded:   lateNotifyUnavailableFont{},
		Read: func(string) ([]byte, error) {
			once.Do(func() { close(g.entered) })
			<-g.gate
			g.reads++
			if g.fail {
				return nil, os.ErrNotExist
			}
			return g.font, nil
		},
	})
	t.Cleanup(func() { g.release() })
	return p, g
}

func (g *gatedProvider) release() { g.once.Do(func() { close(g.gate) }) }

func waitEntered(t *testing.T, g *gatedProvider) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not enter controlled Read")
	}
}

// armWaiter 用真实的 watchLateCompletion 装配一个 Done 等待者（模拟事件泵已跑过一帧）。
func armWaiter(a *App) chan struct{} {
	wake := make(chan struct{}, 4)
	a.lateWake = func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	a.lateStop = make(chan struct{})
	a.watchLateCompletion()
	return wake
}

// 切片1-A：完成先于退出 → 唤醒恰好一次；随后退出幂等。
func TestLateWakeDeliveredOnceWhenCompletionBeforeExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, g := newGatedProvider(t, false)
		lz, err := theme.NewLazy(theme.LazyOptions{Provider: p, Temp: material.NewTheme()})
		if err != nil {
			t.Fatal(err)
		}
		lz.Request()
		waitEntered(t, g)

		a := &App{lz: lz}
		wake := armWaiter(a)
		t.Cleanup(a.stopLateCompletion)

		g.release() // 完成（早于退出）
		select {
		case <-wake:
		case <-time.After(5 * time.Second):
			t.Fatal("completion before exit did not deliver a wake")
		}
		// 等待 bubble 内生产者结束；终态之后本路径没有未释放的 barrier。
		synctest.Wait()
		select {
		case <-wake:
			t.Fatal("terminal wake delivered more than once")
		default:
		}
		if lz.State() != theme.LoadReady {
			t.Fatalf("want Ready, got %v", lz.State())
		}
		a.stopLateCompletion()
		a.stopLateCompletion() // 幂等
	})
}

// 切片1-B：退出先于完成 → 等待者不得再唤醒已结束的事件泵；后台加载仍独立完成。
func TestLateWakeSuppressedWhenExitBeforeCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, g := newGatedProvider(t, false)
		lz, err := theme.NewLazy(theme.LazyOptions{Provider: p, Temp: material.NewTheme()})
		if err != nil {
			t.Fatal(err)
		}
		lz.Request()
		waitEntered(t, g)

		a := &App{lz: lz}
		wake := armWaiter(a)

		a.stopLateCompletion() // 事件泵先退出：关闭 stop
		g.release()            // 之后才完成

		// 终态确实到达（后台不经通知也在推进）。
		select {
		case <-lz.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("background load did not reach terminal after exit")
		}
		if lz.State() != theme.LoadReady {
			t.Fatalf("want Ready after exit, got %v", lz.State())
		}
		// gate 与 stop 均已释放；Wait 后 watcher 不再有可运行的尾部。
		synctest.Wait()
		select {
		case <-wake:
			t.Fatal("wake delivered to an already-exited event pump")
		default:
		}
	})
}

// 切片1-C：失败终态同样触发 notify（错误不被"只唤醒成功"吞掉）。
func TestNotifyFiresOnFailedTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p, g := newGatedProvider(t, true)
		lz, err := theme.NewLazy(theme.LazyOptions{Provider: p, Temp: material.NewTheme()})
		if err != nil {
			t.Fatal(err)
		}
		fired := make(chan theme.LoadState, 4)
		if !lz.SetNotify(func() { fired <- lz.State() }) {
			t.Fatal("SetNotify should succeed before Request")
		}
		lz.Request()
		waitEntered(t, g)
		g.release()

		select {
		case st := <-fired:
			if st != theme.LoadFailed {
				t.Fatalf("notify fired with state %v, want LoadFailed", st)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Failed terminal did not fire notify (error swallowed by success-only wake)")
		}
		if lz.Report().Err == nil || !errors.Is(lz.Report().Err, contracts.ErrFontNotFound) {
			t.Fatalf("failed terminal did not retain ErrFontNotFound: %v", lz.Report().Err)
		}
		synctest.Wait()
		select {
		case <-fired:
			t.Fatal("notify fired more than once for a single terminal")
		default:
		}
	})
}

// 切片1-D：fontFailure 的三条边界（严格/非严格/在途/终态）。
func TestFontFailureSemantics(t *testing.T) {
	// 同步路径（lz==nil）恒 nil，即使 strict。
	if got := (&App{strict: true}).fontFailure(); got != nil {
		t.Fatalf("nil lazy must return nil, got %v", got)
	}

	// 在途（Loading）：严格模式也返回 nil（契约 §1：在途返回 nil）。
	pg, gg := newGatedProvider(t, true)
	lzLoading, err := theme.NewLazy(theme.LazyOptions{Provider: pg, Temp: material.NewTheme()})
	if err != nil {
		t.Fatal(err)
	}
	lzLoading.Request()
	waitEntered(t, gg)
	if got := (&App{lz: lzLoading, strict: true}).fontFailure(); got != nil {
		t.Fatalf("in-flight strict must return nil, got %v", got)
	}
	gg.release()
	select {
	case <-lzLoading.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("loading case did not finish")
	}

	// 失败终态：严格→ErrFontNotFound；非严格→nil。
	pf, gf := newGatedProvider(t, true)
	lzFailed, err := theme.NewLazy(theme.LazyOptions{Provider: pf, Temp: material.NewTheme()})
	if err != nil {
		t.Fatal(err)
	}
	lzFailed.Request()
	waitEntered(t, gf)
	gf.release()
	select {
	case <-lzFailed.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("failed case did not finish")
	}
	if lzFailed.State() != theme.LoadFailed {
		t.Fatalf("want Failed, got %v", lzFailed.State())
	}
	if got := (&App{lz: lzFailed, strict: true}).fontFailure(); !errors.Is(got, contracts.ErrFontNotFound) {
		t.Fatalf("strict+Failed must surface ErrFontNotFound, got %v", got)
	}
	if got := (&App{lz: lzFailed, strict: false}).fontFailure(); got != nil {
		t.Fatalf("non-strict must return nil, got %v", got)
	}

	// 成功终态：严格→nil。
	ps, gs := newGatedProvider(t, false)
	lzReady, err := theme.NewLazy(theme.LazyOptions{Provider: ps, Temp: material.NewTheme()})
	if err != nil {
		t.Fatal(err)
	}
	lzReady.Request()
	waitEntered(t, gs)
	gs.release()
	select {
	case <-lzReady.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("ready case did not finish")
	}
	if lzReady.State() != theme.LoadReady {
		t.Fatalf("want Ready, got %v", lzReady.State())
	}
	if got := (&App{lz: lzReady, strict: true}).fontFailure(); got != nil {
		t.Fatalf("strict+Ready must return nil, got %v", got)
	}
}
