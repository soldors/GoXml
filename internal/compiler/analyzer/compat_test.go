package analyzer

import (
	"go/types"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// 契约 §11 类型兼容：表驱动测试
//
// 每行覆盖「允许 / 拒绝 / 无法判定」三态之一。表驱动而非逐例写函数，
// 是为了让新增类型时只需加一行，且能一眼看出矩阵是否完整。
// ============================================================================

// env 是共享的类型环境（加载夹具包与标准库类型）。
func compatEnv(t *testing.T) *TypeEnv {
	t.Helper()
	return newTestEnv(t, ".")
}

// namedType 取得夹具包中的命名类型。
func namedType(t *testing.T, env *TypeEnv, name string) types.Type {
	t.Helper()
	ts, err := env.LookupNamed(fixturePkgPath, []string{name})
	if err != nil || len(ts) == 0 {
		t.Fatalf("取 %s 失败: %v", name, err)
	}
	return ts[0]
}

// fieldType 取得 ViewModel 某字段的类型。
func fieldType(t *testing.T, env *TypeEnv, field string) types.Type {
	t.Helper()
	vm := namedType(t, env, "ViewModel")
	st := vm.Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Name() == field {
			return st.Field(i).Type()
		}
	}
	t.Fatalf("ViewModel 没有字段 %s", field)
	return nil
}

// TestCompatMatrix 覆盖兼容矩阵的每一格。
func TestCompatMatrix(t *testing.T) {
	env := compatEnv(t)

	type row struct {
		name string
		typ  types.Type
		prop contracts.PropType
		want compatResult
	}

	// 逐个字段取类型，避免依赖字段顺序。
	field := func(n string) types.Type { return fieldType(t, env, n) }

	rows := []row{
		// string
		{"string→string", field("Title"), contracts.PropString, compatOK},
		{"string→bool", field("Title"), contracts.PropBool, compatMismatch},
		{"string→int", field("Title"), contracts.PropInt, compatMismatch},
		{"string→float", field("Title"), contracts.PropFloat, compatMismatch},
		{"string→color", field("Title"), contracts.PropColor, compatMismatch},

		// bool
		{"bool→bool", field("Dirty"), contracts.PropBool, compatOK},
		{"bool→string", field("Dirty"), contracts.PropString, compatMismatch},
		{"bool→int", field("Dirty"), contracts.PropInt, compatMismatch},

		// int（有符号）
		{"int→int", field("Count"), contracts.PropInt, compatOK},
		{"int→string", field("Count"), contracts.PropString, compatMismatch},
		{"int→float", field("Count"), contracts.PropFloat, compatMismatch},

		// 命名类型按底层类别
		{"namedString→string", field("State"), contracts.PropString, compatOK},
		{"namedString→int", field("State"), contracts.PropInt, compatMismatch},

		// 结构体 / 切片 / Map / 指针 / 数组
		{"struct→string", field("User"), contracts.PropString, compatMismatch},
		{"slice→string", field("Items"), contracts.PropString, compatMismatch},
		{"map→string", field("Lookup"), contracts.PropString, compatMismatch},
		{"pointer→string", field("Owner"), contracts.PropString, compatMismatch},
		{"array→string", field("Codes"), contracts.PropString, compatMismatch},

		// color：精确身份
		{"color.NRGBA→color", stdlibType(t, env, stdColorPkg, "NRGBA"), contracts.PropColor, compatOK},
		{"color.RGBA→color", stdlibType(t, env, stdColorPkg, "RGBA"), contracts.PropColor, compatOK},
		{"color.NRGBA→string", stdlibType(t, env, stdColorPkg, "NRGBA"), contracts.PropString, compatMismatch},
		// 结构体即便含 RGBA 字段也不接受
		{"struct→color", field("User"), contracts.PropColor, compatMismatch},

		// insets：精确身份
		{"EdgeInsets→insets", stdlibType(t, env, contractsPkg, "EdgeInsets"), contracts.PropInsets, compatOK},
		{"EdgeInsets→color", stdlibType(t, env, contractsPkg, "EdgeInsets"), contracts.PropColor, compatMismatch},
		{"struct→insets", field("User"), contracts.PropInsets, compatMismatch},

		// PropUnknown 是内部契约错误，不得放行
		{"string→unknown", field("Title"), contracts.PropUnknown, compatUnknown},
		{"int→unknown", field("Count"), contracts.PropUnknown, compatUnknown},
	}

	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			got := checkCompat(r.typ, r.prop, env)
			if got != r.want {
				t.Errorf("checkCompat(%s, %s) = %v, want %v",
					typeInfo(r.typ).Display, propTypeName(r.prop), got, r.want)
			}
		})
	}
}

// stdlibType 加载标准库或本项目包中的命名类型。
func stdlibType(t *testing.T, env *TypeEnv, pkgPath, name string) types.Type {
	t.Helper()
	ts, err := env.LookupNamed(pkgPath, []string{name})
	if err != nil || len(ts) == 0 {
		t.Fatalf("加载 %s.%s 失败: %v", pkgPath, name, err)
	}
	return ts[0]
}

// ---------------------------------------------------------------------------
// 无符号整数一律拒绝（契约 §11.2）
// ---------------------------------------------------------------------------

func TestCompatRejectsUnsignedIntegers(t *testing.T) {
	env := compatEnv(t)
	for _, k := range []types.BasicKind{
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr,
	} {
		typ := types.Typ[k]
		if got := checkCompat(typ, contracts.PropInt, env); got != compatMismatch {
			t.Errorf("%s→int = %v, 期望拒绝", typeInfo(typ).Display, got)
		}
	}
}

// ---------------------------------------------------------------------------
// 有符号整数全部接受（契约 §11.4）
// ---------------------------------------------------------------------------

func TestCompatAcceptsAllSignedIntegers(t *testing.T) {
	env := compatEnv(t)
	for _, k := range []types.BasicKind{
		types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
	} {
		typ := types.Typ[k]
		if got := checkCompat(typ, contracts.PropInt, env); got != compatOK {
			t.Errorf("%s→int = %v, 期望允许", typeInfo(typ).Display, got)
		}
	}
}

// ---------------------------------------------------------------------------
// 浮点：只接受浮点，不接受整数隐式提升（契约 §11.2）
// ---------------------------------------------------------------------------

func TestCompatFloatOnly(t *testing.T) {
	env := compatEnv(t)
	for _, k := range []types.BasicKind{types.Float32, types.Float64} {
		if got := checkCompat(types.Typ[k], contracts.PropFloat, env); got != compatOK {
			t.Errorf("%s→float 应允许", types.Typ[k])
		}
	}
	for _, k := range []types.BasicKind{types.Int, types.Int64} {
		if got := checkCompat(types.Typ[k], contracts.PropFloat, env); got != compatMismatch {
			t.Errorf("%s→float 应拒绝（不做隐式提升）", types.Typ[k])
		}
	}
}

// ---------------------------------------------------------------------------
// 别名与定义类型等价（契约 §11.3）
// ---------------------------------------------------------------------------

func TestCompatAliasEqualsDefined(t *testing.T) {
	env := compatEnv(t)

	// fixture 里 State 是 type Status string（定义类型）；
	// string 本身是内建类型。两者都应兼容 string 属性。
	defined := fieldType(t, env, "State")
	builtin := types.Typ[types.String]

	for _, typ := range []types.Type{defined, builtin} {
		if got := checkCompat(typ, contracts.PropString, env); got != compatOK {
			t.Errorf("%s→string 应允许", typeInfo(typ).Display)
		}
	}
	// 定义类型必须保留 Named 身份。
	if !typeInfo(defined).Named {
		t.Error("定义类型应标记 Named")
	}
	if typeInfo(builtin).Named {
		t.Error("内建类型不应标记 Named")
	}
}

// ---------------------------------------------------------------------------
// Stringer 只作为事实，不影响兼容（契约 §11.3）
// ---------------------------------------------------------------------------

func TestStringerDoesNotAffectCompat(t *testing.T) {
	env := compatEnv(t)
	// State 实现 fmt.Stringer，且底层是 string → 兼容 string。
	state := fieldType(t, env, "State")
	ti := typeInfo(state)
	if !ti.Stringer {
		t.Fatal("State 应实现 fmt.Stringer")
	}
	if got := checkCompat(state, contracts.PropString, env); got != compatOK {
		t.Errorf("Stringer 类型底层是 string，应兼容")
	}
	// 但 Stringer 不能让非字符串底层类型兼容 string。
	vm := namedType(t, env, "ViewModel")
	if got := checkCompat(vm, contracts.PropString, env); got == compatOK {
		t.Error("实现 Stringer 的结构体不应因此兼容 string")
	}
}

// ---------------------------------------------------------------------------
// {!Path} 独立规则（契约 §11.6）
// ---------------------------------------------------------------------------

func TestNegatable(t *testing.T) {
	env := compatEnv(t)
	field := func(n string) types.Type { return fieldType(t, env, n) }

	cases := map[string]bool{
		"Dirty": true,  // bool
		"Title": false, // string
		"Count": false, // int
	}
	for f, want := range cases {
		if got := negatable(field(f)); got != want {
			t.Errorf("negatable(%s) = %v, want %v", f, got, want)
		}
	}
	// nil 不得推断为 bool。
	if negatable(nil) {
		t.Error("nil 类型不应可取反")
	}
}

func TestNegatedRequiresBoolTarget(t *testing.T) {
	// {!Dirty} 绑到 string 属性 → 类型不匹配（取反结果是 bool）。
	r := analyzeSrc(t, vmSrc(`<Text Value="{!Dirty}"/>`))
	codes := codesOf(r)
	if codes[CodeBindTypeMismatch] != 1 {
		t.Errorf("期望 BIND-004: %v", codes)
	}
}

func TestNegateNonBoolRejected(t *testing.T) {
	// {!Count} 绑到 bool 属性（If Condition）→ 操作数不是 bool。
	r := analyzeSrc(t, vmSrc(`
    <If Condition="{!Count}"><Then><Text Value="a"/></Then></If>`))
	codes := codesOf(r)
	if codes[CodeBindTypeMismatch] != 1 {
		t.Errorf("期望 BIND-004: %v", codes)
	}
	msg := r.Diags[0].Message
	if !contains(msg, "not bool") {
		t.Errorf("消息应说明不是 bool: %q", msg)
	}
}

func TestNegateMissingPathNoCascade(t *testing.T) {
	// 路径本身失败时，只报路径诊断，不额外报类型不匹配。
	r := analyzeSrc(t, vmSrc(`
    <If Condition="{!Nope}"><Then><Text Value="a"/></Then></If>`))
	codes := codesOf(r)
	if codes[CodePathRoot] != 1 {
		t.Errorf("应报 BIND-001: %v", codes)
	}
	if codes[CodeBindTypeMismatch] != 0 {
		t.Errorf("不应级联报 BIND-004: %v", codes)
	}
}

// ---------------------------------------------------------------------------
// 兼容失败不进事实
// ---------------------------------------------------------------------------

func TestCompatFailureNotInFacts(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Count}"/>`))
	if r.Status != StatusPartial {
		t.Fatalf("Status = %v", r.Status)
	}
	if codesOf(r)[CodeBindTypeMismatch] != 1 {
		t.Fatalf("期望 BIND-004: %v", codesOf(r))
	}
	if r.Facts.Len() != 0 {
		t.Errorf("类型不匹配的绑定不应产生事实, Len = %d", r.Facts.Len())
	}
}

// ---------------------------------------------------------------------------
// 诊断消息可读
// ---------------------------------------------------------------------------

func TestCompatMessageIsActionable(t *testing.T) {
	r := analyzeSrc(t, vmSrc(`<Text Value="{Count}"/>`))
	msg := r.Diags[0].Message
	for _, want := range []string{"{Count}", "int", "string"} {
		if !contains(msg, want) {
			t.Errorf("消息应包含 %q: %q", want, msg)
		}
	}
}
