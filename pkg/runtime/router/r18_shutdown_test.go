package router

import (
	"context"
	"errors"
	"testing"
)

type navigationCommitContext struct {
	context.Context
	onErr func()
}

func (ctx navigationCommitContext) Err() error {
	ctx.onErr()
	return ctx.Context.Err()
}

func TestR18ShutdownAndCommitShareLock(t *testing.T) {
	r := New()
	cancel := r.rootCancel
	r.rootCancel = func() {
		if r.mu.TryLock() {
			r.mu.Unlock()
			t.Error("explicit shutdown must cancel under the navigation state lock")
		}
		cancel()
	}
	defer r.Shutdown()

	checked := false
	if err := r.Register("/page", func(context.Context) (View, error) {
		r.rootCtx = navigationCommitContext{r.rootCtx, func() {
			checked = true
			if r.mu.TryLock() {
				r.mu.Unlock()
				t.Error("the final cancellation check must hold the navigation state lock")
			}
		}}
		return &recPage{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/page"); err != nil {
		t.Fatal(err)
	}
	r.applyNavigation()
	if !checked || r.Current() != "/page" {
		t.Fatalf("expected the final check and navigation commit, checked=%v current=%q", checked, r.Current())
	}
}

func TestR18ShutdownDuringFactoryRejectsCommit(t *testing.T) {
	for _, factoryErr := range []error{nil, errors.New("factory failed")} {
		name := "factory-success"
		if factoryErr != nil {
			name = "factory-error"
		}
		t.Run(name, func(t *testing.T) {
			r := New()
			old := &recPage{}
			if err := r.Register("/old", factoryOf(old)); err != nil {
				t.Fatal(err)
			}
			if err := r.Navigate("/old"); err != nil {
				t.Fatal(err)
			}
			r.applyNavigation()

			var newCtx context.Context
			if err := r.Register("/new", func(ctx context.Context) (View, error) {
				newCtx = ctx
				r.Shutdown()
				return &recPage{}, factoryErr
			}); err != nil {
				t.Fatal(err)
			}
			if err := r.Navigate("/new"); err != nil {
				t.Fatal(err)
			}
			r.applyNavigation()
			if r.Current() != "/old" || r.DroppedCount() != 1 || r.NavError() != nil {
				t.Fatalf("closed navigation current=%q dropped=%d error=%v",
					r.Current(), r.DroppedCount(), r.NavError())
			}
			if newCtx == nil || !errors.Is(newCtx.Err(), context.Canceled) ||
				!errors.Is(old.selfCtx.Err(), context.Canceled) {
				t.Fatal("shutdown must cancel both committed and in-flight page contexts")
			}
			r.Shutdown()
			if r.DroppedCount() != 1 {
				t.Fatal("repeated shutdown must preserve the drop count")
			}
		})
	}
}

func TestShutdownDropsPendingWithoutAnotherFrame(t *testing.T) {
	r := New()
	if err := r.Register("/pending", func(context.Context) (View, error) {
		t.Fatal("closed Router built a pending page")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/pending"); err != nil {
		t.Fatal(err)
	}
	r.Dispatch(func() { t.Fatal("closed Router ran a pending callback") })

	r.Shutdown()
	if got := r.DroppedCount(); got != 2 {
		t.Fatalf("shutdown without another frame: dropped=%d, want 2", got)
	}
	r.Shutdown()
	r.drain()
	if got := r.DroppedCount(); got != 2 {
		t.Fatalf("repeated shutdown double-counted: dropped=%d", got)
	}
}

func TestShutdownDuringDispatchDropsRestOfBatch(t *testing.T) {
	r := New()
	r.Dispatch(r.Shutdown)
	r.Dispatch(func() { t.Fatal("shutdown did not stop the rest of the dispatch batch") })
	r.drain()
	if got := r.DroppedCount(); got != 1 {
		t.Fatalf("rest of batch: dropped=%d, want 1", got)
	}
}
