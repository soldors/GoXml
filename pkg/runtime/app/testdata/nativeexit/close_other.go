//go:build !windows

package main

import "fmt"

func requestOwnedWindowClose(string, func()) error {
	return fmt.Errorf("automatic native fixture close is Windows-only")
}
