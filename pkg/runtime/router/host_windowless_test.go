package router_test

import (
	"context"
	"errors"
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/app"
	"github.com/goxml/goxml/pkg/runtime/router"
)

type windowlessPage struct {
	layouts int
	selfCtx context.Context
}

func (p *windowlessPage) Layout(layout.Context, *material.Theme) layout.Dimensions {
	p.layouts++
	return layout.Dimensions{}
}

func (p *windowlessPage) factory(ctx context.Context) (router.View, error) {
	p.selfCtx = ctx
	return p, nil
}

// TestR16HostCompositionWindowless 宿主组合（无窗口）：
// app.NewApp 接受 Router 根 View（平台窗口在首次 w.Event() 才创建——S0 证据，
// 构造零 OS 资源）；*App 作为 Invalidator 接线；随后以手动帧驱动验证
// 导航、Layout 与失效接线的端到端组合。真实窗口视觉与 DestroyEvent 生产
// 接线属原生时序，本测试明确不覆盖。
func TestR16HostCompositionWindowless(t *testing.T) {
	r := router.New()
	pa, pb := &windowlessPage{}, &windowlessPage{}
	if err := r.Register("/a", pa.factory); err != nil {
		t.Fatal(err)
	}
	if err := r.Register("/b", pb.factory); err != nil {
		t.Fatal(err)
	}

	a, err := app.NewApp(r.View(), material.NewTheme(), app.Config{})
	if err != nil {
		t.Fatalf("宿主应接受 Router 根 View: %v", err)
	}
	if err := r.SetInvalidator(a); err != nil {
		t.Fatalf("*App 应满足 Invalidator 接线: %v", err)
	}
	if err := r.SetInvalidator(a); err == nil {
		t.Fatal("二次接线应被拒绝（防运行中换窗）")
	}

	if err := r.Navigate("/a"); err != nil {
		t.Fatal(err)
	}
	// 手动帧驱动（无窗口宿主）：与 App.frame 相同的渲染入口契约。
	gtx := &layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(320, 240))}
	gtx.Reset()
	r.View().Layout(*gtx, material.NewTheme())
	if got := r.Current(); got != "/a" {
		t.Fatalf("宿主组合下导航未生效: %q", got)
	}
	if pa.layouts != 1 {
		t.Fatalf("当前页应渲染 1 帧，实际 %d", pa.layouts)
	}
	if err := r.Navigate("/b"); err != nil {
		t.Fatal(err)
	}
	gtx.Reset()
	r.View().Layout(*gtx, material.NewTheme())
	if got := r.Current(); got != "/b" || pb.layouts != 1 {
		t.Fatalf("组合下切换失败: Current=%q pb=%d", got, pb.layouts)
	}
	if err := pa.selfCtx.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("组合下被替换页 Context 应取消: %v", err)
	}
}
