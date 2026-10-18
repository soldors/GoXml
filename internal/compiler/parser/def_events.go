package parser

import (
	"strings"

	"github.com/goxml/goxml/contracts"
)

// parseDefEvents 解析 <Def Events="Click() ; Open(id:string)">。
//
// 语法：分号分隔的事件声明，每项为
//
//	Name
//	Name()
//	Name(param:type, param:type)
//
// 事件签名描述「组件发出什么事件、携带什么数据」，与处理器调用参数
// （Click="Save(item.ID)"）是两件不同的事，不得共用同一份声明。
func (p *parser) parseDefEvents(raw string, span Span) ([]contracts.EventSchema, *contracts.CompilerError) {
	var out []contracts.EventSchema

	for _, decl := range strings.Split(raw, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}

		name := decl
		var paramSrc string
		hasParen := false

		if open := strings.IndexByte(decl, '('); open >= 0 {
			if !strings.HasSuffix(decl, ")") {
				e := p.diag(span, CodeDef, "event declaration %q is missing a closing parenthesis", decl)
				return nil, &e
			}
			name = strings.TrimSpace(decl[:open])
			paramSrc = decl[open+1 : len(decl)-1]
			hasParen = true
		}

		if !isIdentifier(name) {
			e := p.diag(span, CodeDef, "invalid event name %q", name)
			return nil, &e
		}

		ev := contracts.EventSchema{Name: name, Span: span}

		if hasParen && strings.TrimSpace(paramSrc) != "" {
			params, cerr := p.parseEventParams(paramSrc, span, name)
			if cerr != nil {
				return nil, cerr
			}
			ev.Params = params
		}
		out = append(out, ev)
	}

	if len(out) == 0 {
		e := p.diag(span, CodeDef, "Events must declare at least one event")
		return nil, &e
	}
	return out, nil
}

// parseEventParams 解析 "id:string, count:int"。
func (p *parser) parseEventParams(src string, span Span, evName string) ([]contracts.EventParam, *contracts.CompilerError) {
	var out []contracts.EventParam
	seen := map[string]bool{}

	for _, part := range strings.Split(src, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, typeName, ok := strings.Cut(part, ":")
		if !ok {
			e := p.diag(span, CodeDef,
				"invalid parameter %q in event %q; expected name:type", part, evName)
			return nil, &e
		}
		name = strings.TrimSpace(name)
		if !isIdentifier(name) {
			e := p.diag(span, CodeDef, "invalid parameter name %q in event %q", name, evName)
			return nil, &e
		}
		if seen[name] {
			e := p.diag(span, CodeDef, "duplicate parameter %q in event %q", name, evName)
			return nil, &e
		}
		seen[name] = true

		pt := contracts.ParsePropType(typeName)
		if pt == contracts.PropUnknown {
			e := p.diag(span, CodeDef,
				"unknown parameter type %q in event %q; expected string|bool|int|float|color|insets",
				strings.TrimSpace(typeName), evName)
			return nil, &e
		}
		out = append(out, contracts.EventParam{Name: name, Type: pt})
	}
	return out, nil
}
