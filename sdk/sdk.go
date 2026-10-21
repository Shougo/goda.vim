// Package sdk is the contract between goda.vim and the plugins built on it.
//
// goda.vim itself knows nothing about completion, filtering or any other
// feature. It moves messages between the editor and plugins, and gives plugins
// a way to call the editor (Host). What a plugin does with that is its own
// business: a completion plugin, for instance, defines its own extension kinds
// (sources, filters, UIs) on top of this package.
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// Protocol is the version of the contract between the Vim script side
// (autoload/goda.vim) and a plugin binary: the transports, the method naming
// and the goda.hello handshake. It is bumped only when an old binary and a new
// autoload (or the reverse) can no longer work together. autoload/goda.vim
// carries the same number (s:protocol); a test keeps them equal.
const Protocol = 1

// Editors reported by Meta.Host.
const (
	HostVim  = "vim"
	HostNvim = "nvim"
	// HostNone is the jsonl development transport: there is no editor, and
	// every Host call fails.
	HostNone = "none"
)

// Meta describes the editor on the other end. It is a struct so that fields
// can be added later without changing the Host interface.
type Meta struct {
	Host string // HostVim, HostNvim or HostNone
}

// Host is the editor. One implementation exists per transport.
//
// The editor runs what a plugin sends in the order it was sent, whichever of
// these methods sent it.
//
// This interface does not grow: a method added to it would break every
// implementation outside this module (the fakes in tests, for instance). A
// capability added later is a separate interface that a Host may implement,
// found with a type assertion, like Closer for plugins.
type Host interface {
	Meta() Meta
	// Call calls a Vim function and returns its decoded result. Arguments
	// may be any value encoding/json can marshal; the editor sees the JSON
	// shape (struct fields follow their json tags).
	Call(ctx context.Context, fn string, args ...any) (any, error)
	// Notify calls a Vim function without waiting for it: it returns once
	// the call is sent, and the result is dropped. An error of the
	// function does not come back either; it is kept for goda#log(). The
	// error returned is about sending (the editor is gone, an argument
	// cannot be encoded).
	Notify(ctx context.Context, fn string, args ...any) error
	// Eval evaluates a Vim expression.
	Eval(ctx context.Context, expr string) (any, error)
	// Cmd executes an Ex command.
	Cmd(ctx context.Context, cmd string) error
}

// Handler serves one method of a plugin. params are generic decoded values:
// nil, bool, int64, float64, string, []any and map[string]any, the same in
// every editor. The result must be made of the same kinds (or of types that
// round-trip through encoding/json, see Generic).
type Handler func(ctx context.Context, h Host, params []any) (any, error)

// Plugin is a unit of functionality served to the editor.
//
// The editor reaches a method as goda#request(Name(), method, args...). Names
// are [a-z][a-z0-9_]* ("goda" is reserved), method names [A-Za-z_][A-Za-z0-9_]*.
type Plugin interface {
	Name() string
	Handlers() map[string]Handler
}

// Closer is implemented by plugins that hold something to release (a child
// process, a connection, a file). The host calls Close once, when the editor
// closed the connection or the process was told to stop (SIGTERM, interrupt).
// Requests may still be running then, and the editor is gone: there is no
// Host. Close should return promptly; the host gives all plugins together a
// few seconds (host.CloseTimeout) and then exits. Plugins are closed in the
// reverse order of their registration.
type Closer interface {
	Close(ctx context.Context) error
}

// Decode copies a generic value, such as an element of a Handler's params,
// into dst (a pointer to a typed value). It goes through JSON, so json tags
// apply and numbers convert whichever form they arrived in.
func Decode(src, dst any) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// Generic converts a typed value (with json tags) into the generic form a
// Handler may return. Numbers become json.Number, so integers stay integers
// through any codec.
func Generic(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Guard runs fn in its own goroutine so that a panic is recovered and a
// function that ignores ctx cannot block the caller past its deadline. Use it
// around code you do not control, such as third-party extensions.
//
// Go cannot kill a goroutine: after the deadline the function keeps running
// until it returns, and its result is discarded.
func Guard[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				var zero T
				ch <- result{zero, fmt.Errorf("panic: %v", p)}
			}
		}()
		v, err := fn()
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
