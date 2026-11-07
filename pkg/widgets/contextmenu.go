package widgets

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// MenuItem 是 ContextMenu 的一条菜单项。
type MenuItem struct {
	// Title 是显示文本；Action 是 Clicked() 返回的动作名（可为空串）。
	Title  string
	Action string
	// Disabled 为 true 时条目仍渲染但不可选：点击不产生动作、不关菜单。
	Disabled bool
}

// 面板与条目的固定配色（本轮不做主题化；ThemeRef 不在授权范围）。
var (
	colPanelBackground        = color.NRGBA{R: 0xFA, G: 0xFA, B: 0xFA, A: 0xFF}
	colItemBackgroundDisabled = color.NRGBA{R: 0xEC, G: 0xEC, B: 0xEC, A: 0xFF}
	colItemForeground         = color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xFF}
	colItemForegroundDisabled = color.NRGBA{R: 0xA0, G: 0xA0, B: 0xA0, A: 0xFF}
)

// ContextMenu 是锚点定位的弹出菜单：OpenAt(anchor) 打开；选中条目/
// 外点/Escape 关闭。零值不可用，用 NewContextMenu 构造。
//
// 布局契约：
//   - 每帧调用 Layout（关闭时归零几何并廉价返回）；
//   - 条目集合在构造时固定；几何每帧按当前视口重算，不跨帧缓存点击坐标；
//   - 面板高度封顶到视口：内容更高时仅可见条目参与命中，其余经滚轮滚动
//     可达（R1 修复 F-B4 的有界策略；无滚动条视觉，见 scope-matrix C8）；
//   - Clicked()/Opened() 是状态查询，Layout 是唯一的帧内状态推进点。
type ContextMenu struct {
	items  []MenuItem
	clicks []widget.Clickable

	open      bool
	anchor    image.Point
	scrim     overlayTag // 遮罩命中区 + Escape 焦点（同一实例标签）
	panel     overlayTag // 面板空白吸收区
	scrollTag overlayTag // 滚轮区（PASS 模式，声明在条目之后）
	scrimSt   scrimState // 遮罩跨帧按压状态（F-B1）
	focusSt   focusState // 打开转换焦点请求（F-B2）
	scrollOff int        // 滚动偏移（px，向下为正；每帧按几何重钳制）

	// actionOK/actionPending 是「选中条目」的单次读取缓冲。
	actionOK      bool
	actionPending string

	// lastRect/itemRects 是最近一帧几何（诊断与测试用；关闭或不可见
	// 时归零）。
	lastRect  image.Rectangle
	itemRects []image.Rectangle
}

// NewContextMenu 以固定条目集合构造菜单（允许零条目：空面板仍可打开，
// 见包注释候选行为表）。
func NewContextMenu(items ...MenuItem) *ContextMenu {
	c := &ContextMenu{
		items:     append([]MenuItem(nil), items...),
		clicks:    make([]widget.Clickable, len(items)),
		itemRects: make([]image.Rectangle, len(items)),
	}
	c.scrim.owner = c
	c.panel.owner = c
	c.scrollTag.owner = c
	return c
}

// OpenAt 以锚点打开菜单（锚点是面板左上角期望位置，实际位置受视口钳制）。
// 每次打开是一个新的焦点请求转换，滚动从头开始。
func (c *ContextMenu) OpenAt(anchor image.Point) {
	c.anchor = anchor
	c.open = true
	c.focusSt.requested = false
	c.scrollOff = 0
}

// Close 关闭菜单；已关闭时为 no-op。
func (c *ContextMenu) Close() { c.open = false }

// Opened 报告菜单当前是否打开。
func (c *ContextMenu) Opened() bool { return c.open }

// Rect 返回最近一帧菜单面板区域（高度已封顶到视口）；关闭后为零矩形。
func (c *ContextMenu) Rect() image.Rectangle { return c.lastRect }

// ItemRect 返回最近一帧第 i 个条目的视口区域；该条目当前不可见时为零
// 矩形（滚动可达性见 Layout 注释）。
func (c *ContextMenu) ItemRect(i int) image.Rectangle { return c.itemRects[i] }

// Clicked 单次读取自上次读取以来选中的条目动作名；没有新选中时第二返回值
// 为 false。读取即清空：同一动作不会被消费两次。
func (c *ContextMenu) Clicked() (action string, ok bool) {
	action, ok = c.actionPending, c.actionOK
	c.actionPending, c.actionOK = "", false
	return action, ok
}

// Layout 布局菜单。打开时依次：遮罩（外点关闭）→ Escape（关闭）→
// 测量（高度封顶）与视口钳制 → 滚动 → 面板背景/空白吸收区 → 可见条目。
func (c *ContextMenu) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if !c.open {
		c.lastRect = image.Rectangle{}
		c.clearItemRects()
		return layout.Dimensions{}
	}

	// 1. 遮罩：全屏先声明区，压在更早布局的背景之上；外点关闭。
	if c.scrimSt.layout(gtx, &c.scrim) {
		c.open = false
	}
	// 2. Escape 关闭：仅打开转换请求一次焦点；Escape 经约定路径（无
	// Focus 约束的过滤器）无论焦点在遮罩还是内容都可达。
	if c.focusSt.layout(gtx, &c.scrim, !c.focusSt.requested) {
		c.open = false
	}
	if !c.open {
		return layout.Dimensions{}
	}

	// 3. 测量（高度封顶到视口）+ 视口钳制：面板位置与尺寸都不越出视口。
	rowH := c.rowHeightPx(gtx)
	contentH := len(c.items) * rowH
	capH := min(contentH, gtx.Constraints.Max.Y)
	panelSize := c.measurePanel(gtx, th)
	panelSize.Y = capH
	pos := viewportClamp(c.anchor, panelSize, gtx.Constraints.Max)
	rect := image.Rectangle{Min: pos, Max: pos.Add(panelSize)}
	c.lastRect = rect

	// 4. 滚动：滚轮事件落在面板上时更新偏移；每帧按当前几何重钳制
	// （约束变化后不残留越界偏移）。
	c.handleScroll(gtx, contentH-capH)

	// 5. 面板背景与空白吸收区（后于遮罩、先于条目声明）；面板裁剪
	// 覆盖到条目绘制结束。
	panelClip := clip.Rect(rect).Push(gtx.Ops)
	defer panelClip.Pop()
	paint.FillShape(gtx.Ops, colPanelBackground, clip.Rect(rect).Op())
	absorbArea(gtx, &c.panel, rect)

	// 6. 条目：先按 Gio 惯用法消费全部条目点击（非可见条目无命中区，
	// 天然不可点），再绘制当前可见窗口内的行。
	for i := range c.items {
		if c.items[i].Disabled {
			continue
		}
		for c.clicks[i].Clicked(gtx) {
			c.actionOK = true
			c.actionPending = c.items[i].Action
			c.open = false
		}
	}

	for i := range c.items {
		top := i*rowH - c.scrollOff
		c.itemRects[i] = image.Rectangle{}
		if top+rowH <= 0 || top >= capH {
			continue // 完全滚出可见窗口
		}
		rowRect := image.Rectangle{
			Min: image.Pt(0, top),
			Max: image.Pt(panelSize.X, top+rowH),
		}
		c.itemRects[i] = rowRect.Add(pos)

		rowGtx := gtx
		if c.items[i].Disabled {
			// 禁用条目：事件源置零，条目不接收任何指针事件。
			rowGtx = gtx.Disabled()
		}
		c.drawRow(rowGtx, th, i, c.items[i], rect.Min.Add(rowRect.Min), rowRect.Size())
	}

	// 滚轮区：PASS 模式的面板尺寸区，声明在条目之后——hitTest 对 pass
	// 区不阻断下行（条目点击仍可达条目自身），滚轮事件则由本区接收。
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &c.scrollTag)
	pass.Pop()
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// handleScroll 消费本帧落在面板上的滚轮事件并更新/钳制滚动偏移。
// 约定（候选，原生平台方向未验证）：Scroll.Y 视为「内容随滚轮下移量」，
// 即 Y>0 查看更靠前条目、Y<0 查看更靠后条目；无滚动条视觉。
func (c *ContextMenu) handleScroll(gtx layout.Context, maxOffset int) {
	if maxOffset <= 0 {
		c.scrollOff = 0
		return
	}
	for {
		ev, ok := gtx.Source.Event(pointer.Filter{
			Target:  &c.scrollTag,
			Kinds:   pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -maxOffset, Max: maxOffset},
		})
		if !ok {
			break
		}
		if e, isPtr := ev.(pointer.Event); isPtr && e.Kind == pointer.Scroll {
			c.scrollOff -= int(e.Scroll.Y)
			if c.scrollOff < 0 {
				c.scrollOff = 0
			}
			if c.scrollOff > maxOffset {
				c.scrollOff = maxOffset
			}
		}
	}
	if c.scrollOff > maxOffset {
		c.scrollOff = maxOffset
	}
	if c.scrollOff < 0 {
		c.scrollOff = 0
	}
}

// rowHeightPx 是条目行的固定像素高度。
func (c *ContextMenu) rowHeightPx(gtx layout.Context) int { return gtx.Dp(menuItemRow) }

// menuItemRow 是条目行的逻辑高度（dp），与文本无关，保证几何可判别。
const menuItemRow = unit.Dp(32)

func (c *ContextMenu) clearItemRects() {
	for i := range c.itemRects {
		c.itemRects[i] = image.Rectangle{}
	}
}

// measurePanel 计算面板期望宽度：宽 = 最宽条目 + 行内边距（不小于
// 96dp、不大于视口宽）。高度由调用方封顶（Layout 步骤 3）。
func (c *ContextMenu) measurePanel(gtx layout.Context, th *material.Theme) image.Point {
	padding := 2 * gtx.Dp(unit.Dp(16)) // 行内左右各 16dp
	width := 0
	for i := range c.items {
		w := labelWidth(gtx, th.Shaper, c.items[i].Title) + padding
		if w > width {
			width = w
		}
	}
	if min := gtx.Dp(unit.Dp(96)); width < min {
		width = min
	}
	if width > gtx.Constraints.Max.X {
		width = gtx.Constraints.Max.X
	}
	return image.Pt(width, len(c.items)*c.rowHeightPx(gtx))
}

// labelWidth 离线测量单行文本宽度（写进一次性 ops，不污染真实 ops）。
func labelWidth(gtx layout.Context, shaper *text.Shaper, s string) int {
	const maxInt = int(^uint(0) >> 1)
	var ops op.Ops
	wgtx := gtx
	wgtx.Ops = &ops
	wgtx.Constraints = layout.Constraints{Max: image.Pt(maxInt, maxInt)}
	l := widget.Label{}
	macro := op.Record(wgtx.Ops)
	dims := l.Layout(wgtx, shaper, font.Font{}, unit.Sp(14), s, macro.Stop())
	return dims.Size.X
}

// drawRow 在 origin 处绘制一条条目：ButtonLayout 负责命中区注册与
// 点击语义（禁用行已用 gtx.Disabled() 剪除事件源），内容为文本。
// 变换与裁剪全部 Push/Pop 作用域配对，不向后续同级布局泄漏。
func (c *ContextMenu) drawRow(gtx layout.Context, th *material.Theme, i int, item MenuItem, origin, size image.Point) {
	trans := op.Offset(origin).Push(gtx.Ops)
	defer trans.Pop()
	rowGtx := gtx
	rowGtx.Constraints = layout.Exact(size)

	bl := material.ButtonLayout(th, &c.clicks[i])
	bl.Background = transparent
	bl.CornerRadius = 0
	bl.Layout(rowGtx, func(gtx layout.Context) layout.Dimensions {
		fg := colItemForeground
		bg := transparent
		if item.Disabled {
			fg = colItemForegroundDisabled
			bg = colItemBackgroundDisabled
		}
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				defer clip.Rect(image.Rectangle{Max: gtx.Constraints.Min}).Push(gtx.Ops).Pop()
				paint.FillShape(gtx.Ops, bg, clip.Rect(image.Rectangle{Max: gtx.Constraints.Min}).Op())
				return layout.Dimensions{Size: gtx.Constraints.Min}
			},
			func(gtx layout.Context) layout.Dimensions {
				inset := layout.Inset{
					Top: unit.Dp(6), Bottom: unit.Dp(6),
					Left: unit.Dp(16), Right: unit.Dp(16),
				}
				col := op.Record(gtx.Ops)
				paint.ColorOp{Color: fg}.Add(gtx.Ops)
				textCall := col.Stop()
				l := widget.Label{}
				return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return l.Layout(gtx, th.Shaper, font.Font{}, unit.Sp(14), item.Title, textCall)
				})
			},
		)
	})
}

var transparent = color.NRGBA{}
