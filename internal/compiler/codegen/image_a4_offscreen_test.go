package codegen

// R16-A A4：<Image> 离屏像素验收（代表性尺寸 × Fit）。
//
// 判据（硬）：在有效 headless 后端下，渲染 32x32 结果必须**有非零像素**；
// 零像素或后端不可用**不得静默跳过**——明确标记 UNVERIFIED 并使测试失败，
// 由负责人决定是否接纳（任务要求：零像素必须明确失败或标为未验收）。

import (
	"strings"
	"testing"
)

// a4RenderNonZero 生成并离屏渲染一次，返回非零字节数；后端不可用返回 -1。
func a4RenderNonZero(t *testing.T, size, fit string, pngBytes []byte) int {
	t.Helper()
	spec := `<Image Src="assets/a.png" Width="` + size + `" Height="` + size + `"`
	if fit != "" {
		spec += ` Fit="` + fit + `"`
	}
	spec += `/>`
	probe := `
func TestProbeA4Image(t *testing.T) {
	th := material.NewTheme()
	const w, h = 32, 32
	win, err := headless.NewWindow(w, h)
	if err != nil { t.Fatalf("UNVERIFIED: headless 不可用: %v", err) }
	defer win.Release()
	v := NewGoxmlDashboardView()
	var o op.Ops
	v.Layout(rtGtx(&o, w, h), th)
	n := rtNonZero(rtShot(t, win, &o, w, h))
	t.Logf("A4NONZERO=%d", n)
}
`
	out := rtProbeAssets(t, spec, probe, map[string][]byte{"assets/a.png": pngBytes})
	if !strings.Contains(out, "[err=nil]") {
		t.Fatalf("A4 渲染子测试失败（%s/%s）:\n%s", size, fit, out)
	}
	// 从输出里取 A4NONZERO= 数值。
	var n = -1
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "A4NONZERO="); i >= 0 {
			v := strings.TrimSpace(line[i+len("A4NONZERO="):])
			// 去掉可能的尾部字符
			num := 0
			seen := false
			for _, c := range v {
				if c < '0' || c > '9' {
					break
				}
				num = num*10 + int(c-'0')
				seen = true
			}
			if seen {
				n = num
			}
		}
	}
	return n
}

// TestImageA4OffscreenPixels 代表性尺寸 × Fit，全部要求有效非零像素。
func TestImageA4OffscreenPixels(t *testing.T) {
	png := testPNGBytes(t, 4, 4)
	cases := []struct{ size, fit string }{
		{"16", ""}, // 默认 Contain
		{"32", "Contain"},
		{"32", "Cover"},
		{"32", "Unscaled"},
		{"48", "Contain"}, // 比图像大（放大）
		{"8", "Cover"},    // 比图像小（缩小裁剪）
	}
	for _, c := range cases {
		n := a4RenderNonZero(t, c.size, c.fit, png)
		if n < 0 {
			t.Fatalf("UNVERIFIED: %s/%s 无法取得像素计数（headless 不可用或输出缺失）", c.size, c.fit)
		}
		if n == 0 {
			t.Fatalf("UNVERIFIED/FAIL: %s/%s 渲染零像素（无有效像素判据）", c.size, c.fit)
		}
		t.Logf("size=%s fit=%q nonzero=%d", c.size, c.fit, n)
	}
}
