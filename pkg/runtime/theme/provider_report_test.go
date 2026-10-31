package theme

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"gioui.org/widget/material"
)

// A fresh result and LastReport must have the same ownership boundary as the
// cached result. Otherwise one observer can rewrite another Lazy's diagnostics.
func TestProviderReportOwnership(t *testing.T) {
	for _, route := range []string{"system", "embedded", "failed"} {
		for _, source := range []string{"resolve", "last"} {
			t.Run(route+"/"+source, func(t *testing.T) {
				data := hanziFontBytes(t)
				loader := EmbeddedFontLoader(&countingLoader{data: data})
				if route == "failed" {
					loader = &failingLoader{err: errors.New("embedded unavailable")}
				}
				p := NewProvider(ProviderOptions{
					Candidates: []string{"owned.otf"},
					Timeout:    time.Second,
					Embedded:   loader,
					Read: func(string) ([]byte, error) {
						if route == "system" {
							return data, nil
						}
						return nil, os.ErrNotExist
					},
				})
				id, rep, err := p.ResolveWithReport(FamilyCJK)
				if (err != nil) != (route == "failed") || rep == nil {
					t.Fatalf("fixture failed: id=%q report=%+v err=%v", id, rep, err)
				}
				wantStatus := rep.Candidates[0].Status
				wantErr := rep.Err
				wantHit := 0
				if rep.Coverage != nil {
					wantHit = rep.Coverage.Hit
				}
				wantBytes := 0
				if rep.Embedded != nil {
					wantBytes = rep.Embedded.Bytes
				}
				if source == "last" {
					rep = p.LastReport()
				}
				corruptProviderReport(rep)

				again := p.LastReport()
				if again.Family != FamilyCJK || again.ID != id || !again.ProbeRan || again.Err != wantErr ||
					again.Candidates[0].Path != "owned.otf" || again.Candidates[0].Status != wantStatus {
					t.Fatalf("caller changed the provider's report: %+v", again)
				}
				if again.Coverage != nil && (again.Coverage.Hit != wantHit || len(again.Coverage.Missing) != 0) {
					t.Fatalf("caller changed coverage: %+v", again.Coverage)
				}
				if again.Embedded != nil && again.Embedded.Bytes != wantBytes {
					t.Fatalf("caller changed embedded diagnostics: %+v", again.Embedded)
				}
				if route == "failed" {
					return // A failed probe is not cached; LastReport still owns its history.
				}

				// Exercise the real downstream consumer, including Done's terminal
				// publication. A copied outer struct alone still leaks nested fields.
				lz, err := NewLazy(LazyOptions{Provider: p, Temp: material.NewTheme()})
				if err != nil {
					t.Fatal(err)
				}
				lz.Request()
				awaitDone(t, lz.Done())
				loaded := lz.Report()
				if loaded.State != LoadReady || loaded.Err != nil || loaded.Probe == nil ||
					loaded.Probe.Err != nil || loaded.Probe.ProbeRan || loaded.Probe.ID != id ||
					loaded.Probe.Candidates[0].Path != "owned.otf" || p.Scans() != 1 {
					t.Fatalf("shared Lazy consumed corrupted history: %+v", loaded)
				}
			})
		}
	}
}

func corruptProviderReport(rep *ProbeReport) {
	rep.Family = "changed"
	rep.ID = "changed"
	rep.ProbeRan = false
	rep.Err = errors.New("observer error")
	rep.Candidates[0].Path = "changed"
	rep.Candidates[0].Status = StatusParseFailed
	if rep.Embedded != nil {
		rep.Embedded.Bytes = -1
	}
	if rep.Coverage != nil {
		rep.Coverage.Hit = -1
		rep.Coverage.Missing = append(rep.Coverage.Missing, '中')
	}
}

func TestProviderReportConcurrentLazyObservers(t *testing.T) {
	data := hanziFontBytes(t)
	p := NewProvider(ProviderOptions{
		Candidates: []string{"concurrent.otf"},
		Timeout:    time.Second,
		Read:       func(string) ([]byte, error) { return data, nil },
	})
	if _, _, err := p.ResolveWithReport(FamilyCJK); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	observersDone := make(chan struct{})
	var wg sync.WaitGroup
	var editMu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			<-start
			// Serialize deliberate edits so the aliasing regression does not
			// itself require a data race on the unfixed implementation.
			editMu.Lock()
			corruptProviderReport(p.LastReport())
			editMu.Unlock()
		})
	}
	go func() { wg.Wait(); close(observersDone) }()
	close(start)
	awaitDone(t, observersDone)

	// Readers run concurrently, but use independent temporary themes and Shapers.
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			lz, err := NewLazy(LazyOptions{Provider: p, Temp: material.NewTheme()})
			if err != nil {
				t.Error(err)
				return
			}
			lz.Request()
			awaitDone(t, lz.Done())
			rep := lz.Report()
			if rep.State != LoadReady || rep.Err != nil || rep.Probe.Err != nil ||
				rep.Probe.Candidates[0].Path != "concurrent.otf" || rep.Probe.Coverage.Hit <= 0 {
				t.Errorf("observer changed another Lazy's report: %+v", rep)
			}
		})
	}
	wg.Wait()
	if p.Scans() != 1 {
		t.Fatalf("observers caused %d scans, want 1", p.Scans())
	}
}
