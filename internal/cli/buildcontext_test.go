package cli

import (
	"go/build"
	"reflect"
	"strings"
	"testing"
)

// 这些用例锁定 goFlagsSplit / tagsFromGOFLAGS 与 Go 工具链的解析逐条一致，
// 因此不是「随便写几条字符串测试」——每条都对应 cmd/internal/quoted.Split、
// base.InitGOFLAGS、work.tagsFlag.Set 里的一段真实规则。

func TestGoFlagsSplitFaithful(t *testing.T) {
	cases := []struct {
		in     string
		want   []string
		wantEr bool
	}{
		{"", nil, false},
		{"-mod=readonly", []string{"-mod=readonly"}, false},
		{"-tags=a,b -v", []string{"-tags=a,b", "-v"}, false},
		// 引号包裹整个字段：字段内不再转义，空格不算分隔。
		{`"-tags=a b"`, []string{"-tags=a b"}, false},
		{`'-tags=x y'`, []string{"-tags=x y"}, false},
		// 引号出现在字段中间不算引号（与 quoted.Split 注释一致）。
		{`-tags='a`, []string{"-tags='a"}, false},
		// 未闭合引号 → error。
		{`"-unterminated`, nil, true},
		{`'-unterminated`, nil, true},
		// tab/换行/回车都是分隔符。
		{"-tags=a\t-v\n-x", []string{"-tags=a", "-v", "-x"}, false},
	}
	for _, c := range cases {
		got, err := goFlagsSplit(c.in)
		if c.wantEr {
			if err == nil {
				t.Errorf("goFlagsSplit(%q) 期望报错，实际得 %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("goFlagsSplit(%q) 意外报错: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("goFlagsSplit(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestTagsFromGOFLAGS(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    []string
		wantEr  bool
		errPart string
	}{
		{"empty", "", nil, false, ""},
		{"no-tags", "-mod=readonly -v", nil, false, ""},
		{"single", "-tags=goxml_r3", []string{"goxml_r3"}, false, ""},
		{"comma", "-tags=a,b,c", []string{"a", "b", "c"}, false, ""},
		{"comma-empties-ignored", "-tags=,a,,b,", []string{"a", "b"}, false, ""},
		{"double-dash", "--tags=goxml_r3", []string{"goxml_r3"}, false, ""},
		// 重复 -tags：每次解析重置切片 → 最后一次为准（对齐 tagsFlag.Set 的 *v=[]{}）。
		{"repeat-last-wins", "-tags=a -tags=b", []string{"b"}, false, ""},
		// 值里含空格（整体被引号包成一个 token）→ 走 quoted.Split 形态。
		{"quoted-space", `"-tags=a b"`, []string{"a", "b"}, false, ""},
		{"unknown-flag-ignored", "-nosuchflag=1 -tags=x", []string{"x"}, false, ""},
		// 非 flag 形态（复刻 InitGOFLAGS 的拒绝条件）。
		{"non-flag-word", "notAFlag", nil, true, "non-flag"},
		{"bare-dash", "-", nil, true, "non-flag"},
		{"double-dash-bare", "--", nil, true, "non-flag"},
		{"triple-dash", "---x", nil, true, "non-flag"},
		{"eq-no-name", "-=v", nil, true, "non-flag"},
		{"double-eq-no-name", "--=v", nil, true, "non-flag"},
		// 裸 -tags 无值 → fail-closed（go 报 flag needs an argument）。
		{"bare-tags-no-value", "-tags", nil, true, "needs an argument"},
		// 未闭合引号在切词阶段就报错。
		{"unterminated", `"oops`, nil, true, "unterminated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := tagsFromGOFLAGS(c.in)
			if c.wantEr {
				if err == nil {
					t.Fatalf("tagsFromGOFLAGS(%q) 期望报错，实际得 %#v", c.in, got)
				}
				if c.errPart != "" && !strings.Contains(err.Error(), c.errPart) {
					t.Errorf("错误 %q 应含 %q", err.Error(), c.errPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("tagsFromGOFLAGS(%q) 意外报错: %v", c.in, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("tagsFromGOFLAGS(%q) = %#v, 期望 %#v", c.in, got, c.want)
			}
		})
	}
}

func TestBuildContextFromGOFLAGSPreservesDefaults(t *testing.T) {
	// 空 GOFLAGS：与 build.Default 一致（不新增 BuildTags、不吞平台约束）。
	ctxt, err := buildContextFromGOFLAGS("")
	if err != nil {
		t.Fatalf("空 GOFLAGS 不应报错: %v", err)
	}
	if ctxt.GOOS != build.Default.GOOS || ctxt.GOARCH != build.Default.GOARCH {
		t.Errorf("应继承 build.Default 的 GOOS/GOARCH: got %s/%s want %s/%s",
			ctxt.GOOS, ctxt.GOARCH, build.Default.GOOS, build.Default.GOARCH)
	}
	if len(ctxt.BuildTags) != 0 {
		t.Errorf("空 GOFLAGS 不应设 BuildTags，实际 %#v", ctxt.BuildTags)
	}
	// ReleaseTags/ToolTags 必须随拷贝保留——否则 //go:build go1.x 判定会变。
	if len(ctxt.ReleaseTags) != len(build.Default.ReleaseTags) {
		t.Errorf("ReleaseTags 不应在拷贝中丢失")
	}

	// 带 tag：只额外设 BuildTags，其余保持默认。
	ctxt2, err := buildContextFromGOFLAGS("-tags=alpha,beta")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ctxt2.BuildTags, []string{"alpha", "beta"}) {
		t.Errorf("BuildTags = %#v, 期望 [alpha beta]", ctxt2.BuildTags)
	}

	// 非法 GOFLAGS：返回 error，且返回的上下文是零值（绝不「假装成功」）。
	if _, err := buildContextFromGOFLAGS("garbage"); err == nil {
		t.Fatal("非法 GOFLAGS 必须报错")
	}
}
