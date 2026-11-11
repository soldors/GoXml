package hydrationchannel

// R28-A release follow-up：本地微基准入口（不替负责人定阈值、不做整应用采样）。
// 覆盖：通道 RTT（端点+客户端一次捕获）、DTO 大小影响、allocs、状态文件读写分项。
// 运行示例（本机受控回环）：
//   go test ./internal/devx/hydrationchannel/ -run '^$' -bench 'BenchmarkChannel' -benchmem -count=1

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	devxh "github.com/goxml/goxml/internal/devx/hydration"
	rst "github.com/goxml/goxml/pkg/runtime/hydration"
)

func benchEndpoint(tb testing.TB) (*Endpoint, string) {
	tb.Helper()
	payload, _ := json.Marshal(map[string]any{"items": make([]int, 16), "name": "bench"})
	e, err := NewEndpoint(&syncOwner{}, func(context.Context) ([]byte, int, error) { return payload, 1, nil },
		"s", "a", "v1", "tok", 4)
	if err != nil {
		tb.Fatal(err)
	}
	addr, err := e.Start()
	if err != nil {
		tb.Fatal(err)
	}
	return e, addr
}

// BenchmarkChannelCaptureRTT 一次真实回环捕获往返（端点+客户端）。
func BenchmarkChannelCaptureRTT(b *testing.B) {
	e, addr := benchEndpoint(b)
	defer e.Stop()
	c, _ := NewClient(addr, "s", "a", "v1", "tok")
	ctx := context.Background()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := c.Capture(ctx, 1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDTOSize 不同 DTO 大小下的编码（本地 CPU，无网络）。
func BenchmarkDTOSize(b *testing.B) {
	for _, n := range []int{16, 256, 4096} {
		items := make([]int, n)
		payload, _ := json.Marshal(map[string]any{"items": items})
		b.Run(sizeName(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := rst.NewEnvelope("v1", "a", "s", 1, time.Now(), payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStateWriteRead 状态文件原子写 + 读校验分项。
func BenchmarkStateWriteRead(b *testing.B) {
	dir := b.TempDir()
	st, _ := devxh.NewStore(dir, "bench")
	proto := &devxh.Protocol{Store: st, Schema: "v1", AppID: "a"}
	payload := []byte(`{"v":1}`)
	b.Run("write", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := proto.Snapshot(context.Background(), func(context.Context) ([]byte, error) { return payload, nil }, 1); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("read", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := st.ReadVerified(rst.Expect{Schema: "v1", AppID: "a", SessionID: "bench"}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func sizeName(n int) string {
	switch n {
	case 16:
		return "dto_16"
	case 256:
		return "dto_256"
	default:
		return "dto_4096"
	}
}
