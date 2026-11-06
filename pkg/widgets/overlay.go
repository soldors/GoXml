// Package widgets 提供基于 Gio 的自绘复用组件（TASK-WIDGET-001 基础切片）。
//
// 本包是纯 Go 组件库：直接消费 *material.Theme 与 layout.Context，各实例
// 持有各自状态，无全局活跃菜单/Modal，不读取 ThemeRef，不新增 GXML 标签
// 或语义。本轮（R26-B）交付 ContextMenu 与 Modal 共享的遮罩/弹出层基础；
// DataGrid 属同卡片后续切片，不在本包。
//
// # 层级与事件模型（Gio v0.10.3 立即模式）
//
//   - 命中区在 clip.Rect(...).Push(gtx.Ops) 区域内以 event.Op(gtx.Ops, tag)
//     声明；同帧内后声明者在前（命中测试取靠后声明的区），因此「遮罩先
//     铺满整帧、面板/条目后声明」即可让条目压在遮罩之上，遮罩又压在更早
//     布局的背景之上——背景不再接收指针事件；
//   - 遮罩自身不 grab：Press 与 Release 都落在遮罩区才算一次遮罩点击；
//   - 背景置灰/禁用是调用方的职责（gtx.Disabled()）；本包不越权改写背景，
//     B2 测试以「打开 Modal 后背景按钮零点击」验证遮罩的真实阻断。
//
// # 未定候选行为（单列待裁决，见交付 scope-matrix.md；未批准不入规范）
//
//   - 打开时是否请求键盘焦点：本轮打开即请求（Escape 需要焦点）；
//   - ContextMenu 空菜单：允许打开（渲染零内容空面板，外点/Escape 可关）；
//   - ContextMenu 点击面板空白（内边距/间隙）：不关闭（面板自有吸收区）；
//   - Modal 遮罩点击：不关闭（单次读取 ScrimClicked() 供调用方自行决定）；
//   - Modal Escape：关闭（单次读取 EscapeRequested()），并在关闭后的
//     Layout 帧把焦点交还 ReturnFocusTo（若设置）。
package widgets

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// overlayTag 是遮罩/弹层事件区的分发标签（每实例私有指针，天然唯一；
// owner 仅用于调试区分实例）。
type overlayTag struct {
	owner interface{}
}

// scrimLayout 在当前约束全域铺一层遮罩事件区，并消费本帧指向它的事件。
//
// 遮罩是「先声明的全屏区」：压在更早布局的背景之上（命中测试取靠后声明
// 的区，背景点击被遮罩拦截）；之后布局的面板/条目再声明自己的区，即可
// 压在遮罩之上。
//
// 返回本帧是否发生一次完整的遮罩点击（按下与释放均为左键且都落在遮罩区）。
func scrimLayout(gtx layout.Context, tag *overlayTag) (clicked bool) {
	defer clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)

	var pressed bool
	for {
		ev, ok := gtx.Source.Event(pointer.Filter{
			Target: tag,
			Kinds:  pointer.Press | pointer.Release | pointer.Drag | pointer.Cancel,
		})
		if !ok {
			break
		}
		e, isPtr := ev.(pointer.Event)
		if !isPtr {
			continue
		}
		switch e.Kind {
		case pointer.Press:
			if e.Buttons&pointer.ButtonPrimary != 0 {
				pressed = true
			}
		case pointer.Release:
			if pressed && e.Buttons&pointer.ButtonPrimary != 0 {
				clicked = true
			}
			pressed = false
		case pointer.Cancel:
			pressed = false
		}
	}
	return clicked
}

// absorbArea 在 rect 上声明一个只吸收事件、不产生语义的区（面板空白处，
// 防止面板内边距/间隙的点击落到遮罩上被误判为外点）。
func absorbArea(gtx layout.Context, tag *overlayTag, rect image.Rectangle) {
	defer clip.Rect(rect).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
}

// escapeLayout 请求键盘焦点并消费本帧发往 tag 的 Escape（按下或释放都算）。
// 焦点请求与事件消费同帧完成；「打开帧之后队列 Escape、下一帧收到」的
// 真实路径由 B2 离屏 Router 测试验证。
func escapeLayout(gtx layout.Context, tag *overlayTag) (escape bool) {
	gtx.Execute(key.FocusCmd{Tag: tag})
	for {
		ev, ok := gtx.Source.Event(key.Filter{Focus: tag, Name: key.NameEscape})
		if !ok {
			break
		}
		if _, isKey := ev.(key.Event); isKey {
			escape = true
		}
	}
	return escape
}

// viewportClamp 把 size 大小的弹层锚点 pos 钳制进 viewport：右/下越界向
// 内收、负坐标收到 0，弹层永不越出视口。四边矩阵（B3）以可编译变异守护
// 本函数不被静默破坏。
func viewportClamp(pos, size, viewport image.Point) image.Point {
	clamped := pos
	if clamped.X+size.X > viewport.X {
		clamped.X = viewport.X - size.X
	}
	if clamped.Y+size.Y > viewport.Y {
		clamped.Y = viewport.Y - size.Y
	}
	if clamped.X < 0 {
		clamped.X = 0
	}
	if clamped.Y < 0 {
		clamped.Y = 0
	}
	return clamped
}
