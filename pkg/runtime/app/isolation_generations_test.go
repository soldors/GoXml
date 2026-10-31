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

// R2 切片2：失败/重试后的**实例隔离**与**代次隔离**。
// 只用受控 Provider / barrier / 假 Done；不调用原生窗口。
//
// 契约边界（记录，不新增 API）：
//   - 现有「重试」= 失败终态后再调用 Lazy.Request()；**无独立取消 API**（本测试不发明）。
//   - 每次新尝试必须得到**新的 Done 通道**（旧快照保持关闭），且终态字段不得沿用上一轮。
//   - 两个独立 Provider/Lazy/App 之间，状态/错误/通知/唤醒不得互相污染。

func readFont(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "theme", "embedded", "GoxmlSansSC-Regular.otf"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 两个独立实例：A 失败不得污染 B（状态/错误/通知/唤醒）。
func TestFailedInstanceDoesNotPolluteAnother(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pa, ga := newGatedProvider(t, true)  // A 将失败
		pb, gb := newGatedProvider(t, false) // B 将成功
		lzA, err := theme.NewLazy(theme.LazyOptions{Provider: pa, Temp: material.NewTheme()})
		if err != nil {
			t.Fatal(err)
		}
		lzB, err := theme.NewLazy(theme.LazyOptions{Provider: pb, Temp: material.NewTheme()})
		if err != nil {
			t.Fatal(err)
		}
		lzA.Request()
		lzB.Request()
		waitEntered(t, ga)
		waitEntered(t, gb)

		appA := &App{lz: lzA}
		appB := &App{lz: lzB}
		wakeA := armWaiter(appA)
		wakeB := armWaiter(appB)
		t.Cleanup(appA.stopLateCompletion)
		t.Cleanup(appB.stopLateCompletion)

		// A 先完成且失败。
		ga.release()
		select {
		case <-wakeA:
		case <-time.After(5 * time.Second):
			t.Fatal("A: failed terminal did not wake")
		}
		if lzA.State() != theme.LoadFailed || lzA.Report().Err == nil {
			t.Fatalf("A should be Failed with error, got %v err=%v", lzA.State(), lzA.Report().Err)
		}
		// A 的生产者已结束；B 唯一未释放的输入是自己的 gate。
		synctest.Wait()
		// B 仍未完成，且未被 A 的失败影响。
		if lzB.State() != theme.LoadLoading {
			t.Fatalf("B polluted by A's failure: state=%v", lzB.State())
		}
		select {
		case <-wakeB:
			t.Fatal("B woke due to A's completion (cross-instance wake)")
		default:
		}

		// B 再成功，A 的失败态不被 B 的成功改变。
		gb.release()
		select {
		case <-wakeB:
		case <-time.After(5 * time.Second):
			t.Fatal("B: success terminal did not wake")
		}
		if lzB.State() != theme.LoadReady || lzB.Report().Err != nil {
			t.Fatalf("B should be Ready no error, got %v err=%v", lzB.State(), lzB.Report().Err)
		}
		if lzA.State() != theme.LoadFailed || lzA.Report().Err == nil {
			t.Fatalf("A's failed state polluted by B's success: %v err=%v", lzA.State(), lzA.Report().Err)
		}
		// Report 互相独立：改 A 的报告副本不影响 B。
		ra := lzA.Report()
		ra.Err = nil
		if lzA.Report().Err == nil {
			t.Fatal("mutating a report copy changed the instance's own report")
		}
	})
}

// 代次隔离：失败→Request 重试 必须得到新 Done 通道；旧通道保持关闭；终态字段不沿用上一轮；notify 每个终态各一次。
func TestRetryGenerationIsolation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		font := readFont(t)
		gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
		var rel [2]sync.Once
		for i := range gates {
			i := i
			t.Cleanup(func() { rel[i].Do(func() { close(gates[i]) }) })
		}
		entered := make(chan int, 2)
		reads := 0
		p := theme.NewProvider(theme.ProviderOptions{
			Candidates: []string{"gen.otf"},
			Embedded:   lateNotifyUnavailableFont{},
			Read: func(string) ([]byte, error) {
				i := reads
				reads++
				entered <- i
				<-gates[i]
				if i == 0 {
					return nil, os.ErrNotExist
				}
				return font, nil
			},
		})
		lz, err := theme.NewLazy(theme.LazyOptions{Provider: p, Temp: material.NewTheme()})
		if err != nil {
			t.Fatal(err)
		}
		fired := make(chan theme.LoadState, 4)
		if !lz.SetNotify(func() { fired <- lz.State() }) {
			t.Fatal("SetNotify must succeed before Request")
		}

		// 代次 1：失败。
		lz.Request()
		<-entered
		ch1 := lz.Done()
		rel[0].Do(func() { close(gates[0]) })
		select {
		case st := <-fired:
			if st != theme.LoadFailed {
				t.Fatalf("gen1 notify state=%v want Failed", st)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("gen1 did not fire notify")
		}
		select {
		case <-ch1:
		default:
			t.Fatal("gen1 Done channel not closed at terminal")
		}
		if lz.Report().Attempts != 1 || !errors.Is(lz.Report().Err, contracts.ErrFontNotFound) {
			t.Fatalf("gen1 report wrong: attempts=%d err=%v", lz.Report().Attempts, lz.Report().Err)
		}

		// 第一代生产者完全推进后，下一代尚未启动；此时不允许第二次通知。
		synctest.Wait()
		select {
		case <-fired:
			t.Fatal("gen1 notify fired more than once")
		default:
		}
		// 代次 2：重试成功（Request after Failed）。
		lz.Request()
		<-entered
		ch2 := lz.Done()
		if ch2 == ch1 {
			t.Fatal("retry reused the closed Done channel (generation not isolated)")
		}
		select {
		case <-ch2:
			t.Fatal("gen2 Done closed before its terminal (leaked gen1 closure)")
		default:
		}
		select {
		case <-ch1:
			// 旧代次通道保持关闭
		default:
			t.Fatal("gen1 snapshot channel reopened on retry")
		}
		rel[1].Do(func() { close(gates[1]) })
		select {
		case st := <-fired:
			if st != theme.LoadReady {
				t.Fatalf("gen2 notify state=%v want Ready", st)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("gen2 did not fire notify")
		}
		select {
		case <-ch2:
		default:
			t.Fatal("gen2 Done not closed at terminal")
		}
		if lz.State() != theme.LoadReady || lz.Report().Attempts != 2 {
			t.Fatalf("gen2 report wrong: state=%v err=%v attempts=%d", lz.State(), lz.Report().Err, lz.Report().Attempts)
		}
		// 失败错误不得残留到成功代次。
		if err := lz.Report().Err; err != nil {
			t.Fatal("stale failure error leaked into successful generation")
		}
		// 两代 gate 都已释放，等待生产者尾部结束后检查额外通知。
		synctest.Wait()
		// notify 恰好两次（每个终态一次）。
		select {
		case <-fired:
			t.Fatal("notify fired more than once per terminal")
		default:
		}
	})
}
