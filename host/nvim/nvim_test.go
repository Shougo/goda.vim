package nvim

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/Shougo/goda.vim/internal/msgpack"
	"github.com/Shougo/goda.vim/sdk"
)

// testTable is a handful of handlers that exercise the transport.
func testTable() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		"t.echo": func(_ context.Context, _ sdk.Host, p []any) (any, error) { return p, nil },
		"t.meta": func(_ context.Context, h sdk.Host, _ []any) (any, error) { return h.Meta().Host, nil },
		// Calls back into the editor and returns what it answered.
		"t.call": func(ctx context.Context, h sdk.Host, p []any) (any, error) {
			return h.Call(ctx, p[0].(string), p[1:]...)
		},
		// Passes a struct to the editor: the transport must make it encodable.
		"t.struct": func(ctx context.Context, h sdk.Host, _ []any) (any, error) {
			type arg struct {
				N    int    `json:"n"`
				Word string `json:"word"`
			}
			return h.Call(ctx, "f", arg{N: 5, Word: "w"})
		},
		"t.cmd": func(ctx context.Context, h sdk.Host, p []any) (any, error) {
			return nil, h.Cmd(ctx, p[0].(string))
		},
		"t.eval": func(ctx context.Context, h sdk.Host, p []any) (any, error) {
			return h.Eval(ctx, p[0].(string))
		},
		"t.fail": func(context.Context, sdk.Host, []any) (any, error) { return nil, fmt.Errorf("deliberate") },
		"t.boom": func(context.Context, sdk.Host, []any) (any, error) { panic("kaboom") },
	}
}

// fakeEditor plays Neovim's side of the wire. It is written against the
// msgpack-rpc spec, not against a real Neovim, so it proves the protocol logic
// (framing, ids, nested requests) but not Neovim compatibility.
type fakeEditor struct {
	t   *testing.T
	to  io.WriteCloser // editor -> plugin
	dec *msgpack.Decoder
}

func startPlugin(t *testing.T, table map[string]sdk.Handler) *fakeEditor {
	t.Helper()
	toPluginR, toPluginW := io.Pipe()
	fromPluginR, fromPluginW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := ServeIO(context.Background(), toPluginR, fromPluginW, table, t.Logf)
		fromPluginW.Close()
		done <- err
	}()
	t.Cleanup(func() {
		toPluginW.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("plugin did not stop after the editor closed the channel")
		}
	})
	return &fakeEditor{t: t, to: toPluginW, dec: msgpack.NewDecoder(fromPluginR)}
}

func (e *fakeEditor) send(v any) {
	e.t.Helper()
	b, err := msgpack.Encode(v)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.to.Write(b); err != nil {
		e.t.Fatal(err)
	}
}

func (e *fakeEditor) recv() []any {
	e.t.Helper()
	type r struct {
		v   any
		err error
	}
	ch := make(chan r, 1)
	go func() { v, err := e.dec.Decode(); ch <- r{v, err} }()
	select {
	case x := <-ch:
		if x.err != nil {
			e.t.Fatal(x.err)
		}
		msg, ok := x.v.([]any)
		if !ok {
			e.t.Fatalf("not a message: %v", x.v)
		}
		return msg
	case <-time.After(5 * time.Second):
		e.t.Fatal("timed out waiting for the plugin")
		return nil
	}
}

// response checks the header of [1, id, error, result] and returns the rest.
func response(t *testing.T, msg []any, id int64) (errVal, result any) {
	t.Helper()
	if len(msg) != 4 || msg[0] != int64(1) || msg[1] != id {
		t.Fatalf("want a response to request %d, got %v", id, msg)
	}
	return msg[2], msg[3]
}

func TestEcho(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send([]any{0, 7, "t.echo", []any{1, "two", []any{3.5, nil}}})
	e, res := response(t, ed.recv(), 7)
	if e != nil {
		t.Fatalf("unexpected error %v", e)
	}
	if want := []any{int64(1), "two", []any{3.5, nil}}; !reflect.DeepEqual(res, want) {
		t.Fatalf("got %v, want %v", res, want)
	}
}

func TestMeta(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send([]any{0, 1, "t.meta", []any{}})
	if _, res := response(t, ed.recv(), 1); res != "nvim" {
		t.Fatalf("meta = %v", res)
	}
}

// The important case: the editor blocks in rpcrequest() while the handler
// calls back into the editor before the final response is sent.
func TestNestedHostCall(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send([]any{0, 1, "t.call", []any{"getcompletion", "str", "function"}})

	// 1. The plugin asks the editor.
	req := ed.recv()
	if req[0] != int64(0) || req[2] != "nvim_call_function" {
		t.Fatalf("expected nvim_call_function request, got %v", req)
	}
	if want := []any{"getcompletion", []any{"str", "function"}}; !reflect.DeepEqual(req[3], want) {
		t.Fatalf("params = %v, want %v", req[3], want)
	}
	// 2. The editor answers.
	ed.send([]any{1, req[1], nil, []any{"strlen", "strpart"}})
	// 3. The original request completes with that answer.
	e, res := response(t, ed.recv(), 1)
	if e != nil || !reflect.DeepEqual(res, []any{"strlen", "strpart"}) {
		t.Fatalf("got %v, %v", e, res)
	}
}

func TestStructArgumentsAreEncodable(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send([]any{0, 1, "t.struct", []any{}})
	req := ed.recv()
	want := []any{"f", []any{map[string]any{"n": int64(5), "word": "w"}}}
	if !reflect.DeepEqual(req[3], want) {
		t.Fatalf("params = %v, want %v", req[3], want)
	}
	ed.send([]any{1, req[1], nil, int64(1)})
	response(t, ed.recv(), 1)
}

func TestEvalAndCmd(t *testing.T) {
	ed := startPlugin(t, testTable())

	ed.send([]any{0, 1, "t.eval", []any{"1+2"}})
	req := ed.recv()
	if req[2] != "nvim_eval" || !reflect.DeepEqual(req[3], []any{"1+2"}) {
		t.Fatalf("got %v", req)
	}
	ed.send([]any{1, req[1], nil, int64(3)})
	if _, res := response(t, ed.recv(), 1); res != int64(3) {
		t.Fatalf("eval = %v", res)
	}

	ed.send([]any{0, 2, "t.cmd", []any{"let g:x = 1"}})
	req = ed.recv()
	if req[2] != "nvim_command" {
		t.Fatalf("got %v", req)
	}
	ed.send([]any{1, req[1], nil, nil})
	if e, _ := response(t, ed.recv(), 2); e != nil {
		t.Fatalf("cmd failed: %v", e)
	}
}

func TestEditorErrorReachesTheHandler(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send([]any{0, 2, "t.call", []any{"nope"}})
	req := ed.recv()
	ed.send([]any{1, req[1], []any{0, "E117: Unknown function: nope"}, nil})
	e, _ := response(t, ed.recv(), 2)
	if e == nil {
		t.Fatal("the handler's failure must reach the editor as an error response")
	}
}

func TestHandlerFailures(t *testing.T) {
	ed := startPlugin(t, testTable())
	for id, method := range map[int64]string{3: "t.fail", 4: "t.boom", 5: "nope"} {
		ed.send([]any{0, id, method, []any{}})
		if e, _ := response(t, ed.recv(), id); e == nil {
			t.Errorf("%s must produce an error response", method)
		}
	}
	// The plugin is still alive afterwards.
	ed.send([]any{0, 6, "t.meta", []any{}})
	if _, res := response(t, ed.recv(), 6); res != "nvim" {
		t.Fatalf("plugin died after a failing handler: %v", res)
	}
}

// Regression: a request followed immediately by EOF (`printf ... | plugin`)
// must still be answered before the plugin exits.
func TestResponseSurvivesEOF(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send([]any{0, 9, "t.meta", []any{}})
	ed.to.Close() // the editor goes away right after asking
	if e, res := response(t, ed.recv(), 9); e != nil || res != "nvim" {
		t.Fatalf("got %v, %v", e, res)
	}
}
