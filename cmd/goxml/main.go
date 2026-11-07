package main

import (
	"context"
	"github.com/goxml/goxml/internal/cli"
	"os"
	"os/signal"
	"syscall"
)

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.RunContext(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
func main() { os.Exit(run()) }
