// Package nvim is the Neovim transport: msgpack-rpc over stdio. Neovim starts
// the plugin binary with jobstart(..., {'rpc': v:true}) and talks to it with
// rpcrequest(); the plugin calls back with nvim_call_function & co.
//
// The methods served to the editor are defined once, in core.Methods.
package nvim

import (
	"context"
	"io"
	"log"
	"os"

	"github.com/Shougo/goda.vim/core"
	"github.com/Shougo/goda.vim/sdk"
)

// Host implements sdk.Host on top of Neovim's API.
type Host struct{ c *rpc }

func (h *Host) Call(ctx context.Context, fn string, args ...any) (any, error) {
	if args == nil {
		args = []any{}
	}
	return h.c.call(ctx, "nvim_call_function", fn, args)
}

func (h *Host) Eval(ctx context.Context, expr string) (any, error) {
	return h.c.call(ctx, "nvim_eval", expr)
}

func (h *Host) Cmd(ctx context.Context, cmd string) error {
	_, err := h.c.call(ctx, "nvim_command", cmd)
	return err
}

// Serve runs the plugin over stdin/stdout until Neovim closes the channel.
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
	c := newRPC(r, w, logf)
	host := &Host{c: c}
	for name, fn := range core.Methods(reg, host) {
		c.handle(name, handlerFunc(fn))
	}
	return c.serve(ctx)
}
