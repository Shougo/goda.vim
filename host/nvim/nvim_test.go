package nvim

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/Shougo/goda.vim/ext/filter/matcher_head"
	"github.com/Shougo/goda.vim/ext/source/vim"
	"github.com/Shougo/goda.vim/internal/msgpack"
	"github.com/Shougo/goda.vim/sdk"
)

// fakeEditor plays Neovim's side of the wire. It is written against the
// msgpack-rpc spec, not against a real Neovim, so it proves the protocol logic
// (framing, ids, nested requests) but not Neovim compatibility.
type fakeEditor struct {
	t   *testing.T
	to  io.WriteCloser // editor -> plugin
	dec *msgpack.Decoder
}

func startPlugin(t *testing.T, reg *sdk.Registry) *fakeEditor {
	t.Helper()
	toPluginR, toPluginW := io.Pipe()
	fromPluginR, fromPluginW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := ServeIO(context.Background(), toPluginR, fromPluginW, reg, t.Logf)
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

func TestList(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("vim", vim.New())
	reg.RegisterFilter("matcher_head", matcherhead.New())
	ed := startPlugin(t, reg)

	ed.send([]any{0, 7, "goda_list", []any{}})
	msg := ed.recv()
	if msg[0] != int64(1) || msg[1] != int64(7) || msg[2] != nil {
		t.Fatalf("unexpected response header: %v", msg)
	}
	want := map[string]any{
		"sources": []any{"vim"},
		"filters": []any{"matcher_head"},
	}
	if !reflect.DeepEqual(msg[3], want) {
		t.Fatalf("got %v, want %v", msg[3], want)
	}
}

// The important case: the editor blocks in rpcrequest(goda_gather) while the
// extension calls back into the editor (getcompletion) before the final
// response is sent.
func TestGatherWithNestedHostCall(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("vim", vim.New())
	reg.RegisterFilter("matcher_head", matcherhead.New())
	ed := startPlugin(t, reg)

	ed.send([]any{0, 1, "goda_gather", []any{map[string]any{
		"sources": []any{"vim"},
		"filters": []any{"matcher_head"},
		"context": map[string]any{"complete_str": "str"},
	}}})

	// 1. The plugin asks the editor for completions.
	req := ed.recv()
	if req[0] != int64(0) || req[2] != "nvim_call_function" {
		t.Fatalf("expected nvim_call_function request, got %v", req)
	}
	params := req[3].([]any)
	if params[0] != "getcompletion" {
		t.Fatalf("unexpected function %v", params[0])
	}
	// 2. The editor answers (the second one is filtered out by matcher_head).
	ed.send([]any{1, req[1], nil, []any{"strlen", "strpart", "substitute"}})

	// 3. The original request completes.
	resp := ed.recv()
	if resp[0] != int64(1) || resp[1] != int64(1) || resp[2] != nil {
		t.Fatalf("unexpected response: %v", resp)
	}
	res := resp[3].(map[string]any)
	cands := res["candidates"].([]any)
	if len(cands) != 2 {
		t.Fatalf("want 2 candidates, got %v", cands)
	}
	if w := cands[0].(map[string]any)["word"]; w != "strlen" {
		t.Fatalf("first candidate = %v", w)
	}
	if errs := res["errors"].([]any); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestEditorErrorBecomesExtensionError(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("vim", vim.New())
	ed := startPlugin(t, reg)

	ed.send([]any{0, 2, "goda_gather", []any{map[string]any{
		"sources": []any{"vim"},
		"context": map[string]any{"complete_str": "x"},
	}}})
	req := ed.recv()
	ed.send([]any{1, req[1], []any{0, "E117: Unknown function"}, nil})

	resp := ed.recv()
	res := resp[3].(map[string]any)
	errs := res["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("want one extension error, got %v", errs)
	}
	if res["candidates"] == nil {
		t.Fatal("candidates must still be present")
	}
}

// Regression: a request followed immediately by EOF (`printf ... | plugin`)
// must still be answered before the plugin exits.
func TestResponseSurvivesEOF(t *testing.T) {
	ed := startPlugin(t, sdk.NewRegistry())
	ed.send([]any{0, 9, "goda_list", []any{}})
	ed.to.Close() // the editor goes away right after asking

	msg := ed.recv()
	if msg[0] != int64(1) || msg[1] != int64(9) || msg[2] != nil {
		t.Fatalf("unexpected response: %v", msg)
	}
}

func TestUnknownMethodAndBadRequest(t *testing.T) {
	ed := startPlugin(t, sdk.NewRegistry())

	ed.send([]any{0, 3, "nope", []any{}})
	msg := ed.recv()
	if msg[2] == nil {
		t.Fatalf("expected an error response, got %v", msg)
	}

	ed.send([]any{0, 4, "goda_gather", []any{}})
	msg = ed.recv()
	if msg[1] != int64(4) || msg[2] == nil {
		t.Fatalf("expected an error response for the missing argument, got %v", msg)
	}
}
