// Command plugin is the plugin binary used by vim_e2e_test.go: one plugin
// ("test") whose methods probe the transport and the host API from inside a
// real Vim.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Shougo/goda.vim/host"
	"github.com/Shougo/goda.vim/sdk"
)

type testPlugin struct{}

func (testPlugin) Name() string { return "test" }

// Close records that the host told the plugin to stop (sdk.Closer).
func (testPlugin) Close(context.Context) error {
	if path := os.Getenv("GODA_CLOSE_FILE"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		fmt.Fprintln(f, "closed")
	}
	return nil
}

func (testPlugin) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		"echo": func(_ context.Context, _ sdk.Host, p []any) (any, error) { return p, nil },

		"meta": func(_ context.Context, h sdk.Host, _ []any) (any, error) { return h.Meta().Host, nil },

		// The plugin calls back into this Vim while Vim waits for the answer.
		"callback": func(ctx context.Context, h sdk.Host, _ []any) (any, error) {
			return h.Call(ctx, "getcompletion", "strl", "function")
		},

		// Exercises Eval, Cmd and Call, including their error paths, and
		// reports what it saw.
		"probe": probe,

		// Notify: run without an answer, in order with the calls that
		// follow; an error is kept for goda#log().
		"notify": func(ctx context.Context, h sdk.Host, _ []any) (any, error) {
			if err := h.Notify(ctx, "execute", "let g:goda_notified = 'yes'"); err != nil {
				return nil, err
			}
			if err := h.Notify(ctx, "goda_no_such_function"); err != nil {
				return nil, err
			}
			return h.Eval(ctx, "get(g:, 'goda_notified', 'not yet')")
		},

		// stdout carries the protocol and must stay clean.
		"shout": func(context.Context, sdk.Host, []any) (any, error) {
			fmt.Println("this goes to stdout")
			return "shout", nil
		},

		// Answers after Vim's request timeout.
		"slow": func(context.Context, sdk.Host, []any) (any, error) {
			time.Sleep(2 * time.Second)
			return "late", nil
		},

		// A struct argument: its json tags are what Vim sees.
		"struct": func(ctx context.Context, h sdk.Host, _ []any) (any, error) {
			type item struct {
				Word string `json:"word"`
				Kind string `json:"kind"`
			}
			return h.Call(ctx, "get", map[string]any{"x": []item{{"w", "k"}}}, "x")
		},
	}
}

func probe(ctx context.Context, h sdk.Host, _ []any) (any, error) {
	var out []any
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	mentions := func(err error, s string) bool { return err != nil && strings.Contains(err.Error(), s) }

	v, err := h.Eval(ctx, "1 + 2")
	add("eval=%v:%T:%v", v, v, err)

	err = h.Cmd(ctx, "let g:goda_probe = 42")
	v, err2 := h.Eval(ctx, "g:goda_probe")
	add("cmd=%v:%v:%v", err, v, err2)

	add("cmd-error=%v", mentions(h.Cmd(ctx, "throw 'boom'"), "boom"))

	_, err = h.Call(ctx, "no_such_function_xyz")
	add("call-error=%v", mentions(err, "E117"))

	v, err = h.Eval(ctx, "[1, 2.5, 'x', v:true, v:null]")
	if arr, ok := v.([]any); ok && err == nil && len(arr) == 5 {
		add("types=%T,%T,%T,%T,%T", arr[0], arr[1], arr[2], arr[3], arr[4])
	} else {
		add("types=unexpected %v %v", v, err)
	}

	// "ERROR" is what Vim answers for a failed call; a function that really
	// returns that string must not be mistaken for a failure.
	v, err = h.Call(ctx, "toupper", "error")
	add("error-string=%v:%v", v, err)
	return out, nil
}

func main() { host.Main(testPlugin{}) }
