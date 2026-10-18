// Package broken 故意包含类型错误，用于验证加载器如何识别「包加载失败」。
//
// 选择类型错误而非语法错误：语法错误会让 gofmt 无法解析，进而破坏
// 「gofmt -l . 必须为空」的质量门禁；类型错误同样会让 packages.Load
// 产生 p.Errors，但 gofmt 可以正常处理。
//
// 它位于 testdata 下：递归模式（./...）会跳过 testdata，
// 因此不影响 go build / go vet；只有显式加载才会看到它。
package broken

// BadType 是故意的类型错误（字符串不能赋给 int）。
var BadType int = "not an int"
