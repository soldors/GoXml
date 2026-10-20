package miniproj

// VM 是 CLI 最小夹具的 ViewModel（与 home.gxml 的 ViewModel="VM" 对应）。
type VM struct {
	Title string
	Count int
}

// Increment 是零参事件处理器。
func (vm *VM) Increment() { vm.Count++ }
