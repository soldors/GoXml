package codegen

// R25-R2 S3：代表性非 Expanded 输入的逐字节回归（多样本，替代 R25-B 的
// 单样本 hash）。样本覆盖：静态页、Exact/Fill 尺寸、动态容器（If/For）、
// Padding 装饰、Input 事件。产物写 TempDir，由基线树同测试 diff。

import (
	"os"
	"path/filepath"
	"testing"
)

var r25r2Samples = []struct {
	name string
	body string
}{
	{"static", `<Column><Text Value="静态甲"/><Text Value="静态乙"/></Column>`},
	{"exact-fill", `<Row Height="80"><Text Value="L" Width="40"/><Text Value="R" Width="Fill"/><Text Value="R2"/></Row>`},
	{"dynamic-container", `<Column><If Condition="{Dirty}"><Then><Text Value="{Title}"/></Then></If><For Each="item" In="{Items}" Key="{item.ID}"><Row Height="40"><Text Value="{item.Name}"/></Row></For></Column>`},
	{"padding-deco", `<Column Padding="6"><Button Text="点" Click="Increment" Background="#EFEFEF" Padding="4"/></Column>`},
}

// TestR25R2WriteSamples 候选/基线两分支各跑一次，产物落 TempDir 供 diff。
func TestR25R2WriteSamples(t *testing.T) {
	dst := os.Getenv("R25R2_SAMPLE_DIR")
	if dst == "" {
		t.Skip("R25R2_SAMPLE_DIR 未设置（逐字节对照由协调者/脚本两分支各跑一次）")
	}
	for _, tc := range r25r2Samples {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			v := verified(t, tc.body)
			out, err := Generate(v, bindingOpts())
			if err != nil {
				t.Fatalf("%s 生成失败: %v", tc.name, err)
			}
			p := filepath.Join(dst, tc.name+".go")
			if err := os.WriteFile(p, out, 0o644); err != nil {
				t.Fatal(err)
			}
		})
	}
}
