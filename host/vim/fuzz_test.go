package vim

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// FuzzServe feeds arbitrary bytes as Vim's side of the channel: ServeIO must
// not panic, and it must return once the input ends, even with a handler
// waiting for an answer from Vim that never comes (t.call).
func FuzzServe(f *testing.F) {
	for _, s := range []string{
		`[7, ["t.echo", 1, "two", [3.5, null], {"k": true}]]`,
		`[1, ["t.meta"]]`,
		`[1, ["t.call", "getcompletion", "str", "function"]]`,
		`[-1, [0, ["strlen", "strpart"]]]`,
		`[1, ["t.fail"]]` + "\n" + `[2, ["t.boom"]]`,
		`[0, ["t.echo"]]`,
		`[1, "t.echo"]`,
		`[1, [null]]`,
		`not json`,
	} {
		f.Add([]byte(s + "\n"))
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
			t.Fatalf("ServeIO did not return at the end of %q", b)
		}
	})
}
