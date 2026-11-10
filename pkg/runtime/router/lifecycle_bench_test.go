package router_test

// R28-C / S3：成本分开计量。
//   - 稳定帧 Layout（无 opt-in / 启用 opt-in）：每帧成本，要求 0 allocs/op；
//   - Mount/Unmount 一次性生命期成本：单独计量，不算进每帧开销；
//   - Post + RunPosted（后台资源收口）：单独计量。
// 微基准只报告绝对 ns/op、B/op、allocs/op 与访问规模，不外推 GPU/冷启动/
// M1/RSS，也不证明整应用 <5%。

import (
	"context"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/lifecycle"
	"github.com/goxml/goxml/pkg/runtime/router"
)

func benchFrame(b *testing.B, optIn bool) {
	b.Helper()
	r := router.New()
	if optIn {
		if err := r.EnableLifecycle(); err != nil {
			b.Fatal(err)
		}
	}
	page := &r28Page{name: "bench"}
	if err := r.Register("/p", func(context.Context) (router.View, error) { return r28Wrap(page), nil }); err != nil {
		b.Fatal(err)
	}
	if err := r.Navigate("/p"); err != nil {
		b.Fatal(err)
	}
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops}
	th := material.NewTheme()
	v := r.View()
	for i := 0; i < 3; i++ { // 预热：首帧含工厂构造/挂载
		gtx.Reset()
		v.Layout(gtx, th)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtx.Reset()
		v.Layout(gtx, th)
	}
}

// BenchmarkR28StableFrameNoOptIn 无 opt-in 的既有 Router 稳定帧。
func BenchmarkR28StableFrameNoOptIn(b *testing.B) { benchFrame(b, false) }

// BenchmarkR28StableFrameOptIn 启用显式生命周期后的稳定帧（无切换/无投递）。
func BenchmarkR28StableFrameOptIn(b *testing.B) { benchFrame(b, true) }

// BenchmarkR28MountUnmountOnce 一次完整生命期（构造 + Mount + Finalize）
// 的一次性成本；刻意与稳定帧分开，不摊到每帧。
func BenchmarkR28MountUnmountOnce(b *testing.B) {
	page := &r28Page{name: "once"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		o := lifecycle.NewOwner(context.Background())
		if _, err := o.Switch(page, nil); err != nil {
			b.Fatal(err)
		}
		o.Finalize()
	}
}

// BenchmarkR28PostRunPosted 一次后台投递 + owner 实际执行时复查的成本。
func BenchmarkR28PostRunPosted(b *testing.B) {
	o := lifecycle.NewOwner(context.Background())
	page := &r28Page{name: "post"}
	h, err := o.Switch(page, nil)
	if err != nil {
		b.Fatal(err)
	}
	noop := func() {}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		o.Post(h, noop)
		o.RunPosted()
	}
	o.Finalize()
}
