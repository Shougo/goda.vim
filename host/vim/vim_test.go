package vim

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

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
		// Notifies, then calls: the notification must come first.
		"t.notify": func(ctx context.Context, h sdk.Host, p []any) (any, error) {
			if err := h.Notify(ctx, "setline", 1, "x"); err != nil {
				return nil, err
			}
			return h.Call(ctx, "line", ".")
		},
		"t.fail": func(context.Context, sdk.Host, []any) (any, error) { return nil, fmt.Errorf("deliberate") },
		"t.boom": func(context.Context, sdk.Host, []any) (any, error) { panic("kaboom") },
	}
}

// fakeEditor plays Vim's side of the wire, written from the format verified
// against a real Vim (see the package comment and vim_e2e_test.go). It proves
// the protocol logic (framing, ids, nested calls), not Vim compatibility;
// vim_e2e_test.go covers that.
type fakeEditor struct {
	t   *testing.T
	to  io.WriteCloser // editor -> plugin
	dec *json.Decoder
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

func TestEcho(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send(`[7, ["t.echo", 1, "two", [3.5, null], {"k": true}]]`)
	res, errMsg := envelope(t, ed.recv(), "7")
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	got, _ := json.Marshal(res)
	if string(got) != `[1,"two",[3.5,null],{"k":true}]` {
		t.Fatalf("got %s", got)
	}
}

func TestMeta(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send(`[1, ["t.meta"]]`)
	if res, _ := envelope(t, ed.recv(), "1"); res != "vim" {
		t.Fatalf("meta = %v", res)
	}
}

// The important case: Vim blocks in ch_evalexpr() while the handler calls back
// into Vim before the final answer is sent.
func TestNestedHostCall(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send(`[1, ["t.call", "getcompletion", "str", "function"]]`)

	// 1. The plugin asks Vim, through the error-catching helper.
	call := ed.recv()
	if call[0] != "call" || call[1] != "goda#_call" {
		t.Fatalf("expected a call of goda#_call, got %v", call)
	}
	if id := call[3].(json.Number).String(); id != "-1" {
		t.Fatalf("the plugin's own ids must be negative, got %s", id)
	}
	if want := []any{"getcompletion", []any{"str", "function"}}; !reflect.DeepEqual(call[2], want) {
		t.Fatalf("args = %v, want %v", call[2], want)
	}
	// 2. Vim answers.
	ed.send(`[-1, [0, ["strlen", "strpart"]]]`)
	// 3. The original request completes with that answer.
	res, errMsg := envelope(t, ed.recv(), "1")
	if errMsg != "" || !reflect.DeepEqual(res, []any{"strlen", "strpart"}) {
		t.Fatalf("got %v, %q", res, errMsg)
	}
}

func TestStructArgumentsAreEncodable(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send(`[1, ["t.struct"]]`)
	call := ed.recv()
	got, _ := json.Marshal(call[2])
	if string(got) != `["f",[{"n":5,"word":"w"}]]` {
		t.Fatalf("args = %s", got)
	}
	ed.send(`[-1, [0, 1]]`)
	envelope(t, ed.recv(), "1")
}

func TestEvalAndCmd(t *testing.T) {
	ed := startPlugin(t, testTable())

	ed.send(`[1, ["t.eval", "1+2"]]`)
	call := ed.recv()
	if call[1] != "goda#_eval" {
		t.Fatalf("got %v", call)
	}
	ed.send(`[-1, [0, 3]]`)
	if res, _ := envelope(t, ed.recv(), "1"); res.(json.Number).String() != "3" {
		t.Fatalf("eval = %v", res)
	}

	ed.send(`[2, ["t.cmd", "let g:x = 1"]]`)
	if call = ed.recv(); call[1] != "goda#_cmd" {
		t.Fatalf("got %v", call)
	}
	ed.send(`[-2, [0, null]]`)
	if _, errMsg := envelope(t, ed.recv(), "2"); errMsg != "" {
		t.Fatalf("cmd failed: %s", errMsg)
	}
}

func TestEditorFailuresReachTheHandler(t *testing.T) {
	tests := []struct{ name, answer, want string }{
		{"exception", `[-1, [1, "E117: Unknown function: x"]]`, "E117"},
		// What Vim sends when it cannot call the helper at all.
		{"helper missing", `[-1, "ERROR"]`, "runtimepath"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := startPlugin(t, testTable())
			ed.send(`[2, ["t.call", "x"]]`)
			ed.recv() // the call
			ed.send(tt.answer)
			if _, errMsg := envelope(t, ed.recv(), "2"); !strings.Contains(errMsg, tt.want) {
				t.Fatalf("want an error mentioning %q, got %q", tt.want, errMsg)
			}
		})
	}
}

func TestNotificationGetsNoAnswer(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send(`[0, ["t.meta"]]`) // id 0: nobody is waiting
	ed.send(`[5, ["t.meta"]]`)
	// The first message out must be the answer to 5, not to the notification.
	if _, errMsg := envelope(t, ed.recv(), "5"); errMsg != "" {
		t.Fatal(errMsg)
	}
}

func TestBadRequests(t *testing.T) {
	ed := startPlugin(t, testTable())
	for id, line := range map[string]string{
		"3":  `[3, ["nope"]]`,     // unknown method
		"4":  `[4, "not a list"]`, // malformed
		"8":  `[8, []]`,           // empty
		"9":  `[9, [42]]`,         // method is not a string
		"10": `[10, ["t.fail"]]`,  // handler error
		"11": `[11, ["t.boom"]]`,  // handler panic
		"12": `[12, ["t.call"]]`,  // handler panics on missing params
	} {
		ed.send(line)
		if _, errMsg := envelope(t, ed.recv(), id); errMsg == "" {
			t.Errorf("request %s must produce an error", id)
		}
	}
	// Still alive afterwards.
	ed.send(`[13, ["t.meta"]]`)
	if res, _ := envelope(t, ed.recv(), "13"); res != "vim" {
		t.Fatalf("plugin died after failing requests: %v", res)
	}
}

// Regression: a request followed immediately by EOF must still be answered
// before the plugin exits.
func TestAnswerSurvivesEOF(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send(`[9, ["t.meta"]]`)
	ed.to.Close()
	if res, errMsg := envelope(t, ed.recv(), "9"); errMsg != "" || res != "vim" {
		t.Fatalf("got %v, %q", res, errMsg)
	}
}

func TestNotify(t *testing.T) {
	ed := startPlugin(t, testTable())
	ed.send(`[1, ["t.notify"]]`)
	// A call command without a number: Vim answers nothing.
	if n := ed.recv(); !reflect.DeepEqual(n, []any{"call", "goda#_notify", []any{"setline", []any{json.Number("1"), "x"}}}) {
		t.Fatalf("notify sent %v", n)
	}
	call := ed.recv()
	if len(call) != 4 || call[1] != "goda#_call" {
		t.Fatalf("then got %v", call)
	}
	ed.send(fmt.Sprintf(`[%s, [0, 1]]`, call[3]))
	if res, errMsg := envelope(t, ed.recv(), "1"); errMsg != "" || res.(json.Number).String() != "1" {
		t.Fatalf("result %v, error %q", res, errMsg)
	}
}
