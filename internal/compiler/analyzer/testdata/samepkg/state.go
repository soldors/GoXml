// Package samepkg 用于验证 ViewModel 的「同包」写法（ViewModel="AppState"）。
//
// 该目录同时含 case.gxml：同包解析以 .gxml 所在目录为包目录。
package samepkg

// AppState 是同包声明的目标类型。
type AppState struct {
	Name string
}

// Field 是嵌套类型，用于多段路径。
type Field struct {
	Value string
}
