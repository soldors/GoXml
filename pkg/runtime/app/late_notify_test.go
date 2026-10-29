package app

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/widget/material"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/pkg/runtime/theme"
)

type lateNotifyUnavailableFont struct{}

func (lateNotifyUnavailableFont) Load() ([]byte, string, error) {
	return nil, "", os.ErrNotExist
}

// A controlled event pump: only a real production completion wake permits the
// second frame. No polling, timer-driven frames or native window is involved.
func TestLateNotifyIdleCompletion(t *testing.T) {
	for _, prebound := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-prior-notify", true: "preserve-prior-notify"}[prebound], func(t *testing.T) {
			testLateNotifyIdleCompletion(t, prebound)
		})
	}
}

func TestLateNotifyFailedRetry(t *testing.T) {
	font, err := os.ReadFile(filepath.Join("..", "theme", "embedded", "GoxmlSansSC-Regular.otf"))
	if err != nil {
		t.Fatal(err)
	}
	gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
	var releases [2]sync.Once
	for i := range gates {
		t.Cleanup(func() { releases[i].Do(func() { close(gates[i]) }) })
	}
	entered := make(chan int, 2)
	reads := 0 // only the serial workers access this; terminal publication orders retries
	p := theme.NewProvider(theme.ProviderOptions{
		Candidates: []string{"retry.otf"}, Embedded: lateNotifyUnavailableFont{},
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
	lz.Request()
	waitRead := func(want int) {
		t.Helper()
		select {
		case got := <-entered:
			if got != want {
				t.Fatalf("read attempt %d, want %d", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not enter read")
		}
	}
	waitRead(0)
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{StrictFontFailure: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopLateCompletion)
	wakes := make(chan struct{}, 2)
	a.lateWake = func() { wakes <- struct{}{} }
	temp := a.th.Shaper
	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 1 {
			lz.Request()
			waitRead(1)
		}
		driveFrames(a, 1) // arm the current attempt before allowing completion
		releases[attempt].Do(func() { close(gates[attempt]) })
		select {
		case <-wakes:
		case <-time.After(5 * time.Second):
			t.Fatal("terminal wake missing")
		}
		if attempt == 0 {
			if lz.State() != theme.LoadFailed || !errors.Is(a.fontFailure(), contracts.ErrFontNotFound) || lz.Report().Err == nil {
				t.Fatal("failed attempt/strict error lost")
			}
			driveFrames(a, 1)
			if a.th.Shaper != temp || lz.Report().Attempts != 1 {
				t.Fatal("failure must preserve the temporary font without retry")
			}
		} else {
			driveFrames(a, 2)
			if lz.State() != theme.LoadReady || a.fontFailure() != nil || a.th.Shaper == temp || shaperChanges(v.shapers) != 1 {
				t.Fatal("explicit retry failed to wake and swap once")
			}
		}
	}
}

func TestLateNotifyAlreadyReady(t *testing.T) {
	lz := newFaceSelLazy(t, faceSelOK())
	lz.Request()
	requireReady(t, lz)
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.stopLateCompletion)
	a.lateWake = func() {} // no native window; first frame must consume pending
	temp := a.th.Shaper
	driveFrames(a, 2)
	if a.th.Shaper == temp || lz.Report().FrameSubmittedAt.IsZero() || shaperChanges(shaperSeq(temp, v.shapers)) != 1 {
		t.Fatal("precompleted attempt did not apply exactly once")
	}
	a.stopLateCompletion()
	a.stopLateCompletion() // event-loop cleanup is idempotent
}

func testLateNotifyIdleCompletion(t *testing.T, prebound bool) {
	t.Helper()
	gate := make(chan struct{})
	entered := make(chan struct{})
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate) }) }
	t.Cleanup(unblock)
	font, err := os.ReadFile(filepath.Join("..", "theme", "embedded", "GoxmlSansSC-Regular.otf"))
	if err != nil {
		t.Fatal(err)
	}
	p := theme.NewProvider(theme.ProviderOptions{
		Candidates: []string{"controlled.otf"},
		Read: func(string) ([]byte, error) {
			close(entered)
			<-gate
			return font, nil
		},
	})
	lz, err := theme.NewLazy(theme.LazyOptions{Provider: p, Temp: material.NewTheme()})
	if err != nil {
		t.Fatal(err)
	}
	prior := make(chan theme.LoadReport, 1)
	if prebound && !lz.SetNotify(func() { prior <- lz.Report() }) {
		t.Fatal("initial notify binding failed")
	}
	lz.Request() // Deliberately before NewLazyApp: its SetNotify must fail.
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not enter controlled Read")
	}
	v := &lazyProbeView{}
	a, err := NewLazyApp(v, lz, Config{})
	if err != nil {
		t.Fatal(err)
	}
	v.owner = a
	wake := make(chan struct{}, 2)
	if a.lateWake == nil {
		t.Fatal("late binding fallback not configured")
	}
	a.lateWake = func() { wake <- struct{}{} }
	t.Cleanup(a.stopLateCompletion)
	temp := a.th.Shaper
	driveFrames(a, 1)
	if lz.State() != theme.LoadLoading || v.shapers[0] != temp {
		t.Fatal("first Layout must precede completion and use temporary font")
	}
	unblock()
	select {
	case <-wake:
	case <-time.After(5 * time.Second):
		t.Fatal("idle event pump received no terminal wake after early Request")
	}
	if lz.State() != theme.LoadReady || a.th.Shaper != temp {
		t.Fatal("completion must publish Ready without mutating the rendered Shaper")
	}
	driveFrames(a, 2)
	if shaperChanges(v.shapers) != 1 || a.th.Shaper == temp || lz.Report().FrameSubmittedAt.IsZero() {
		t.Fatal("wake must lead to exactly one frame-internal swap and submission")
	}
	for _, in := range v.inFrames {
		if !in {
			t.Fatal("Layout escaped FrameEvent")
		}
	}
	if prebound {
		select {
		case rep := <-prior:
			if rep.State != theme.LoadReady {
				t.Fatal("prior notify did not observe Ready")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("prior notify was replaced or lost")
		}
	}
	if n := testing.AllocsPerRun(100, a.watchLateCompletion); n != 0 {
		t.Fatalf("stable completion observer allocates per frame: %g", n)
	}
}
