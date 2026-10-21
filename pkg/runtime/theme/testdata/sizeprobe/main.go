// Command sizeprobe 是 TASK-FONT-001-B / B5 的体积测量靶子。
//
// # 为什么需要它
//
// B5 要测的是「嵌入字体编进二进制后增加多少字节」。但仓库里现有的两个
// 可执行入口都**不链接** `pkg/runtime/theme`：
//
//   - `examples/demo` 走 `app.NewApp(view, material.NewTheme(), …)`，
//     自己注入 Gio 默认主题，因此 theme/embedded 被链接器整段丢弃；
//   - `cmd/goxml` 是编译器 CLI，不涉及运行时主题。
//
// 直接拿 demo 做前后对比会得到"增量 ≈ 0"，然后"未超 3MB 预算"这件事
// 就变成了假绿——那不是预算达标，而是根本没测到东西。
//
// 本程序只做一件事：**走一次默认主题路径**（`theme.NewTheme()`），
// 于是 `theme → probe → defaultEmbeddedLoader → embedded.Load → fontData`
// 这条链被真实链接进来，前后对比才有意义。
//
// 放在 `testdata/` 下是刻意的：`go build ./...`、`go vet ./...`、
// `go test ./...` 都会跳过 testdata，因此它不会被当成本项目的产物入口。
package main

import (
	"fmt"
	"os"

	"gioui.org/widget/material"

	"github.com/goxml/goxml/pkg/runtime/theme"
)

func main() {
	var th *material.Theme = theme.NewTheme()
	if th == nil {
		fmt.Fprintln(os.Stderr, "sizeprobe: NewTheme 返回 nil")
		os.Exit(1)
	}
	fmt.Println("sizeprobe: ok")
}
