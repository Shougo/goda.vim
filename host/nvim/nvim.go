// Package nvim is the Neovim transport: msgpack-rpc over stdio. Neovim starts
// the plugin binary with jobstart(..., {'rpc': v:true}) and talks to it with
// rpcrequest(); the plugin calls back with nvim_call_function & co.
//
// The method table (plugin.method -> handler) is built by package host.
package nvim

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"math"
	"os"
	"unicode/utf8"

	"github.com/Shougo/goda.vim/sdk"
)

// Host implements sdk.Host on top of Neovim's API.
type Host struct{ c *rpc }

func (h *Host) Meta() sdk.Meta { return sdk.Meta{Host: sdk.HostNvim} }

// Call converts args to generic values first, so that structs (which the
// msgpack codec does not know) can be passed as they can on Vim.
func (h *Host) Call(ctx context.Context, fn string, args ...any) (any, error) {
	norm, err := normalize(args)
	if err != nil {
		return nil, err
	}
	// Through goda#_ncall: the notifications sent before run first (they
	// wait for Neovim's loop, see autoload/goda.vim).
	return h.c.call(ctx, "nvim_call_function", "goda#_ncall", []any{fn, norm})
}

// Notify sends nvim_call_function as a msgpack-rpc notification. It goes
// through goda#_notify, as on Vim, so that an error is kept for goda#log()
// the same way in both editors.
func (h *Host) Notify(_ context.Context, fn string, args ...any) error {
	norm, err := normalize(args)
	if err != nil {
		return err
	}
	return h.c.notify("nvim_call_function", "goda#_notify", []any{fn, norm})
}

func (h *Host) Eval(ctx context.Context, expr string) (any, error) {
	return h.c.call(ctx, "nvim_call_function", "goda#_neval", []any{expr})
}

func (h *Host) Cmd(ctx context.Context, cmd string) error {
	_, err := h.c.call(ctx, "nvim_call_function", "goda#_ncmd", []any{cmd})
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

// normalize gives args as generic values for the msgpack codec. Generic
// args are sent as they are: sdk.Generic would give the same values, and
// its JSON round trip costs much for long lists (the items of a
// completion). Others go through sdk.Generic.
func normalize(args []any) (any, error) {
	if args == nil {
		return []any{}, nil
	}
	if generic(args) {
		return args, nil
	}
	return sdk.Generic(args)
}

// generic reports whether v is made of values that sdk.Generic keeps as
// they are on the wire. Not: a float (an integral one becomes an integer), a
// string not in UTF-8 (JSON replaces the bytes), an integer past int64 (it
// becomes a float), []byte (base64), a struct, and the typed slices and maps
// but []string.
func generic(v any) bool {
	switch x := v.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint8, uint16, uint32,
		json.Number:
		return true
	case uint:
		return uint64(x) <= math.MaxInt64
	case uint64:
		return x <= math.MaxInt64
	case string:
		return utf8.ValidString(x)
	case []string:
		for _, e := range x {
			if !utf8.ValidString(e) {
				return false
			}
		}
		return true
	case []any:
		for _, e := range x {
			if !generic(e) {
				return false
			}
		}
		return true
	case map[string]any:
		for k, e := range x {
			if !utf8.ValidString(k) || !generic(e) {
				return false
			}
		}
		return true
	}
	return false
}
