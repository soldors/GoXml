// Package r25vm 是 R25-B/R25-R2 Expanded 验收的持久 ViewModel 夹具。
//
// 与 rtProbe 运行时写入的 tmprtNN/vm 不同：本包随仓提交，供 Generate 层
// 测试（含 bench 与逐字节对照）使用稳定导入路径。
package r25vm

// VM 是 Expanded 验收用 ViewModel。
type VM struct {
	CountA, CountB int
	LastID         string
	ShowA          bool
	Rows           []Row
}

// Row 是 <For> 行数据。
type Row struct {
	ID   string
	Name string
}

// TapA/B 独立按钮处理器。
func (vm *VM) TapA() { vm.CountA++ }
func (vm *VM) TapB() { vm.CountB++ }

// TapID 记录行事件实参。
func (vm *VM) TapID(id string) { vm.LastID = id; vm.CountA++ }
