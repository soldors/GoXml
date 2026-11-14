//go:build windows

package watchadapter

// R29-A 收口 S2：受控对照 —— 共享 generate 写入串行化（锁）vs 仅运行输出隔离。
// 证明：同一项目的并发 generate 事务必须靠跨进程锁串行化；仅给各运行不同输出目录**不足以**避免共享
// .goxml 生成写入的交错。并核实锁覆盖**非 Hydration（默认关闭）**的生成路径。

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 无锁并发 generate：临界区可重叠（maxConcurrent>1）——证明“仅输出隔离”不够。
func TestCloseoutUnlockedGenerateOverlaps(t *testing.T) {
	var cur, max int32
	var wg sync.WaitGroup
	work := func() {
		defer wg.Done()
		c := atomic.AddInt32(&cur, 1)
		for {
			m := atomic.LoadInt32(&max)
			if c <= m || atomic.CompareAndSwapInt32(&max, m, c) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
		atomic.AddInt32(&cur, -1)
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go work()
	}
	wg.Wait()
	if max <= 1 {
		t.Fatalf("sanity: unlocked concurrent work should overlap, observed max=%d", max)
	}
}

// 有锁并发 generate（同一项目 .goxml）：串行化（maxConcurrent==1），且锁文件创建于默认路径。
func TestCloseoutLockedGenerateSerializesDefaultPath(t *testing.T) {
	dir := t.TempDir()
	var cur, max int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = withGenerateLock(context.Background(), dir, func() (int, string) {
				c := atomic.AddInt32(&cur, 1)
				for {
					m := atomic.LoadInt32(&max)
					if c <= m || atomic.CompareAndSwapInt32(&max, m, c) {
						break
					}
				}
				time.Sleep(25 * time.Millisecond)
				atomic.AddInt32(&cur, -1)
				return 0, ""
			})
		}()
	}
	wg.Wait()
	if max != 1 {
		t.Fatalf("shared generate transaction must be serialized by lock, observed max=%d", max)
	}
	if _, err := os.Stat(filepath.Join(dir, ".goxml", "generate.lock")); err != nil {
		t.Fatalf("default (non-hydration) generate path must create .goxml/generate.lock: %v", err)
	}
}
