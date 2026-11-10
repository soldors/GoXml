package router_test

// R28-C / S3：并发族 race 守卫。
// 挂载/卸载发生在 UI owner（主 goroutine 的帧）；外部 Shutdown、导航请求、
// 后台任务完成后的 owner 投递均来自其它 goroutine。全部用通道 + WaitGroup
// 同步与有界截止，不用 sleep 猜顺序，测试自身 goroutine 必须 join。

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/router"
)

func TestR28ConcurrentShutdownPostAndOwnerFrames(t *testing.T) {
	r := router.New()
	if err := r.EnableLifecycle(); err != nil {
		t.Fatal(err)
	}
	o, err := r.LifecycleOwner()
	if err != nil {
		t.Fatal(err)
	}
	pa, pb := &r28Page{name: "a"}, &r28Page{name: "b"}
	if err := r.Register("/a", func(context.Context) (router.View, error) { return r28Wrap(pa), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", func(context.Context) (router.View, error) { return r28Wrap(pb), nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	r28Frame(r.View()) // 基线挂载

	const workers = 6
	const perWorker = 40
	var wg sync.WaitGroup
	var applied atomic.Int64
	begin := make(chan struct{})
	deadline := time.After(3 * time.Second)
	frameStop := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-begin
			for j := 0; j < perWorker; j++ {
				switch (id + j) % 3 {
				case 0:
					path := "/a"
					if j%2 == 0 {
						path = "/b"
					}
					_ = r.Navigate(path)
				case 1:
					r.LifecyclePost(func() { applied.Add(1) })
				case 2:
					_ = r.Current()
					_ = r.NavError()
				}
			}
		}(i)
	}

	// UI owner：唯一驱动 Layout 的 goroutine。
	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)
		ops := new(op.Ops)
		gtx := layout.Context{Ops: ops}
		th := material.NewTheme()
		v := r.View()
		for {
			select {
			case <-frameStop:
				return
			default:
				gtx.Reset()
				v.Layout(gtx, th)
			}
		}
	}()

	// 后台 Shutdown 与导航/投递并发，但只触发一次。
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-begin
		r.Shutdown()
	}()

	close(begin)
	joined := make(chan struct{})
	go func() { wg.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-deadline:
		close(frameStop)
		<-ownerDone
		t.Fatal("后台 goroutine 未在 3 秒内收口")
	}
	close(frameStop)
	select {
	case <-ownerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("UI owner 帧循环未收口")
	}
	// owner 最终化在 UI owner 上完成，且只清理一次。
	r.LifecycleFinalize()
	r.LifecycleFinalize()
	if !o.CleanupDone() {
		t.Fatal("并发后 owner 未完成清理")
	}
	ma, ua, _ := pa.counts()
	mb, ub, _ := pb.counts()
	if ua > ma || ub > mb {
		t.Fatalf("并发下出现重复卸载: a=%d/%d b=%d/%d", ma, ua, mb, ub)
	}
	if snap := o.Snapshot(); snap.Applied < 0 || snap.Rejected < 0 {
		t.Fatalf("计数异常: %+v", snap)
	}
	t.Logf("OBS race: applied=%d snap=%+v", applied.Load(), o.Snapshot())
}

// TestR28ConcurrentTaskCompletionVersusCancel 后台任务完成与 owner 取消的
// 竞态：用两次确定性屏障覆盖「投递先于取消」「取消先于投递」两种交错，
// 后台 goroutine 一律 join，最后再驱动一帧复查。
func TestR28ConcurrentTaskCompletionVersusCancel(t *testing.T) {
	for _, cancelFirst := range []bool{false, true} {
		name := "post-first"
		if cancelFirst {
			name = "cancel-first"
		}
		t.Run(name, func(t *testing.T) {
			r := router.New()
			if err := r.EnableLifecycle(); err != nil {
				t.Fatal(err)
			}
			o, err := r.LifecycleOwner()
			if err != nil {
				t.Fatal(err)
			}
			page := &r28Page{name: "race"}
			if err := r.Register("/p", func(context.Context) (router.View, error) { return r28Wrap(page), nil }); err != nil {
				t.Fatal(err)
			}
			if err := r.Navigate("/p"); err != nil {
				t.Fatal(err)
			}
			r28Frame(r.View())

			var applied atomic.Int64
			post := func() {
				for i := 0; i < 8; i++ {
					r.LifecyclePost(func() { applied.Add(1) })
				}
			}
			join := func(fn func()) {
				done := make(chan struct{})
				go func() { defer close(done); fn() }()
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Fatal("后台 goroutine 未收口")
				}
			}
			if cancelFirst {
				join(r.Shutdown)
				join(post)
			} else {
				join(post)
				join(r.Shutdown)
			}
			r.LifecycleFinalize()
			r28Frame(r.View())
			if applied.Load() != 0 {
				t.Fatalf("取消后排队的后台结果被应用: %d", applied.Load())
			}
			if snap := o.Snapshot(); snap.Applied != 0 {
				t.Fatalf("owner 应用了迟到结果: %+v", snap)
			}
		})
	}
}
