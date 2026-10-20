package main

import (
	"os"

	"github.com/goxml/goxml/internal/cli"
)

// goxml 是 Goxml 的命令行入口。
//
// 当前只有 help 与 generate 的参数层（TASK-GENERATE-CLI-001-1）；
// generate 的管线接线归 1-2。
func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
