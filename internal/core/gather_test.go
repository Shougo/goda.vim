package core_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Shougo/goda.vim/ext/filter/matcher_head"
	"github.com/Shougo/goda.vim/ext/filter/sorter_length"
	"github.com/Shougo/goda.vim/ext/source/around"
	"github.com/Shougo/goda.vim/ext/source/vim"
	"github.com/Shougo/goda.vim/internal/core"
	"github.com/Shougo/goda.vim/sdk"
)

type fakeHost struct{ calls map[string]any }

func (f fakeHost) Call(_ context.Context, fn string, _ ...any) (any, error) {
	v, ok := f.calls[fn]
	if !ok {
		return nil, fmt.Errorf("unexpected call to %s", fn)
	}
	return v, nil
}
func (fakeHost) Eval(context.Context, string) (any, error) { return nil, nil }
func (fakeHost) Cmd(context.Context, string) error         { return nil }

type srcFunc func(context.Context, sdk.Host, sdk.Context) ([]sdk.Candidate, error)

func (f srcFunc) Gather(ctx context.Context, h sdk.Host, c sdk.Context) ([]sdk.Candidate, error) {
	return f(ctx, h, c)
}

type filterFunc func(context.Context, sdk.Context, []sdk.Candidate) ([]sdk.Candidate, error)

func (f filterFunc) Filter(ctx context.Context, c sdk.Context, in []sdk.Candidate) ([]sdk.Candidate, error) {
	return f(ctx, c, in)
}

func words(cs []sdk.Candidate) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Word
	}
	return out
}

func TestPipeline(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("around", around.New())
	reg.RegisterSource("vim", vim.New())
	reg.RegisterFilter("matcher_head", matcherhead.New())
	reg.RegisterFilter("sorter_length", sorterlength.New())

	h := fakeHost{calls: map[string]any{"getcompletion": []any{"foldclosed"}}}
	res := core.Gather(context.Background(), reg, h, sdk.GatherRequest{
		Sources: []string{"around", "vim"},
		Filters: []string{"matcher_head", "sorter_length"},
		Context: sdk.Context{
			CompleteStr: "fo",
			Line:        1,
			Lines:       []string{"foo foobar fizz", "barista foo_baz"},
		},
	})
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", res.Errors)
	}
	want := []string{"foo", "foobar", "foo_baz", "foldclosed"}
	if got := words(res.Candidates); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPanicIsolated(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("boom", srcFunc(func(context.Context, sdk.Host, sdk.Context) ([]sdk.Candidate, error) {
		panic("kaboom")
	}))
	reg.RegisterSource("ok", srcFunc(func(context.Context, sdk.Host, sdk.Context) ([]sdk.Candidate, error) {
		return []sdk.Candidate{{Word: "fine"}}, nil
	}))
	res := core.Gather(context.Background(), reg, fakeHost{}, sdk.GatherRequest{Sources: []string{"boom", "ok"}})
	if got := words(res.Candidates); !reflect.DeepEqual(got, []string{"fine"}) {
		t.Fatalf("candidates = %v", got)
	}
	if len(res.Errors) != 1 || res.Errors[0].Name != "boom" || !strings.Contains(res.Errors[0].Message, "panic") {
		t.Fatalf("errors = %+v", res.Errors)
	}
}

func TestTimeoutDoesNotBlock(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("slow", srcFunc(func(context.Context, sdk.Host, sdk.Context) ([]sdk.Candidate, error) {
		time.Sleep(2 * time.Second) // deliberately ignores ctx
		return []sdk.Candidate{{Word: "late"}}, nil
	}))
	reg.RegisterSource("fast", srcFunc(func(context.Context, sdk.Host, sdk.Context) ([]sdk.Candidate, error) {
		return []sdk.Candidate{{Word: "quick"}}, nil
	}))

	start := time.Now()
	res := core.Gather(context.Background(), reg, fakeHost{}, sdk.GatherRequest{
		Sources: []string{"slow", "fast"}, TimeoutMs: 50,
	})
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Gather blocked for %v", d)
	}
	if got := words(res.Candidates); !reflect.DeepEqual(got, []string{"quick"}) {
		t.Fatalf("candidates = %v", got)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Message, "timed out") {
		t.Fatalf("errors = %+v", res.Errors)
	}
}

func TestUnknownAndFailingFilter(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("s", srcFunc(func(context.Context, sdk.Host, sdk.Context) ([]sdk.Candidate, error) {
		return []sdk.Candidate{{Word: "a"}, {Word: "b"}}, nil
	}))
	reg.RegisterFilter("bad", filterFunc(func(context.Context, sdk.Context, []sdk.Candidate) ([]sdk.Candidate, error) {
		return nil, fmt.Errorf("nope")
	}))
	res := core.Gather(context.Background(), reg, fakeHost{}, sdk.GatherRequest{
		Sources: []string{"s", "missing"},
		Filters: []string{"bad", "missing"},
	})
	// A failing filter is skipped: its input flows on unchanged.
	if got := words(res.Candidates); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("candidates = %v", got)
	}
	if len(res.Errors) != 3 {
		t.Fatalf("want 3 errors (missing source, bad filter, missing filter), got %+v", res.Errors)
	}
}

func TestEmptyResultIsNotNil(t *testing.T) {
	res := core.Gather(context.Background(), sdk.NewRegistry(), fakeHost{}, sdk.GatherRequest{})
	if res.Candidates == nil || res.Errors == nil {
		t.Fatal("slices must be non-nil so they encode as empty arrays")
	}
}
