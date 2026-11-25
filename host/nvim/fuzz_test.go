package nvim

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/Shougo/goda.vim/internal/msgpack"
)

// FuzzServe feeds arbitrary bytes as Neovim's side of msgpack-rpc: ServeIO
// must not panic, and it must return once the input ends, even with a
// handler waiting for an answer from Neovim that never comes (t.call).
func FuzzServe(f *testing.F) {
	for _, msgs := range [][]any{
		{[]any{0, 7, "t.echo", []any{1, "two", []any{3.5, nil}}}},
		{[]any{0, 1, "t.meta", []any{}}},
		{[]any{0, 1, "t.call", []any{"getcompletion", "str", "function"}}},
		{[]any{1, -1, nil, []any{"strlen"}}},
		{[]any{2, "t.echo", []any{1}}},
		{[]any{0, 1, "t.fail", []any{}}, []any{0, 2, "t.boom", []any{}}},
		{[]any{0, 1, "t.echo"}},
		{[]any{0, "x", "t.echo", []any{}}},
	} {
		var buf bytes.Buffer
		for _, m := range msgs {
			b, err := msgpack.Encode(m)
			if err != nil {
				f.Fatal(err)
			}
			buf.Write(b)
		}
		f.Add(buf.Bytes())
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = ServeIO(ctx, bytes.NewReader(b), io.Discard, testTable(), func(string, ...any) {})
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("ServeIO did not return at the end of %x", b)
		}
	})
}
