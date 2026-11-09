package vimscript

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/Shougo/goda.vim/sdk"
)

type host struct {
	sdk.Host
	cmds  []string
	calls []string
}

func (h *host) Cmd(_ context.Context, cmd string) error {
	h.cmds = append(h.cmds, cmd)
	return nil
}

func (h *host) Call(_ context.Context, fn string, _ ...any) (any, error) {
	h.calls = append(h.calls, fn)
	return nil, nil
}

func TestCall(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("LocalAppData", t.TempDir())
	src := []byte("function! myplugin#t#f() abort\nendfunction\n")
	s := New("autoload/myplugin/t.vim", src)
	h := &host{}
	for range 2 {
		if _, err := s.Call(context.Background(), h, "myplugin#t#f"); err != nil {
			t.Fatal(err)
		}
	}
	// Sourced once, before the calls.
	if len(h.cmds) != 2 || !strings.HasPrefix(h.cmds[0], "source ") || len(h.calls) != 2 {
		t.Fatalf("cmds %q, calls %q", h.cmds, h.calls)
	}
	file, err := s.write()
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != string(src) {
		t.Errorf("%s: %q %v", file, b, err)
	}
	if !strings.HasSuffix(strings.ReplaceAll(file, "\\", "/"), "/autoload/myplugin/t.vim") {
		t.Errorf("not an autoload path: %s", file)
	}
}

func TestFnameescape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the backslashes are separators")
	}
	if got := fnameescape("/a b/c%d#e"); got != `/a\ b/c\%d\#e` {
		t.Errorf("got %q", got)
	}
}
