// Package fixture 提供 Analyzer 夹具使用的 ViewModel 类型。
//
// 这些类型刻意包含各种边界形态：可访问字段、不可访问字段、
// 嵌套结构、切片、以及签名各异的事件处理方法。
// 夹具 .gxml 通过这些类型固定绑定解析与事件校验的期望行为。
package fixture

// User 是嵌套结构，用于验证多段路径。
type User struct {
	Name  string
	Email string
	Age   int
	note  string // 不可导出：用于 BIND-003
}

// Item 是循环元素类型。
type Item struct {
	ID   int
	Name string
}

// ViewModel 是夹具使用的根类型。
type ViewModel struct {
	User   User
	Items  []Item
	Count  int
	Dirty  bool
	Title  string
	Lookup map[string]int
}

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
