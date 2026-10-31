package expander

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

func TestEventArgumentsKeepStringCommas(t *testing.T) {
	cases := []struct {
		value string
		want  []string
	}{
		{`Save('hello, world')`, []string{`'hello, world'`}},
		{`Save(',,', 42, 'it''s, fine', false, User.Name)`, []string{`',,'`, `42`, `'it''s, fine'`, `false`, `User.Name`}},
		{`Save('a,b', 'c,d')`, []string{`'a,b'`, `'c,d'`}},
		{`Save('a\,b')`, []string{`'a\,b'`}},
		{`Save('it's, fine')`, []string{`'it's, fine'`}},
		// R2-zcode 补充边界：空串、串尾逗号、Unicode（多字节 UTF-8 不得被
		// 按字节切分破坏，串内逗号不算分隔符）。
		{`Save('')`, []string{`''`}},
		{`Save('a,')`, []string{`'a,'`}},
		{`Save('中文, 值')`, []string{`'中文, 值'`}},
		{`Save('日本語', '中文')`, []string{`'日本語'`, `'中文'`}},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			e := &expander{file: "event.gxml"}
			ev, diag := e.parseEvent(contracts.RawAttr{Name: "Click", Value: contracts.BindingVal{Raw: tc.value}})
			if diag != nil {
				t.Fatal(diag)
			}
			if len(ev.Args) != len(tc.want) {
				t.Fatalf("argument count = %d, want %d", len(ev.Args), len(tc.want))
			}
			for i, arg := range ev.Args {
				got := arg.Literal
				if arg.Kind == contracts.ArgBinding {
					got = arg.Binding.Path()
				}
				if got != tc.want[i] {
					t.Errorf("argument %d = %q, want %q", i, got, tc.want[i])
				}
			}
		})
	}
}

func TestEventArgumentsRejectUnclosedString(t *testing.T) {
	for _, value := range []string{`Save('a,b)`, `Save('a,b',)`, `Save(, 'a,b')`, `Save('a,b', , 42)`} {
		t.Run(value, func(t *testing.T) {
			e := &expander{file: "event.gxml"}
			_, diag := e.parseEvent(contracts.RawAttr{Name: "Click", Value: contracts.BindingVal{Raw: value}})
			if diag == nil || diag.Code != CodeEventArg {
				t.Fatalf("want %s, got %v", CodeEventArg, diag)
			}
		})
	}
}
