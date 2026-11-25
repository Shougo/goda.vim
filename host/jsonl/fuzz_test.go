package jsonl

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// FuzzServe feeds arbitrary lines: ServeIO must not panic, and it must return
// once the input ends.
func FuzzServe(f *testing.F) {
	for _, s := range []string{
		`{"method":"t.echo","params":[1,"two"]}`,
		`{"method":"t.call"}`,
		`{"method":"t.fail"}` + "\n" + `{"method":"t.boom"}`,
		`{"method":"t.echo","params":"x"}`,
		`{"params":[]}`,
		`not json`,
	} {
		f.Add([]byte(s + "\n"))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = ServeIO(context.Background(), bytes.NewReader(b), io.Discard, table())
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("ServeIO did not return at the end of %q", b)
		}
	})
}
