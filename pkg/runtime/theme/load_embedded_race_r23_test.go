package theme

// R23-B：loadEmbedded 并发安全守卫。
//
// 背景（B3 无窗口 race 实证）：临时主题由**每个 Lazy 各自**构造，多个 Lazy 共享同一
// Provider 时会并发进入 newTempTheme -> Provider.loadEmbedded -> Embedded.Load()。
// EmbeddedFontLoader 契约允许调用方注入任意（可能非并发安全的）加载器；此前 loadEmbedded
// 没有 Provider 级同步，导致并发 Load() 产生数据竞争（TestNight07... 在 -race 下红）。
//
// 本守卫：用一个**非并发安全**的计数加载器（故意用普通 int，不放原子）从多个 goroutine
// 并发调用 loadEmbedded。在 `-race` 下，若 loadEmbedded 未串行化，本测试会报 DATA RACE；
// 串行化后不报，且计数精确等于并发次数。非 -race 下它验证功能一致性（不依赖本机字体）。

import (
	"sync"
	"testing"

	"time"
)

// racingCountingLoader 故意使用普通 int 计数（非原子），以在缺乏串行化时暴露 race。
type racingCountingLoader struct {
	calls int
	data  []byte
	name  string
}

func (l *racingCountingLoader) Load() ([]byte, string, error) {
	// 故意不加锁地自增——若 loadEmbedded 未串行化，此处数据竞争由 -race 报出。
	l.calls++
	return l.data, l.name, nil
}

// TestLoadEmbeddedConcurrentSharedProviderIsSerialized 并发 loadEmbedded 必须安全且计数精确。
func TestLoadEmbeddedConcurrentSharedProviderIsSerialized(t *testing.T) {
	// 用真实嵌入子集字节，确保 ParseCollection 成功、路径真实。
	data, name, err := defaultEmbeddedLoader{}.Load()
	if err != nil || len(data) == 0 {
		t.Fatalf("前提不成立：默认嵌入加载器不可用（%v），不得当作通过", err)
	}
	loader := &racingCountingLoader{data: data, name: name}
	p := NewProvider(ProviderOptions{Embedded: loader})

	const N = 32
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := make(map[string]int)
	faceCounts := make(map[int]int)
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			id, faces, res := p.loadEmbedded()
			if res.Err != nil {
				mu.Lock()
				t.Errorf("并发 loadEmbedded 返回错误: %v", res.Err)
				mu.Unlock()
				return
			}
			mu.Lock()
			ids[id]++
			faceCounts[len(faces)]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	// 计数必须精确 = N（每次调用都真正 Load 一次；串行化不改变调用次数语义）。
	if got := loader.calls; got != N {
		t.Fatalf("并发 loadEmbedded 的 Load() 调用次数 = %d，期望 %d", got, N)
	}
	// 结果一致性：同一 Provider 同一嵌入子集，id 唯一、faces 数唯一。
	if len(ids) != 1 {
		t.Fatalf("并发 loadEmbedded 返回了 %d 种 id，期望 1：%v", len(ids), ids)
	}
	if len(faceCounts) != 1 {
		t.Fatalf("并发 loadEmbedded 返回了多种 faces 数，期望一致：%v", faceCounts)
	}
	for fc := range faceCounts {
		if fc == 0 {
			t.Fatal("faces 数为 0：嵌入解析结果不应为空")
		}
	}
}

// Public Lazy requests must share Provider serialization as well as its cache.
// No custom Temp is supplied: every request traverses the actual temporary-theme
// path before the background Resolve. Completion uses Done, not sleeps or Layout.
func TestSharedProviderConcurrentLazyRequests(t *testing.T) {
	data, name, err := defaultEmbeddedLoader{}.Load()
	if err != nil || len(data) == 0 {
		t.Fatalf("embedded fixture unavailable: %v", err)
	}
	loader := &racingCountingLoader{data: data, name: name}
	p := NewProvider(ProviderOptions{Embedded: loader, Candidates: []string{}})
	const n = 16
	lazies := make([]*Lazy, n)
	start := make(chan struct{})
	finished := make(chan int, n)
	for i := range lazies {
		lazies[i], err = NewLazy(LazyOptions{Provider: p})
		if err != nil {
			t.Fatal(err)
		}
		go func(i int) {
			<-start
			lazies[i].Request()
			<-lazies[i].Done()
			finished <- i
		}(i)
	}
	close(start)
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for range lazies {
		select {
		case i := <-finished:
			rep := lazies[i].Report()
			if rep.State != LoadReady || rep.Err != nil || rep.Attempts != 1 || lazies[i].Theme() == nil {
				t.Errorf("Lazy[%d] incomplete: %+v", i, rep)
			}
		case <-deadline.C:
			t.Fatal("concurrent public Lazy requests did not complete within 10s")
		}
	}
	// Done follows publication, and the completion channels join all requests;
	// the intentionally unsafe loader counter can now be read without a race.
	if got := loader.calls; got != n+1 {
		t.Fatalf("Load calls=%d, want %d (one temporary per Lazy + one cached resolution)", got, n+1)
	}
	if got := p.Scans(); got != 1 {
		t.Fatalf("shared Provider scanned %d times, want 1", got)
	}
}
