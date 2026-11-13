//go:build windows

package watchadapter

// R29-A R5：共享 generate 事务跨进程串行化（LockFileEx）。同项目并发 withGenerateLock 不得并发进入临界区。
// 用进程内并发 goroutine 争用同一 lock 文件验证互斥（跨进程同一 LockFileEx 语义；Windows 独占锁）。

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestR5GenerateLockSerializesSharedTransaction(t *testing.T) {
	dir := t.TempDir()
	var inCritical int32
	var maxConcurrent int32
	var wg sync.WaitGroup
	const n = 6
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = withGenerateLock(context.Background(), dir, func() (int, string) {
				c := atomic.AddInt32(&inCritical, 1)
				for {
					m := atomic.LoadInt32(&maxConcurrent)
					if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond) // 临界区
				atomic.AddInt32(&inCritical, -1)
				return 0, ""
			})
		}()
	}
	wg.Wait()
	if maxConcurrent > 1 {
		t.Fatalf("generate lock must serialize shared transaction; observed %d concurrent", maxConcurrent)
	}
}

// 取消时有界返回（不无限等待锁）。
func TestR5GenerateLockCancelBounded(t *testing.T) {
	dir := t.TempDir()
	hold := make(chan struct{})
	go func() {
		_, _ = withGenerateLock(context.Background(), dir, func() (int, string) {
			<-hold
			return 0, ""
		})
	}()
	time.Sleep(60 * time.Millisecond) // 确保持锁
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	start := time.Now()
	rc, _ := withGenerateLock(ctx, dir, func() (int, string) { return 0, "should-not-run" })
	el := time.Since(start)
	close(hold)
	if rc == 0 {
		t.Fatal("canceled lock wait must not enter critical section")
	}
	if el > 2*time.Second {
		t.Fatalf("canceled lock wait not bounded: %v", el)
	}
}
