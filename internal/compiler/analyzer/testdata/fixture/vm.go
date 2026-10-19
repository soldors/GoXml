// Package fixture 提供 Analyzer 夹具使用的 ViewModel 类型。
//
// 这些类型刻意包含各种边界形态：可访问字段、不可访问字段、
// 嵌套结构、切片、以及签名各异的事件处理方法。
// 夹具 .gxml 通过这些类型固定绑定解析与事件校验的期望行为。
package fixture

// Profile 是二级嵌套，用于验证三段路径 {User.Profile.Name}。
type Profile struct {
	Name string
	Age  int
	Note string
}

// User 是嵌套结构，用于验证多段路径。
type User struct {
	Name    string
	Email   string
	Age     int
	Profile Profile // 三段路径的中转
	note    string  // 不可导出：用于 BIND-003
}

// Item 是循环元素类型。
type Item struct {
	ID   int
	Name string
}

// Status 是命名类型且实现 fmt.Stringer。
type Status string

// String 实现 fmt.Stringer。
func (s Status) String() string { return string(s) }

// Meta 用于验证嵌入字段的提升访问。
type Meta struct {
	Version string
}

// ViewModel 是夹具使用的根类型。
type ViewModel struct {
	User   User
	Items  []Item
	Count  int
	Dirty  bool
	Title  string
	Lookup map[string]int
	Codes  [3]int // 数组：可迭代
	Owner  *User  // 指针：字段访问需解引用
	State  Status // 命名类型 + Stringer
	secret string // 根级未导出字段：用于 BIND-003
	Meta          // 嵌入：验证提升字段
}

// Display 是零参单返回值方法，可作为路径段。
func (vm *ViewModel) Display() string { return vm.Title }

// Format 有参数，不能作为路径段。
func (vm *ViewModel) Format(prefix string) string { return prefix }

// Pair 有多个返回值，不能作为路径段。
func (vm *ViewModel) Pair() (string, int) { return "", 0 }

// hiddenGetter 未导出，不可访问。
func (vm *ViewModel) hiddenGetter() string { return "" }

// ---- 事件处理方法：签名刻意覆盖各条校验规则 ----

// OnNameChanged 与 Input.ValueChanged 的载荷匹配：1 个 string。
func (vm *ViewModel) OnNameChanged(value string) {}

// Delete 接收 1 个 int，匹配 Click="Delete(User.Age)"。
func (vm *ViewModel) Delete(id int) {}

// OnOpen 接收载荷 id:string + 调用点实参 1 个 int。
func (vm *ViewModel) OnOpen(id string, cardID int) {}

// OnWrongArity 只有 0 个形参，但 ValueChanged 载荷有 1 个 → EVENT-004。
func (vm *ViewModel) OnWrongArity() {}

// OnWrongType 形参是 int，但 ValueChanged 载荷是 string → EVENT-006。
func (vm *ViewModel) OnWrongType(n int) {}

// OnWithReturn 有返回值 → 违反 Q4，用于事件方法形态校验。
func (vm *ViewModel) OnWithReturn() error { return nil }

// OnLiteral 接收调用点字面量实参。
func (vm *ViewModel) OnLiteral(n int) {}

// OnEcho 接收字符串实参。
func (vm *ViewModel) OnEcho(s string) {}

// OnToggle 接收布尔实参。
func (vm *ViewModel) OnToggle(b bool) {}

// ---- 零参、无返回值的合法事件处理器 ----
//
// Codegen 的事件切片（Button + Click）需要「无参且无返回值」的处理器：
// 上面多数方法为覆盖校验规则而带参数或有返回值，不适合作为正向用例。

// Increment 是零参无返回值的合法处理器，用于 <Button Click="Increment"/>。
func (vm *ViewModel) Increment() { vm.Count++ }

// Reset 同为零参无返回值处理器，用于验证多个 Button 各自独立接线。
func (vm *ViewModel) Reset() { vm.Count = 0 }
