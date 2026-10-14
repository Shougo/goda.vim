package vim

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Shougo/goda.vim/ext/filter/matcher_head"
	vimsource "github.com/Shougo/goda.vim/ext/source/vim"
	"github.com/Shougo/goda.vim/sdk"
)

// fakeEditor plays Vim's side of the wire, written from the format verified
// against a real Vim (see the package comment and vim_e2e_test.go). It proves
// the protocol logic (framing, ids, nested calls), not Vim compatibility;
// vim_e2e_test.go covers that.
type fakeEditor struct {
	t   *testing.T
	to  io.WriteCloser // editor -> plugin
	dec *json.Decoder
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
	dec := json.NewDecoder(fromPluginR)
	dec.UseNumber()
	return &fakeEditor{t: t, to: toPluginW, dec: dec}
}

func (e *fakeEditor) send(line string) {
	e.t.Helper()
	if _, err := io.WriteString(e.to, line+"\n"); err != nil {
		e.t.Fatal(err)
	}
}

func (e *fakeEditor) recv() []any {
	e.t.Helper()
	type r struct {
		v   []any
		err error
	}
	ch := make(chan r, 1)
	go func() {
		var v []any
		err := e.dec.Decode(&v)
		ch <- r{v, err}
	}()
	select {
	case x := <-ch:
		if x.err != nil {
			e.t.Fatal(x.err)
		}
		return x.v
	case <-time.After(5 * time.Second):
		e.t.Fatal("timed out waiting for the plugin")
		return nil
	}
}

// envelope unpacks [id, {"result": ...}] or [id, {"error": ...}].
func envelope(t *testing.T, msg []any, wantID string) (result any, errMsg string) {
	t.Helper()
	if len(msg) != 2 || msg[0].(json.Number).String() != wantID {
		t.Fatalf("want an answer to request %s, got %v", wantID, msg)
	}
	env := msg[1].(map[string]any)
	if e, ok := env["error"]; ok {
		return nil, e.(string)
	}
	return env["result"], ""
}

func TestList(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("vim", vimsource.New())
	reg.RegisterFilter("matcher_head", matcherhead.New())
	ed := startPlugin(t, reg)

	ed.send(`[7, ["goda_list"]]`)
	res, errMsg := envelope(t, ed.recv(), "7")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	want := map[string]any{"sources": []any{"vim"}, "filters": []any{"matcher_head"}}
	if !reflect.DeepEqual(res, want) {
		t.Fatalf("got %v, want %v", res, want)
	}
}

// The important case: Vim blocks in ch_evalexpr(goda_gather) while the
// extension calls back into Vim before the final answer is sent.
func TestGatherWithNestedHostCall(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("vim", vimsource.New())
	reg.RegisterFilter("matcher_head", matcherhead.New())
	ed := startPlugin(t, reg)

	ed.send(`[1, ["goda_gather", {"sources":["vim"],"filters":["matcher_head"],"context":{"complete_str":"str"}}]]`)

	// 1. The plugin asks Vim for completions, through the error-catching helper.
	call := ed.recv()
	if call[0] != "call" || call[1] != "goda#_call" {
		t.Fatalf("expected a call of goda#_call, got %v", call)
	}
	if id := call[3].(json.Number).String(); id != "-1" {
		t.Fatalf("the plugin's own ids must be negative, got %s", id)
	}
	args := call[2].([]any) // [fn, [args...]]
	if args[0] != "getcompletion" {
		t.Fatalf("unexpected function %v", args[0])
	}
	// 2. Vim answers (substitute is dropped by matcher_head).
	ed.send(`[-1, [0, ["strlen", "strpart", "substitute"]]]`)

	// 3. The original request completes.
	res, errMsg := envelope(t, ed.recv(), "1")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	m := res.(map[string]any)
	cands := m["candidates"].([]any)
	if len(cands) != 2 || cands[0].(map[string]any)["word"] != "strlen" {
		t.Fatalf("candidates = %v", cands)
	}
	if errs := m["errors"].([]any); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestEditorErrorsBecomeExtensionErrors(t *testing.T) {
	tests := []struct{ name, answer, want string }{
		{"exception", `[-1, [1, "E117: Unknown function: x"]]`, "E117"},
		// What Vim sends when it cannot call the helper at all.
		{"helper missing", `[-1, "ERROR"]`, "runtimepath"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := sdk.NewRegistry()
			reg.RegisterSource("vim", vimsource.New())
			ed := startPlugin(t, reg)

			ed.send(`[2, ["goda_gather", {"sources":["vim"],"context":{"complete_str":"x"}}]]`)
			ed.recv() // the call
			ed.send(tt.answer)

			res, errMsg := envelope(t, ed.recv(), "2")
			if errMsg != "" {
				t.Fatalf("the request itself must succeed: %s", errMsg)
			}
			errs := res.(map[string]any)["errors"].([]any)
			if len(errs) != 1 || !strings.Contains(errs[0].(map[string]any)["message"].(string), tt.want) {
				t.Fatalf("want one extension error mentioning %q, got %v", tt.want, errs)
			}
		})
	}
}

func TestNotificationGetsNoAnswer(t *testing.T) {
	ed := startPlugin(t, sdk.NewRegistry())
	ed.send(`[0, ["goda_list"]]`) // id 0: nobody is waiting
	ed.send(`[5, ["goda_list"]]`)
	// The first message out must be the answer to 5, not to the notification.
	if _, errMsg := envelope(t, ed.recv(), "5"); errMsg != "" {
		t.Fatal(errMsg)
	}
}

func TestBadRequests(t *testing.T) {
	ed := startPlugin(t, sdk.NewRegistry())
	for id, line := range map[string]string{
		"3": `[3, ["nope"]]`,        // unknown method
		"4": `[4, "not a list"]`,    // malformed
		"6": `[6, ["goda_gather"]]`, // missing argument
		"8": `[8, []]`,              // empty
		"9": `[9, [42]]`,            // method is not a string
	} {
		ed.send(line)
		if _, errMsg := envelope(t, ed.recv(), id); errMsg == "" {
			t.Errorf("request %s must produce an error", id)
		}
	}
}

// Regression: a request followed immediately by EOF must still be answered
// before the plugin exits.
func TestAnswerSurvivesEOF(t *testing.T) {
	ed := startPlugin(t, sdk.NewRegistry())
	ed.send(`[9, ["goda_list"]]`)
	ed.to.Close()
	if _, errMsg := envelope(t, ed.recv(), "9"); errMsg != "" {
		t.Fatal(errMsg)
	}
}

func TestDecodeNormalizesNumbers(t *testing.T) {
	got, err := decode([]byte(`[1, 2.5, -3, {"a": [4]}, "s", true, null]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []any{int64(1), 2.5, int64(-3), map[string]any{"a": []any{int64(4)}}, "s", true, nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
