package codegen

import (
	"fmt"
	"strings"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// ============================================================================
// 事件载荷的通用生成（TASK-EVENT-PAYLOAD-001，契约见任务卡）
//
// 事实来源（全部来自 Analyzer，Codegen 不重新校验）：
//   ResolvedEvent.Payload   []ResolvedParam —— 载荷的**声明**（Name/Type/Span）
//   ResolvedEvent.CallArgs  []ResolvedArg   —— XML 调用点实参（含类型化值）
//   ResolvedEvent.Mapping   []ParamOrigin   —— 逐形参标注来源
//   ResolvedEvent.Method.Params []ResolvedParam
//
// 关键认识：**Payload 里没有运行时值**——载荷的运行时取值只能由控件提供
// （如 Editor 的当前文本）。因此本层的输入是「控件提供的载荷表达式」，
// 输出是「按 Mapping 排好序的实参列表」。
//
// 责任边界（任务卡 §2.4）：
//   控件侧：为每个载荷参数提供一个 Go 表达式；
//   本层  ：按 Mapping 组装实参与调用；
//   Analyzer：载荷数量/类型/无返回值的用户错误诊断——本层**不重复诊断**。
// ============================================================================

// payloadExprFunc 由控件提供：给定载荷序号（0 起），返回该载荷的 Go 表达式。
//
// 返回 error 的情形是**控件实现缺陷**（如声明了载荷却给不出表达式），
// 不是用户的 GXML 语法错误。
type payloadExprFunc func(i int) (string, error)

// renderInvocationArgs 按 Mapping 组装方法调用的实参列表。
//
// 顺序严格由 Mapping 决定：载荷与调用点实参按形参位置交错排列
// （契约 §8.4 R1：Analyzer 已保证顺序合法）。
//
// 返回空串表示无实参（零参调用）。
func (g *generator) renderInvocationArgs(ev analyzer.ResolvedEvent, payload payloadExprFunc) (string, error) {
	// ---- 内部一致性校验（不是用户错误）----
	//
	// 三者关系由 Analyzer 保证；不成立说明 Analyzer 实现缺陷。
	// 这里**不伪装成用户诊断**，也不静默生成错误代码。
	if len(ev.Mapping) != len(ev.Method.Params) {
		return "", fmt.Errorf(
			"codegen: internal: event %q has %d mapping entries but the handler has %d parameters; "+
				"the analyzer contract (mapping ↔ params) is violated",
			ev.Name, len(ev.Mapping), len(ev.Method.Params))
	}

	wantPayload, wantCallArg := 0, 0
	for _, origin := range ev.Mapping {
		switch origin {
		case analyzer.FromPayload:
			wantPayload++
		case analyzer.FromCallArg:
			wantCallArg++
		default:
			return "", fmt.Errorf(
				"codegen: internal: event %q has unknown parameter origin %v at position %d",
				ev.Name, origin, wantPayload+wantCallArg)
		}
	}
	if wantPayload != len(ev.Payload) {
		return "", fmt.Errorf(
			"codegen: internal: event %q mapping expects %d payload parameters but Payload has %d",
			ev.Name, wantPayload, len(ev.Payload))
	}
	if wantCallArg != len(ev.CallArgs) {
		return "", fmt.Errorf(
			"codegen: internal: event %q mapping expects %d call arguments but CallArgs has %d",
			ev.Name, wantCallArg, len(ev.CallArgs))
	}
	if wantPayload > 0 && payload == nil {
		// 控件声明了载荷却没提供表达式：控件侧的实现缺陷。
		return "", fmt.Errorf(
			"codegen: internal: event %q requires %d payload argument(s) but the control provided no payload expression",
			ev.Name, wantPayload)
	}

	// ---- 按 Mapping 组装 ----
	out := make([]string, 0, len(ev.Mapping))
	pi, ci := 0, 0
	for _, origin := range ev.Mapping {
		switch origin {
		case analyzer.FromPayload:
			expr, err := payload(pi)
			if err != nil {
				return "", fmt.Errorf(
					"codegen: internal: event %q payload argument %d: %w", ev.Name, pi, err)
			}
			if strings.TrimSpace(expr) == "" {
				return "", fmt.Errorf(
					"codegen: internal: event %q payload argument %d rendered to an empty expression",
					ev.Name, pi)
			}
			out = append(out, expr)
			pi++

		case analyzer.FromCallArg:
			expr, err := g.renderEventArg(ev.CallArgs[ci])
			if err != nil {
				return "", err
			}
			out = append(out, expr)
			ci++
		}
	}

	return strings.Join(out, ", "), nil
}
