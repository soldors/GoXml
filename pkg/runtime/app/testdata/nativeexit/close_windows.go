//go:build windows

package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// requestOwnedWindowClose asks this very process's window to close normally.
// The exact PID-qualified title and owning PID are both checked before WM_CLOSE.
// It never sends input, searches window content, or addresses another process.
func requestOwnedWindowClose(title string, beforePost func()) error {
	u := windows.NewLazySystemDLL("user32.dll")
	name, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return err
	}
	hwnd, _, _ := u.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(name)))
	if hwnd == 0 {
		return fmt.Errorf("self-owned window not found after real frames")
	}
	var owner uint32
	tid, _, err := u.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&owner)))
	if tid == 0 || owner != uint32(os.Getpid()) {
		return fmt.Errorf("window ownership mismatch: owner=%d self=%d: %v", owner, os.Getpid(), err)
	}
	beforePost()
	fmt.Fprintf(os.Stderr, "SELF_CLOSE_REQUEST pid=%d hwnd=%d trigger=second-real-frame-and-Lazy-done\n", owner, hwnd)
	const wmClose = 0x0010
	ok, _, err := u.NewProc("PostMessageW").Call(hwnd, wmClose, 0, 0)
	if ok == 0 {
		return fmt.Errorf("self WM_CLOSE failed: %w", err)
	}
	return nil
}
