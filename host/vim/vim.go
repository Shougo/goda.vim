// Package vim is the Vim transport: Vim's JSON channel over stdio. Vim starts
// the plugin binary with job_start(..., {'mode': 'json'}) and talks to it with
// ch_evalexpr(); the plugin calls back with the channel "call" command.
//
// Wire format, one JSON array per line. This was checked against a real Vim
// (see vim_e2e_test.go), not just the documentation:
//
//	Vim -> plugin   [N, [method, params...]]     N > 0: a request, answer it
//	                                             N = 0: a notification, no answer
//	plugin -> Vim   [N, {"result": v}]           answer to request N
//	                [N, {"error": "message"}]
//	plugin -> Vim   ["call", fn, [args], -K]     ask Vim to call a function;
//	Vim -> plugin   [-K, value]                  Vim answers with the same id
//
// The plugin numbers its own calls -1, -2, ... so they can never collide with
// the positive ids Vim uses for its requests. Vim keeps executing "call"
// commands while it is blocked in ch_evalexpr(), which is what makes an
// extension calling back into the editor mid-request work.
//
// Vim reports a failed call (unknown function, bad arguments) as the plain
// string "ERROR", indistinguishable from a function that returns "ERROR".
// So host calls go through three helpers in autoload/goda.vim
// (goda#_call, goda#_eval, goda#_cmd) that catch errors and answer with
// [0, result] or [1, message].
//
// The methods served to the editor are defined once, in core.Methods.
package vim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/Shougo/goda.vim/internal/core"
	"github.com/Shougo/goda.vim/sdk"
)

var errClosed = errors.New("vim: channel closed")

// Host implements sdk.Host on top of Vim's channel commands.
type Host struct{ c *conn }

func (h *Host) Call(ctx context.Context, fn string, args ...any) (any, error) {
	if args == nil {
		args = []any{}
	}
	return h.c.helper(ctx, "goda#_call", fn, args)
}

func (h *Host) Eval(ctx context.Context, expr string) (any, error) {
	return h.c.helper(ctx, "goda#_eval", expr)
}

func (h *Host) Cmd(ctx context.Context, cmd string) error {
	_, err := h.c.helper(ctx, "goda#_cmd", cmd)
	return err
}

// Serve runs the plugin over stdin/stdout until Vim closes the channel.
// stdout carries the protocol, so the process-wide os.Stdout is redirected to
// stderr: a stray fmt.Println in an extension cannot corrupt the stream.
func Serve(reg *sdk.Registry) error {
	in, out := os.Stdin, os.Stdout
	os.Stdout = os.Stderr
	logger := log.New(os.Stderr, "goda: ", log.LstdFlags)
	return ServeIO(context.Background(), in, out, reg, logger.Printf)
}

// ServeIO is Serve over arbitrary streams (used by tests).
func ServeIO(ctx context.Context, r io.Reader, w io.Writer, reg *sdk.Registry, logf func(string, ...any)) error {
	c := newConn(r, w, logf)
	host := &Host{c: c}
	for name, fn := range core.Methods(reg, host) {
		c.handlers[name] = fn
	}
	return c.serve(ctx)
}

type reply struct {
	value any
	err   error
}

// conn is one JSON channel. Requests from Vim run in their own goroutines, so
// a handler may call back into Vim while Vim waits for that very handler.
type conn struct {
	dec      *json.Decoder
	w        io.Writer
	logf     func(format string, args ...any)
	handlers map[string]core.Handler

	wmu      sync.Mutex     // serialises writes
	inflight sync.WaitGroup // handlers still running; serve waits for them

	mu      sync.Mutex // guards the fields below
	last    int64      // last id handed out; ids count down from -1
	pending map[int64]chan reply
	closed  bool
	done    chan struct{}
}

func newConn(r io.Reader, w io.Writer, logf func(string, ...any)) *conn {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &conn{
		dec:      json.NewDecoder(r),
		w:        w,
		logf:     logf,
		handlers: map[string]core.Handler{},
		pending:  map[int64]chan reply{},
		done:     make(chan struct{}),
	}
}

// serve reads messages until Vim closes the stream. Requests that were already
// received are still answered before it returns.
func (c *conn) serve(ctx context.Context) error {
	defer func() {
		c.shutdown()
		c.inflight.Wait()
	}()
	for {
		var msg []json.RawMessage
		if err := c.dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err // malformed JSON: the stream cannot be resynchronised
		}
		if len(msg) != 2 {
			c.logf("ignoring malformed message with %d elements", len(msg))
			continue
		}
		var id int64
		if err := json.Unmarshal(msg[0], &id); err != nil {
			c.logf("ignoring message with non-numeric id %s", msg[0])
			continue
		}
		if id < 0 {
			c.deliver(id, msg[1])
			continue
		}
		c.inflight.Add(1)
		go func() {
			defer c.inflight.Done()
			c.serveRequest(ctx, id, msg[1])
		}()
	}
}

func (c *conn) serveRequest(ctx context.Context, id int64, raw json.RawMessage) {
	res, err := c.run(ctx, raw)
	if id == 0 { // notification: nobody is waiting for an answer
		if err != nil {
			c.logf("notification: %v", err)
		}
		return
	}
	env := map[string]any{"result": res}
	if err != nil {
		env = map[string]any{"error": err.Error()}
	}
	if serr := c.send([]any{id, env}); serr != nil {
		// Most likely the result could not be encoded; tell Vim instead of
		// leaving it waiting. If the pipe itself is broken this fails too.
		if serr2 := c.send([]any{id, map[string]any{"error": serr.Error()}}); serr2 != nil {
			c.logf("request %d: cannot answer: %v", id, serr2)
		}
	}
}

func (c *conn) run(ctx context.Context, raw json.RawMessage) (res any, err error) {
	payload, derr := decode(raw)
	arr, ok := payload.([]any)
	if derr != nil || !ok || len(arr) == 0 {
		return nil, errors.New("malformed request: want [method, params...]")
	}
	method, ok := arr[0].(string)
	if !ok {
		return nil, errors.New("malformed request: method must be a string")
	}
	fn, ok := c.handlers[method]
	if !ok {
		return nil, fmt.Errorf("unknown method %q", method)
	}
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, fmt.Errorf("panic in %s: %v", method, p)
		}
	}()
	return fn(ctx, arr[1:])
}

func (c *conn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(b)
	return err
}

// helper calls one of the goda#_* functions and unpacks its
// [0, result] / [1, message] answer.
func (c *conn) helper(ctx context.Context, fn string, args ...any) (any, error) {
	v, err := c.call(ctx, fn, args)
	if err != nil {
		return nil, err
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 2 {
		// Typically the string "ERROR": Vim could not even call the helper.
		return nil, fmt.Errorf("vim: %s failed: %v (is goda.vim on 'runtimepath'?)", fn, v)
	}
	if status, _ := arr[0].(int64); status != 0 {
		return nil, fmt.Errorf("vim: %v", arr[1])
	}
	return arr[1], nil
}

// call sends a "call" command and waits for Vim's answer.
func (c *conn) call(ctx context.Context, fn string, args []any) (any, error) {
	ch := make(chan reply, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errClosed
	}
	c.last--
	id := c.last
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.send([]any{"call", fn, args, id}); err != nil {
		c.drop(id)
		return nil, err
	}
	select {
	case r := <-ch:
		return r.value, r.err
	case <-ctx.Done():
		c.drop(id)
		return nil, ctx.Err()
	case <-c.done:
		return nil, errClosed
	}
}

func (c *conn) deliver(id int64, raw json.RawMessage) {
	c.mu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if !ok {
		c.logf("answer for unknown call %d", id)
		return
	}
	v, err := decode(raw)
	ch <- reply{value: v, err: err}
}

func (c *conn) drop(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *conn) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
	for id, ch := range c.pending {
		ch <- reply{err: errClosed}
		delete(c.pending, id)
	}
}

// decode turns JSON into the same generic values the msgpack transport yields
// (int64, float64, string, bool, nil, []any, map[string]any), so an extension
// sees identical types whichever editor it runs in.
func decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return normalize(v), nil
}

func normalize(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case []any:
		for i := range x {
			x[i] = normalize(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = normalize(x[k])
		}
		return x
	default:
		return v
	}
}
