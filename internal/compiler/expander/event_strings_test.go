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
