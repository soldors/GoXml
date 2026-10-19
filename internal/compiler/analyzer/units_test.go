package analyzer

import (
	"go/types"
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/expander"
)

// ============================================================================
// 纯函数单元测试
//
// 有些分支无法通过 .gxml 触发：例如 color/insets 载荷目前没有内置事件
// 使用它，而 <Def Events> 在 v1 不传播事件（规范 §7.2.3）。这些分支
// 仍属契约的一部分，直接测函数而不是跳过。
// ============================================================================

func TestPropTypeNameAllCases(t *testing.T) {
	cases := map[contracts.PropType]string{
		contracts.PropString:  "string",
		contracts.PropBool:    "bool",
		contracts.PropInt:     "int",
		contracts.PropFloat:   "float",
		contracts.PropColor:   "color",
		contracts.PropInsets:  "insets",
		contracts.PropUnknown: "unknown",
	}
	for p, want := range cases {
		if got := propTypeName(p); got != want {
			t.Errorf("propTypeName(%d) = %q, want %q", uint8(p), got, want)
		}
	}
}

func TestTypeInfoForPropAllCases(t *testing.T) {
	cases := map[contracts.PropType]ValueKind{
		contracts.PropString: ValueString,
		contracts.PropBool:   ValueBool,
		contracts.PropInt:    ValueInt,
		contracts.PropFloat:  ValueFloat,
		contracts.PropColor:  ValueStruct,
		contracts.PropInsets: ValueStruct,
	}
	for p, want := range cases {
		ti := typeInfoForProp(p)
		if ti.Kind != want {
			t.Errorf("typeInfoForProp(%s).Kind = %v, want %v", propTypeName(p), ti.Kind, want)
		}
		if ti.Display == "" {
			t.Errorf("typeInfoForProp(%s).Display 不应为空", propTypeName(p))
		}
	}
	if got := typeInfoForProp(contracts.PropUnknown); got != (TypeInfo{}) {
		t.Errorf("PropUnknown 应返回零值, 实际 %+v", got)
	}
}

func TestLiteralType(t *testing.T) {
	ok := map[string]ValueKind{
		"true":   ValueBool,
		"false":  ValueBool,
		"0":      ValueInt,
		"42":     ValueInt,
		"-3":     ValueInt,
		"2.5":    ValueFloat,
		"-0.5":   ValueFloat,
		"'hi'":   ValueString,
		"'42'":   ValueString, // 关键：带引号的 42 是字符串
		"'true'": ValueString, // 关键：带引号的 true 不是布尔
	}
	for lit, want := range ok {
		got, valid := literalType(lit)
		if !valid {
			t.Errorf("literalType(%q) 应有效", lit)
			continue
		}
		if k := valueKind(got); k != want {
			t.Errorf("literalType(%q) = %v, want %v", lit, k, want)
		}
	}

	// 空串与无法识别的形态应失败（不得猜一个类型）。
	for _, bad := range []string{"", "abc", "1.2.3"} {
		if _, valid := literalType(bad); valid {
			t.Errorf("literalType(%q) 应无效", bad)
		}
	}
}

func TestPayloadCompatible(t *testing.T) {
	env := newTestEnv(t, ".")

	cases := []struct {
		name  string
		prop  contracts.PropType
		param types.Type
		want  bool
	}{
		{"string→string", contracts.PropString, types.Typ[types.String], true},
		{"string→int", contracts.PropString, types.Typ[types.Int], false},
		{"bool→bool", contracts.PropBool, types.Typ[types.Bool], true},
		{"bool→string", contracts.PropBool, types.Typ[types.String], false},
		{"int→int", contracts.PropInt, types.Typ[types.Int], true},
		{"int→int64", contracts.PropInt, types.Typ[types.Int64], true}, // 按类别：有符号宽度均可
		{"int→uint", contracts.PropInt, types.Typ[types.Uint], false},
		{"float→float64", contracts.PropFloat, types.Typ[types.Float64], true},
		{"float→float32", contracts.PropFloat, types.Typ[types.Float32], true}, // 载荷无具体 Go 类型，按类别匹配
		{"float→int", contracts.PropFloat, types.Typ[types.Int], false},
		{"color→color.NRGBA", contracts.PropColor,
			stdlibType(t, env, stdColorPkg, "NRGBA"), true},
		{"color→color.RGBA", contracts.PropColor,
			stdlibType(t, env, stdColorPkg, "RGBA"), true},
		{"color→string", contracts.PropColor, types.Typ[types.String], false},
		{"insets→EdgeInsets", contracts.PropInsets,
			stdlibType(t, env, contractsPkg, "EdgeInsets"), true},
		{"insets→string", contracts.PropInsets, types.Typ[types.String], false},
		{"unknown→string", contracts.PropUnknown, types.Typ[types.String], false},
		{"nil param", contracts.PropString, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := payloadCompatible(c.prop, c.param, env); got != c.want {
				t.Errorf("payloadCompatible(%s, %s) = %v, want %v",
					propTypeName(c.prop), typeInfo(c.param).Display, got, c.want)
			}
		})
	}

	// env 为 nil 时，color/insets 一律不放行。
	if payloadCompatible(contracts.PropColor, stdlibType(t, env, stdColorPkg, "NRGBA"), nil) {
		t.Error("env 为 nil 时不应放行 color")
	}
	// 内建类型不依赖 env。
	if !payloadCompatible(contracts.PropString, types.Typ[types.String], nil) {
		t.Error("string 判定不应依赖 env")
	}
}

func TestMatchesAnyAssignableMissingPackage(t *testing.T) {
	env := newTestEnv(t, ".")
	if matchesAnyAssignable(types.Typ[types.String], "no/such/pkg", []string{"X"}, env) {
		t.Error("包不存在时应返回 false")
	}
	if matchesAnyAssignable(types.Typ[types.String], stdColorPkg, []string{"NoSuchType"}, env) {
		t.Error("类型不存在时应返回 false")
	}
}

func TestMatchesAnyNamedMissingPackage(t *testing.T) {
	env := newTestEnv(t, ".")
	if matchesAnyNamed(types.Typ[types.String], "no/such/pkg", []string{"X"}, env) {
		t.Error("包不存在时应返回 false")
	}
	if matchesAnyNamed(types.Typ[types.String], stdColorPkg, []string{"NoSuchType"}, env) {
		t.Error("类型不存在时应返回 false")
	}
	if matchesAnyNamed(types.Typ[types.String], stdColorPkg, []string{"NRGBA"}, nil) {
		t.Error("env 为 nil 时应返回 false")
	}
}

func TestTypeInfoNilAndBasics(t *testing.T) {
	if got := typeInfo(nil); got != (TypeInfo{}) {
		t.Errorf("typeInfo(nil) = %+v, 期望零值", got)
	}
	// 非常规基础类型（complex）应归入 ValueOther。
	if k := valueKind(types.Typ[types.Complex128]); k != ValueOther {
		t.Errorf("complex128 → %v, 期望 other", k)
	}
	if k := valueKind(types.Typ[types.Uintptr]); k != ValueUint {
		t.Errorf("uintptr → %v, 期望 uint", k)
	}
}

func TestIterableElemTypeErrors(t *testing.T) {
	if _, _, err := iterableElemType(nil); err == nil {
		t.Error("nil 应报错")
	}
	if _, _, err := iterableElemType(types.Typ[types.String]); err == nil {
		t.Error("string 在 v1 不可迭代，应报错")
	}
	if _, _, err := iterableElemType(types.Typ[types.Int]); err == nil {
		t.Error("int 不可迭代，应报错")
	}
	// slice / array 应成功并给出正确的元素类型与 int 索引。
	for _, typ := range []types.Type{
		types.NewSlice(types.Typ[types.Int]),
		types.NewArray(types.Typ[types.Int], 3),
	} {
		elem, index, err := iterableElemType(typ)
		if err != nil {
			t.Errorf("%s 应可迭代: %v", typeInfo(typ).Display, err)
			continue
		}
		if elem == nil || index == nil {
			t.Errorf("%s 的元素/索引类型不应为 nil", typeInfo(typ).Display)
		}
	}

	// Map **不**可迭代（规范 §9：Map 作为 <For In> 数据源属 v1 明确不支持）。
	m := types.NewMap(types.Typ[types.String], types.Typ[types.Int])
	if _, _, err := iterableElemType(m); err == nil {
		t.Error("map 在 v1 不可迭代（规范 §9），应报错")
	}
}

// ---------------------------------------------------------------------------
// Schema 缺失时的防御性行为
// ---------------------------------------------------------------------------

func TestTargetTypeWithoutSchema(t *testing.T) {
	a := &analyzer{}
	if got := a.targetType("Text", "Value"); got != contracts.PropUnknown {
		t.Errorf("无 Schema 时 targetType = %v, 期望 unknown", got)
	}
	// Schema 存在但属性未声明。
	reg := &fakeRegistry{schemas: map[string]*expander.ComponentSchema{
		"X": {Tag: "X", Properties: []expander.PropertySchema{{Name: "A", Type: contracts.PropInt}}},
	}}
	a2 := &analyzer{schemas: reg}
	if got := a2.targetType("X", "A"); got != contracts.PropInt {
		t.Errorf("已声明属性应返回 int, 实际 %v", got)
	}
	if got := a2.targetType("X", "Nope"); got != contracts.PropUnknown {
		t.Errorf("未声明属性应返回 unknown, 实际 %v", got)
	}
	if got := a2.targetType("Missing", "A"); got != contracts.PropUnknown {
		t.Errorf("未知标签应返回 unknown, 实际 %v", got)
	}
}

func TestEventSchemaWithoutSchema(t *testing.T) {
	a := &analyzer{}
	if _, ok := a.eventSchema("Button", "Click"); ok {
		t.Error("无 Schema 时应返回 false")
	}
}

// fakeRegistry 是最小的 SchemaRegistry 实现。
type fakeRegistry struct {
	schemas map[string]*expander.ComponentSchema
}

func (f *fakeRegistry) Lookup(tag string) (*expander.ComponentSchema, bool) {
	s, ok := f.schemas[tag]
	return s, ok
}
