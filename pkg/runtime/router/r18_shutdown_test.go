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
	defer r.shutdown()

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
				r.shutdown()
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
			r.shutdown()
			if r.DroppedCount() != 1 {
				t.Fatal("repeated shutdown must preserve the drop count")
			}
		})
	}
}
