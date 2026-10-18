module github.com/goxml/goxml

// 最低 Go 版本由依赖决定：gioui.org v0.10.3 要求 go 1.24.0。
// 项目统一基线取 1.26，与开发机一致。
go 1.26.0

require (
	gioui.org v0.10.3
	golang.org/x/mod v0.42.0
	golang.org/x/tools v0.52.0
)

require (
	github.com/go-text/typesetting v0.3.5 // indirect
	golang.org/x/exp/shiny v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/image v0.26.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)
