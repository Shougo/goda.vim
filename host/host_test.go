package host

import (
	"context"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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
	for _, name := range []string{"cmp.complete", "cmp.gather", "skk.toggle", "goda.plugins", "goda.hello"} {
		if table[name] == nil {
			t.Errorf("method %s is missing", name)
		}
	}
	if len(table) != 5 {
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

func TestHello(t *testing.T) {
	table, err := Table(testPlugin{"cmp", map[string]sdk.Handler{"complete": nop}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := table["goda.hello"](context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["protocol"] != int64(sdk.Protocol) {
		t.Errorf("protocol = %v, want %d", m["protocol"], sdk.Protocol)
	}
	if v, _ := m["version"].(string); v == "" {
		t.Errorf("version = %v", m["version"])
	}
	if !reflect.DeepEqual(m["plugins"], map[string]any{"cmp": []any{"complete"}}) {
		t.Errorf("plugins = %v", m["plugins"])
	}
}

// autoload/goda.vim must expect the protocol this module implements.
func TestVimScriptProtocol(t *testing.T) {
	b, err := os.ReadFile("../autoload/goda.vim")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^let s:protocol = (\d+)\s*$`).FindSubmatch(b)
	if m == nil {
		t.Fatal("autoload/goda.vim has no 'let s:protocol = N' line")
	}
	if got, want := string(m[1]), strconv.Itoa(sdk.Protocol); got != want {
		t.Errorf("autoload/goda.vim s:protocol = %s, sdk.Protocol = %s", got, want)
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

var closeMu sync.Mutex

type closerPlugin struct {
	testPlugin
	log    *[]string
	err    error
	panics bool
	block  bool
}

func (p closerPlugin) Close(ctx context.Context) error {
	closeMu.Lock()
	*p.log = append(*p.log, p.name)
	closeMu.Unlock()
	if p.panics {
		panic("boom")
	}
	if p.block {
		<-ctx.Done()
	}
	return p.err
}

func TestClosePlugins(t *testing.T) {
	var log []string
	plugins := []sdk.Plugin{
		closerPlugin{testPlugin: testPlugin{name: "a"}, log: &log},
		testPlugin{name: "plain"}, // no Close: skipped
		closerPlugin{testPlugin: testPlugin{name: "b"}, log: &log, panics: true},
		closerPlugin{testPlugin: testPlugin{name: "c"}, log: &log, err: context.Canceled},
	}
	closePlugins(plugins, time.Second)
	// Last registered first; a panic or error does not stop the others.
	closeMu.Lock()
	defer closeMu.Unlock()
	if !reflect.DeepEqual(log, []string{"c", "b", "a"}) {
		t.Errorf("close order = %v, want [c b a]", log)
	}
}

func TestClosePluginsTimeout(t *testing.T) {
	var log []string
	start := time.Now()
	closePlugins([]sdk.Plugin{
		closerPlugin{testPlugin: testPlugin{name: "a"}, log: &log},
		closerPlugin{testPlugin: testPlugin{name: "stuck"}, log: &log, block: true},
	}, 100*time.Millisecond)
	if time.Since(start) > time.Second {
		t.Errorf("closePlugins did not give up")
	}
}
