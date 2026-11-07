package watch

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestCoordR2RunAndStopConcurrent(t *testing.T) {
	w, e := New(Config{ProjectDir: mkProj(t), PollIntervalMS: 1}, &fakeBuilder{}, &fakeSwapper{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- w.Run(ctx) }()
	for i := 0; i < 10000; i++ {
		w.Stop()
		runtime.Gosched()
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Stop did not terminate Run")
	}
}
