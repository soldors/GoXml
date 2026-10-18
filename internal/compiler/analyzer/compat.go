package analyzer

import (
	"go/types"

	"github.com/goxml/goxml/contracts"
)

// 本文件实现契约 §11「类型兼容契约」：判定绑定值的 Go 类型能否赋给
// 属性的 PropType。只做兼容判定，不做值转换——转换属于 Codegen。

// 兼容结果三态。
type compatResult uint8

const (
	// compatOK 类型兼容。
	compatOK compatResult = iota
	// compatMismatch 类型不兼容，应报 BIND-004。
	compatMismatch
	// compatUnknown Schema 未声明类型（PropUnknown），属内部契约错误：
	// 不报用户诊断，但该绑定不得进入 Facts（契约 §11.7）。
	compatUnknown
)

// 精确类型身份所需的包路径与类型名（契约 §11.5）。
const (
	stdColorPkg    = "image/color"
	contractsPkg   = "github.com/goxml/goxml/contracts"
	insetsTypeName = "EdgeInsets"
)

// 颜色属性接受的精确类型。
var colorTypeNames = []string{"NRGBA", "RGBA"}

// checkCompat 判定 t 是否可赋给 prop。
//
// env 用于解析精确身份类型（color / insets）。为 nil 时，这两类一律
// 视为不兼容——宁可拒绝，也不要因为「看起来像」而放行。
func checkCompat(t types.Type, prop contracts.PropType, env *TypeEnv) compatResult {
	if prop == contracts.PropUnknown {
		return compatUnknown
	}
	if t == nil {
		return compatMismatch
	}

	switch prop {
	case contracts.PropString:
		if valueKind(t) == ValueString {
			return compatOK
		}
	case contracts.PropBool:
		if valueKind(t) == ValueBool {
			return compatOK
		}
	case contracts.PropInt:
		// 只接受有符号整数；无符号需要范围检查，属值转换（§11.2）。
		if valueKind(t) == ValueInt {
			return compatOK
		}
	case contracts.PropFloat:
		if valueKind(t) == ValueFloat {
			return compatOK
		}
	case contracts.PropColor:
		if matchesAnyNamed(t, stdColorPkg, colorTypeNames, env) {
			return compatOK
		}
	case contracts.PropInsets:
		if matchesAnyNamed(t, contractsPkg, []string{insetsTypeName}, env) {
			return compatOK
		}
	}
	return compatMismatch
}

// matchesAnyNamed 检查 t 是否为 pkgPath 下名为 names 之一的精确类型。
//
// 用 types.Identical 比较，而不是比较字符串：后者会被别名、vendoring、
// 模块替换等因素影响。也不接受「结构相似」的类型——契约 §11.5 明确
// 要求按身份判定，避免任何含 RGBA 字段的结构体被误认为颜色。
func matchesAnyNamed(t types.Type, pkgPath string, names []string, env *TypeEnv) bool {
	if env == nil {
		return false
	}
	want, err := env.LookupNamed(pkgPath, names)
	if err != nil {
		// 期望类型加载失败：属环境问题，不放行。
		return false
	}
	for _, w := range want {
		if types.Identical(t, w) {
			return true
		}
	}
	return false
}

// negatable 判定 `{!Path}` 的路径类型是否允许取反（契约 §11.6）。
//
// 只允许布尔（含命名布尔类型）。不得把未知类型推断为 bool。
func negatable(t types.Type) bool {
	return t != nil && valueKind(t) == ValueBool
}

// propTypeName 返回 PropType 的可读名，用于诊断消息。
func propTypeName(p contracts.PropType) string {
	switch p {
	case contracts.PropString:
		return "string"
	case contracts.PropBool:
		return "bool"
	case contracts.PropInt:
		return "int"
	case contracts.PropFloat:
		return "float"
	case contracts.PropColor:
		return "color"
	case contracts.PropInsets:
		return "insets"
	}
	return "unknown"
}
