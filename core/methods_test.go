package core_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Shougo/goda.vim/core"
	"github.com/Shougo/goda.vim/ext/source/around"
	"github.com/Shougo/goda.vim/sdk"
)

func newMethods() map[string]core.Handler {
	reg := sdk.NewRegistry()
	reg.RegisterSource("around", around.New())
	return core.Methods(reg, fakeHost{})
}

func candidateWords(t *testing.T, res any) []string {
	t.Helper()
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result is %T, want map", res)
	}
	var out []string
	for _, c := range m["candidates"].([]any) {
		out = append(out, c.(map[string]any)["word"].(string))
	}
	return out
}

// Params arrive in whatever form the transport's codec produced: msgpack
// gives int64, the Vim JSON transport can hand over json.Number.
func TestMethodsAcceptEitherNumberForm(t *testing.T) {
	m := newMethods()
	for name, line := range map[string]any{"int64": int64(1), "json.Number": json.Number("1")} {
		res, err := m["goda_gather"](context.Background(), []any{map[string]any{
			"sources": []any{"around"},
			"filters": []any{},
			"context": map[string]any{"complete_str": "fo", "line": line, "lines": []any{"foo"}},
		}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := candidateWords(t, res); len(got) != 1 || got[0] != "foo" {
			t.Fatalf("%s: candidates = %v", name, got)
		}
	}
}

func TestMethodsBadRequests(t *testing.T) {
	m := newMethods()
	if _, err := m["goda_gather"](context.Background(), nil); err == nil {
		t.Error("missing request must fail")
	}
	if _, err := m["goda_gather"](context.Background(), []any{"not an object"}); err == nil {
		t.Error("a non-object request must fail")
	}
}

func TestMethodsList(t *testing.T) {
	res, err := newMethods()["goda_list"](context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res)
	if string(b) != `{"filters":[],"sources":["around"]}` {
		t.Fatalf("unexpected list: %s", b)
	}
}
