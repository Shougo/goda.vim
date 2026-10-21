// Package nvim is the Neovim transport: msgpack-rpc over stdio. Neovim starts
// the plugin binary with jobstart(..., {'rpc': v:true}) and talks to it with
// rpcrequest(); the plugin calls back with nvim_call_function & co.
//
// The method table (plugin.method -> handler) is built by package host.
package nvim

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/Shougo/goda.vim/sdk"
)

// Host implements sdk.Host on top of Neovim's API.
type Host struct{ c *rpc }

func (h *Host) Meta() sdk.Meta { return sdk.Meta{Host: sdk.HostNvim} }

// Call converts args to generic values first, so that structs (which the
// msgpack codec does not know) can be passed as they can on Vim.
func (h *Host) Call(ctx context.Context, fn string, args ...any) (any, error) {
	if args == nil {
		args = []any{}
	}
	norm, err := sdk.Generic(args)
	if err != nil {
		return nil, err
	}
	return h.c.call(ctx, "nvim_call_function", fn, norm)
}

// Notify sends nvim_call_function as a msgpack-rpc notification. It goes
// through goda#_notify, as on Vim, so that an error is kept for goda#log()
// the same way in both editors.
func (h *Host) Notify(_ context.Context, fn string, args ...any) error {
	if args == nil {
		args = []any{}
	}
	norm, err := sdk.Generic(args)
	if err != nil {
		return err
	}
	return h.c.notify("nvim_call_function", "goda#_notify", []any{fn, norm})
}

func (h *Host) Eval(ctx context.Context, expr string) (any, error) {
	return h.c.call(ctx, "nvim_eval", expr)
}

func (h *Host) Cmd(ctx context.Context, cmd string) error {
	_, err := h.c.call(ctx, "nvim_command", cmd)
	return err
}

// Serve runs the plugins over stdin/stdout until Neovim closes the channel.
// stdout carries the protocol, so the process-wide os.Stdout is redirected to
// stderr: a stray fmt.Println in a plugin cannot corrupt the stream.
func Serve(table map[string]sdk.Handler) error {
	in, out := os.Stdin, os.Stdout
	os.Stdout = os.Stderr
	logger := log.New(os.Stderr, "goda: ", log.LstdFlags)
	return ServeIO(context.Background(), in, out, table, logger.Printf)
}

// ServeIO is Serve over arbitrary streams (used by tests).
func ServeIO(ctx context.Context, r io.Reader, w io.Writer, table map[string]sdk.Handler, logf func(string, ...any)) error {
	c := newRPC(r, w, logf)
	host := &Host{c: c}
	for name, fn := range table {
		c.handle(name, func(ctx context.Context, params []any) (any, error) {
			return fn(ctx, host, params)
		})
	}
	return c.serve(ctx)
}
