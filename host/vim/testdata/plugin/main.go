// Command plugin is the plugin binary used by vim_e2e_test.go: the real Vim
// transport plus a few extensions that probe the host API.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Shougo/goda.vim/ext/filter/matcher_head"
	"github.com/Shougo/goda.vim/ext/source/around"
	vimsource "github.com/Shougo/goda.vim/ext/source/vim"
	vimhost "github.com/Shougo/goda.vim/host/vim"
	"github.com/Shougo/goda.vim/sdk"
)

// slow ignores its context on purpose, like a badly behaved extension.
type slow struct{}

func (slow) Gather(context.Context, sdk.Host, sdk.Context) ([]sdk.Candidate, error) {
	time.Sleep(3 * time.Second)
	return []sdk.Candidate{{Word: "late"}}, nil
}

// shout writes to stdout, which carries the protocol and must stay clean.
type shout struct{}

func (shout) Gather(context.Context, sdk.Host, sdk.Context) ([]sdk.Candidate, error) {
	fmt.Println("this goes to stdout")
	return []sdk.Candidate{{Word: "shout"}}, nil
}

// probe exercises Eval, Cmd and Call, including their error paths, and
// reports what it saw as candidate words.
type probe struct{}

func (probe) Gather(ctx context.Context, h sdk.Host, _ sdk.Context) ([]sdk.Candidate, error) {
	var words []string
	add := func(format string, args ...any) { words = append(words, fmt.Sprintf(format, args...)) }
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

	out := make([]sdk.Candidate, len(words))
	for i, w := range words {
		out[i] = sdk.Candidate{Word: w}
	}
	return out, nil
}

func main() {
	reg := sdk.NewRegistry()
	reg.RegisterSource("around", around.New())
	reg.RegisterSource("vim", vimsource.New())
	reg.RegisterSource("slow", slow{})
	reg.RegisterSource("shout", shout{})
	reg.RegisterSource("probe", probe{})
	reg.RegisterFilter("matcher_head", matcherhead.New())
	if err := vimhost.Serve(reg); err != nil {
		fmt.Fprintln(os.Stderr, "goda:", err)
		os.Exit(1)
	}
}
