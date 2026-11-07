// Package widgets 提供基于 Gio 的自绘复用组件（TASK-WIDGET-001 基础切片）。
//
// 本包是纯 Go 组件库：直接消费 *material.Theme 与 layout.Context，各实例
// 持有各自状态，无全局活跃菜单/Modal，不读取 ThemeRef，不新增 GXML 标签
// 或语义。R26-B（含返修 R1）交付 ContextMenu 与 Modal 共享的遮罩/弹出层
// 基础；DataGrid 属同卡片后续切片，不在本包。
//
// # 层级与事件模型（Gio v0.10.3 立即模式）
//
//   - 命中区在 clip.Rect(...).Push(gtx.Ops) 区域内以 event.Op(gtx.Ops, tag)
//     声明；同帧内后声明者在前（命中测试取靠后声明的区），因此「遮罩先
//     铺满整帧、面板/条目后声明」即可让条目压在遮罩之上，遮罩又压在更早
//     布局的背景之上——背景不再接收指针事件；
//   - 遮罩按压状态跨帧保存（真实输入中 Press 与 Release 分属两帧；Windows
//     宿主在主键释放后生成 Buttons=0 的 Release 事件）：Release 不检查
//     Buttons，仅要求按压确属本遮罩且指针身份一致（对齐 gesture.Click）；
//   - 组件对调用方 ops 的影响全部作用域配对（clip/transform Push-Pop），
//     不向后续同级布局泄漏变换；
//   - 背景置灰/禁用是调用方的职责（gtx.Disabled()）；本包不越权改写背景，
//     B2 测试以「打开 Modal 后背景按钮零点击」验证遮罩的真实阻断。
//
// # 焦点模型
//
//   - 打开转换时请求一次焦点（OpenAt/Open 的显式转换），此后不再每帧强抢：
//     内容（如 Editor）取得的焦点保持不动；
//   - Escape 走约定路径：打开期间注册**不带 Focus 约束**的 Escape 过滤器
//     （keyFilterMatch 对 Focus==nil 不做焦点比对），无论焦点在遮罩还是
//     内容都可达——不依赖每帧抢焦点；
//   - 焦点持有资格三件套：FocusCmd 请求 + key.FocusFilter 授予 focusable
//   - clip 内 event.Op 输入区授予 visible（缺一帧末焦点被摘除）；
//   - Modal 关闭后在下一帧 Layout 把焦点交还 ReturnFocusTo（若设置）。
//
// # 未定候选行为（单列待裁决，见交付 scope-matrix.md；未批准不入规范）
//
//   - ContextMenu 空菜单：允许打开（渲染零内容空面板，外点/Escape 可关）；
//   - ContextMenu 点击面板空白（内边距/间隙）：不关闭（面板自有吸收区）；
//   - Modal 遮罩点击：不关闭（单次读取 ScrimClicked() 供调用方自行决定）；
//   - Modal Escape：关闭（单次读取 EscapeRequested()），焦点回交
//     ReturnFocusTo（若设置）；
//   - 高菜单：高度封顶到视口，超出部分滚轮滚动可达（无滚动条视觉，
//     滚动方向沿用事件 Scroll.Y 约定，原生平台方向未验证）。
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

// scrimState 保存遮罩的跨帧按压状态。真实输入中 Press 与 Release 分属
// 两帧，且 Release 事件的 Buttons 是释放后的剩余按钮（Windows 宿主在
// 主键释放后生成 Buttons=0 的 Release）——按压判定不能是帧局部变量，
// 释放判定不能要求 Buttons 仍含 Primary（R1 修复 F-B1）。
type scrimState struct {
	pressed bool
	pid     pointer.ID
}

// layout 铺一层当前约束全域的遮罩事件区，并消费本帧指向它的事件。
//
// 遮罩是「先声明的全屏区」：压在更早布局的背景之上（命中测试取靠后声明
// 的区，背景点击被遮罩拦截）；之后布局的面板/条目再声明自己的区，即可
// 压在遮罩之上。
//
// 返回本帧是否完成一次遮罩点击（本遮罩按压后的 Release，或同帧成对的
// Press+Release）。Cancel 清除按压；按住拖出遮罩区后 Release 不会路由
// 回遮罩（无 grab），残留按压在下次 Press 时重置。
func (s *scrimState) layout(gtx layout.Context, tag *overlayTag) (clicked bool) {
	defer clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)

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
				s.pressed = true
				s.pid = e.PointerID
			}
		case pointer.Release:
			if s.pressed && s.pid == e.PointerID {
				clicked = true
			}
			s.pressed = false
		case pointer.Cancel:
			s.pressed = false
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

// focusState 保存「本轮打开周期是否已请求过焦点」。焦点只在打开转换时
// 请求一次：内容（如 Editor）已取得的焦点不被逐帧强抢（R1 修复 F-B2）。
type focusState struct {
	requested bool
}

// layout 注册 focusable 资格并消费本帧发往本组件的 Escape。
//
//   - request 为 true 时执行一次 FocusCmd（调用方以「打开转换」为准传入）；
//   - key.FocusFilter 是必须的：keyQueue.Frame 在帧末摘除不可聚焦标签的
//     焦点，而 focusable 资格只由 FocusFilter 授予（key.Filter 不算）；
//   - Escape 用**不带 Focus 约束**的过滤器消费（keyFilterMatch 对
//     Focus==nil 不做焦点比对）：无论焦点在遮罩还是内容都可达，这是
//     「约定关闭路径」，不依赖每帧抢焦点。
func (f *focusState) layout(gtx layout.Context, tag *overlayTag, request bool) (escape bool) {
	if request {
		f.requested = true
		gtx.Execute(key.FocusCmd{Tag: tag})
	}
	gtx.Source.Event(key.FocusFilter{Target: tag})
	for {
		ev, ok := gtx.Source.Event(key.Filter{Name: key.NameEscape})
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
// 内收、负坐标收到 0。调用方必须先把弹层尺寸封顶到视口内（R1 修复 F-B4
// 后高菜单在测量阶段封顶高度），钳制只负责位置。
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
