package jsonl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Shougo/goda.vim/sdk"
)

func table() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		"t.echo": func(_ context.Context, _ sdk.Host, p []any) (any, error) { return p, nil },
		"t.meta": func(_ context.Context, h sdk.Host, _ []any) (any, error) { return h.Meta().Host, nil },
		"t.call": func(ctx context.Context, h sdk.Host, _ []any) (any, error) { return h.Call(ctx, "strlen", "x") },
		"t.fail": func(context.Context, sdk.Host, []any) (any, error) { return nil, fmt.Errorf("deliberate") },
		"t.boom": func(context.Context, sdk.Host, []any) (any, error) { panic("kaboom") },
		"t.bad":  func(context.Context, sdk.Host, []any) (any, error) { return make(chan int), nil },
	}
}

func run(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := ServeIO(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, table()); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("answer %q is not JSON: %v", l, err)
		}
		res = append(res, m)
	}
	if len(res) != len(lines) {
		t.Fatalf("%d requests, %d answers: %s", len(lines), len(res), out.String())
	}
	return res
}

func TestServe(t *testing.T) {
	r := run(t,
		`{"method":"t.echo","params":[1,"two",[3.5]]}`,
		`{"method":"t.meta"}`,
	)
	if got, _ := json.Marshal(r[0]["result"]); string(got) != `[1,"two",[3.5]]` {
		t.Errorf("echo = %s", got)
	}
	if r[1]["result"] != "none" {
		t.Errorf("meta = %v", r[1])
	}
}

func TestErrors(t *testing.T) {
	r := run(t,
		`{"method":"t.call"}`,
		`{"method":"t.fail"}`,
		`{"method":"t.boom"}`,
		`{"method":"t.bad"}`,
		`{"method":"nope"}`,
		`not json`,
		`{"params":[]}`,
		`{"method":"t.echo","params":"x"}`,
		`{"method":"t.echo"}`,
	)
	want := []string{"no editor host", "deliberate", "panic", "unsupported type", "unknown method", "malformed", "malformed", "malformed", ""}
	for i, w := range want {
		msg, isErr := r[i]["error"].(string)
		if w == "" {
			if isErr {
				t.Errorf("request %d must succeed, got %q", i, msg)
			}
			continue
		}
		if !isErr || !strings.Contains(msg, w) {
			t.Errorf("request %d: want an error mentioning %q, got %v", i, w, r[i])
		}
	}
}
