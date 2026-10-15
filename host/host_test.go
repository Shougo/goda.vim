package host

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Shougo/goda.vim/sdk"
)

type testPlugin struct {
	name     string
	handlers map[string]sdk.Handler
}

func (p testPlugin) Name() string                     { return p.name }
func (p testPlugin) Handlers() map[string]sdk.Handler { return p.handlers }

func nop(context.Context, sdk.Host, []any) (any, error) { return "ok", nil }

func TestTable(t *testing.T) {
	table, err := Table(
		testPlugin{"cmp", map[string]sdk.Handler{"complete": nop, "gather": nop}},
		testPlugin{"skk", map[string]sdk.Handler{"toggle": nop}},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cmp.complete", "cmp.gather", "skk.toggle", "goda.plugins"} {
		if table[name] == nil {
			t.Errorf("method %s is missing", name)
		}
	}
	if len(table) != 4 {
		t.Errorf("unexpected methods: %v", table)
	}

	got, err := table["goda.plugins"](context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"cmp": []any{"complete", "gather"}, "skk": []any{"toggle"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("goda.plugins = %v, want %v", got, want)
	}
}

func TestTableWithoutPlugins(t *testing.T) {
	table, err := Table()
	if err != nil || table["goda.plugins"] == nil {
		t.Fatalf("goda.plugins must exist even without plugins: %v, %v", table, err)
	}
}

func TestTableErrors(t *testing.T) {
	h := map[string]sdk.Handler{"m": nop}
	tests := map[string][]sdk.Plugin{
		"nil plugin":       {nil},
		"empty name":       {testPlugin{"", h}},
		"uppercase":        {testPlugin{"Cmp", h}},
		"dot in name":      {testPlugin{"a.b", h}},
		"reserved":         {testPlugin{"goda", h}},
		"duplicate":        {testPlugin{"a", h}, testPlugin{"a", h}},
		"bad method":       {testPlugin{"a", map[string]sdk.Handler{"x.y": nop}}},
		"empty method":     {testPlugin{"a", map[string]sdk.Handler{"": nop}}},
		"nil handler":      {testPlugin{"a", map[string]sdk.Handler{"m": nil}}},
		"digit-led method": {testPlugin{"a", map[string]sdk.Handler{"1m": nop}}},
	}
	for name, ps := range tests {
		if _, err := Table(ps...); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestServeTransportErrors(t *testing.T) {
	if err := Serve("", testPlugin{"a", nil}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("a missing transport must be reported, got %v", err)
	}
	if err := Serve("emacs"); err == nil || !strings.Contains(err.Error(), `unknown transport "emacs"`) {
		t.Errorf("an unknown transport must be reported, got %v", err)
	}
	// Plugin errors win over transport errors: nothing is served half-configured.
	if err := Serve("nvim", testPlugin{"BAD", nil}); err == nil || !strings.Contains(err.Error(), "must match") {
		t.Errorf("got %v", err)
	}
}
