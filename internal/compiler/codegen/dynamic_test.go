package codegen

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// 回归：动态节点判定必须包含事件
//
// 缺陷背景（2026-10-19 核实）：判定函数原名 hasBinding，只看属性绑定。
// 含 Click 的页面会被判为静态 → 整页录进 op.CallOp → 录制帧轮询一次
// Clicked(gtx) 后再无人点击，后续帧只回放宏。
// 表现：编译通过、界面正常、点了没反应。
//
// 事件处理输出契约 §4.1 冻结的判定：事件 ∨ 绑定 ∨ 子节点动态。
// ============================================================================

func nodeWithEvent() *contracts.NodeIR {
	return &contracts.NodeIR{
		Tag: "Button",
		Events: []contracts.EventBinding{{
			Name:   "Click",
			Method: "Save",
		}},
	}
}

func nodeStatic() *contracts.NodeIR {
	return &contracts.NodeIR{
		Tag: "Text",
		Attrs: []contracts.Attr{{
			Name:  "Value",
			Value: contracts.BindingVal{Raw: "hello"},
		}},
	}
}

func nodeWithBinding() *contracts.NodeIR {
	return &contracts.NodeIR{
		Tag: "Text",
		Attrs: []contracts.Attr{{
			Name:  "Value",
			Value: contracts.BindingVal{IsBinding: true, Segments: []string{"Title"}},
		}},
	}
}

func TestHasDynamic_EventMakesNodeDynamic(t *testing.T) {
	// 核心断言：只有事件、没有绑定，也必须是动态。
	if !hasDynamic(nodeWithEvent()) {
		t.Fatal("含事件的节点必须判为动态——否则会被整页固化，按钮点不动")
	}
}

func TestHasDynamic_BindingStillDynamic(t *testing.T) {
	if !hasDynamic(nodeWithBinding()) {
		t.Fatal("含绑定的节点必须判为动态（既有规则不可回退）")
	}
}

func TestHasDynamic_StaticIsNotDynamic(t *testing.T) {
	if hasDynamic(nodeStatic()) {
		t.Fatal("纯静态节点不应判为动态（否则静态固化被无谓关闭）")
	}
}

func TestHasDynamic_ChildEventMakesParentDynamic(t *testing.T) {
	parent := &contracts.NodeIR{
		Tag:      "Column",
		Children: []*contracts.NodeIR{nodeStatic(), nodeWithEvent()},
	}
	if !hasDynamic(parent) {
		t.Fatal("子节点含事件时父节点必须判为动态")
	}
}

func TestHasDynamic_NilIsSafe(t *testing.T) {
	if hasDynamic(nil) {
		t.Fatal("nil 应返回 false")
	}
}

// TestButtonPageNotFrozen 端到端回归（事件页不得整页固化）：
// 含 Click 的页面生成结果中不得出现整页 cacheCall 回放。
//
// 这条测试此前无法写——Codegen 还不支持 Button。见 button_slice_test.go。
