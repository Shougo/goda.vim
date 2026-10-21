package nvim

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/Shougo/goda.vim/internal/msgpack"
)

// msgpack-rpc message kinds.
const (
	kindRequest      = 0 // [0, msgid, method, params]
	kindResponse     = 1 // [1, msgid, error, result]
	kindNotification = 2 // [2, method, params]
)

var errClosed = errors.New("nvim: connection closed")

type handlerFunc func(ctx context.Context, params []any) (any, error)

type reply struct {
	result any
	err    error
}

// rpc is a symmetric msgpack-rpc endpoint. Incoming requests run in their own
// goroutines, so a handler may call back into the editor (rpc.call) while the
// editor is blocked in rpcrequest() waiting for that very handler. That nested
// pattern is how extensions use sdk.Host.
type rpc struct {
	dec  *msgpack.Decoder
	w    io.Writer
	logf func(format string, args ...any)

	wmu sync.Mutex // serialises writes

	inflight sync.WaitGroup // handlers still running; serve waits for them

	mu       sync.Mutex // guards the fields below
	nextID   int64
	pending  map[int64]chan reply
	handlers map[string]handlerFunc
	closed   bool
	done     chan struct{}
}

func newRPC(r io.Reader, w io.Writer, logf func(string, ...any)) *rpc {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &rpc{
		dec:      msgpack.NewDecoder(r),
		w:        w,
		logf:     logf,
		pending:  map[int64]chan reply{},
		handlers: map[string]handlerFunc{},
		done:     make(chan struct{}),
	}
}

// handle registers a method. Call it before serve.
func (c *rpc) handle(method string, fn handlerFunc) { c.handlers[method] = fn }

func (c *rpc) send(msg []any) error {
	b, err := msgpack.Encode(msg)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(b)
	return err
}

// serve reads messages until the peer closes the stream. Requests that were
// already received are still answered before it returns: shutdown only fails
// calls waiting on the (now gone) editor, then in-flight handlers finish.
func (c *rpc) serve(ctx context.Context) error {
	defer func() {
		c.shutdown()
		c.inflight.Wait()
	}()
	for {
		v, err := c.dec.Decode()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		msg, ok := v.([]any)
		if !ok || len(msg) < 3 {
			c.logf("ignoring malformed message: %v", v)
			continue
		}
		kind, _ := msg[0].(int64)
		switch kind {
		case kindRequest:
			if len(msg) != 4 {
				c.logf("ignoring malformed request: %v", v)
				continue
			}
			id, _ := msg[1].(int64)
			method, _ := msg[2].(string)
			params, _ := msg[3].([]any)
			c.inflight.Go(func() {
				c.serveRequest(ctx, id, method, params)
			})
		case kindResponse:
			if len(msg) != 4 {
				c.logf("ignoring malformed response: %v", v)
				continue
			}
			id, _ := msg[1].(int64)
			c.mu.Lock()
			ch, ok := c.pending[id]
			delete(c.pending, id)
			c.mu.Unlock()
			if !ok {
				c.logf("response for unknown request %d", id)
				continue
			}
			if msg[2] != nil {
				ch <- reply{err: remoteError(msg[2])}
			} else {
				ch <- reply{result: msg[3]}
			}
		case kindNotification:
			method, _ := msg[1].(string)
			params, _ := msg[2].([]any)
			if method == "nvim_error_event" {
				// Neovim's report of a failed notification of ours.
				c.logf("editor error: %v", params)
				continue
			}
			c.inflight.Go(func() {
				if _, err := c.dispatch(ctx, method, params); err != nil {
					c.logf("notification %s: %v", method, err)
				}
			})
		default:
			c.logf("ignoring message of unknown kind %d", kind)
		}
	}
}

func (c *rpc) serveRequest(ctx context.Context, id int64, method string, params []any) {
	res, err := c.dispatch(ctx, method, params)
	if err == nil {
		err = c.send([]any{int64(kindResponse), id, nil, res})
		if err == nil {
			return
		}
		// The result could not be encoded; tell the editor instead of hanging it.
	}
	// Error objects are [type, message]; type 0 is Neovim's "Exception".
	if serr := c.send([]any{int64(kindResponse), id, []any{int64(0), err.Error()}, nil}); serr != nil {
		c.logf("request %d (%s): cannot send error response: %v", id, method, serr)
	}
}

func (c *rpc) dispatch(ctx context.Context, method string, params []any) (res any, err error) {
	fn, ok := c.handlers[method]
	if !ok {
		return nil, fmt.Errorf("unknown method %q", method)
	}
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, fmt.Errorf("panic in %s: %v", method, p)
		}
	}()
	return fn(ctx, params)
}

// notify sends a notification to the editor; nothing comes back.
func (c *rpc) notify(method string, params ...any) error {
	if params == nil {
		params = []any{}
	}
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return errClosed
	}
	return c.send([]any{int64(kindNotification), method, params})
}

// call sends a request to the editor and waits for the response.
func (c *rpc) call(ctx context.Context, method string, params ...any) (any, error) {
	if params == nil {
		params = []any{}
	}
	ch := make(chan reply, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errClosed
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.send([]any{int64(kindRequest), id, method, params}); err != nil {
		c.drop(id)
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		c.drop(id)
		return nil, ctx.Err()
	case <-c.done:
		return nil, errClosed
	}
}

func (c *rpc) drop(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *rpc) shutdown() {
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

func remoteError(v any) error {
	if arr, ok := v.([]any); ok && len(arr) == 2 {
		return fmt.Errorf("nvim: %v", arr[1])
	}
	return fmt.Errorf("nvim: %v", v)
}
