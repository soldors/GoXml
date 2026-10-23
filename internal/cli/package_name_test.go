package cli

import "testing"

// 本文件锁住 parsePackageName 的三种输入形态。
//
// 为什么值得单独测：包名错不是"少解析一点"，而是会一路流到 Codegen，
// 在生成产物的 package 子句里才炸，排查时要倒着追很远。以下三档分别对应
// 曾经真实存在的故障形态，改实现前先看它们会不会红。

func TestParsePackageName(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
		ok   bool
	}{
		{
			name: "LF 常规",
			src:  "package main\n\nfunc main() {}\n",
			want: "main",
			ok:   true,
		},
		{
			name: "CRLF 不得把 \\r 带进包名",
			src:  "package main\r\n\r\nfunc main() {}\r\n",
			want: "main",
			ok:   true,
		},
		{
			name: "行注释不得被当成包名的一部分",
			src:  "package main // 入口包\n\nfunc main() {}\n",
			want: "main",
			ok:   true,
		},
		{
			name: "前导空白",
			src:  "\t  package main\n",
			want: "main",
			ok:   true,
		},
		{
			name: "不以 package 开头的行不算",
			src:  "// package commented\npackage real\n",
			want: "real",
			ok:   true,
		},
		{
			name: "非法标识符应跳过而不是照收",
			src:  "package 123bad\npackage good\n",
			want: "good",
			ok:   true,
		},
		{
			name: "没有 package 子句",
			src:  "func main() {}\n",
			want: "",
			ok:   false,
		},
		{
			name: "空源码",
			src:  "",
			want: "",
			ok:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parsePackageName([]byte(c.src))
			if ok != c.ok {
				t.Fatalf("ok = %v，期望 %v（got=%q）", ok, c.ok, got)
			}
			if got != c.want {
				t.Fatalf("包名 = %q，期望 %q", got, c.want)
			}
		})
	}
}

// TestParsePackageNameRejectsCROnly：CR 也必须被裁掉，不能只裁 LF。
//
// 老式 Mac 换行（\r 单独出现）现在已经罕见，但 splitLines 只按 \n 切行，
// 如果某天改成按 \r 切行而 TrimSpace 不处理，这里会立刻红。
func TestParsePackageNameRejectsCROnly(t *testing.T) {
	got, ok := parsePackageName([]byte("package main\rpackage other\r"))
	if !ok {
		t.Fatalf("应识别到包名，实际失败")
	}
	if got != "main" && got != "other" {
		t.Fatalf("包名 = %q，期望裁掉 \\r 后的 main 或 other", got)
	}
}
