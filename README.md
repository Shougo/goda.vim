# goda.vim

**Go based dark powered framework for Vim/Neovim.** (Pronounced "Godda".)

A successor in spirit to [denops.vim](https://github.com/vim-denops/denops.vim)
that does not depend on Deno. A plugin is a Go program; the editor starts it and
talks to it. The same binary serves **Vim and Neovim**.

goda.vim itself knows nothing about completion or any other feature: it moves
messages between the editor and plugins, and lets a plugin call back into the
editor. Plugins built on it, such as [goda-cmp.vim](https://github.com/Shougo/goda-cmp.vim),
define their own extension kinds (sources, filters, UIs) as plain Go packages
that are linked into the binary at build time.

> Status: early. The API may still change; see [Status](#status).

## Naming

| what | example | notes |
|---|---|---|
| framework | `goda.vim` | this repository |
| a Vim plugin built on goda | `goda-cmp.vim` | has `.vim` |
| an extension | `goda-cmp-source-file` | a Go module, no `.vim` |

Do not name a plugin binary `goda`: `go install github.com/loov/goda@latest`
(an unrelated tool) installs that name.

## Requirements

* Go 1.24 or newer, to build plugins.
* Vim 9.1 or newer with `+job` and `+channel` (and `+timers` for plugins that
  use timers), or Neovim.

## Try it

This builds and calls a plugin of ten lines. Nothing but Go and the editor is
needed.

```sh
mkdir hello && cd hello
go mod init hello
```

Save this as `main.go`:

```go
package main

import (
	"context"
	"fmt"

	"github.com/Shougo/goda.vim/host"
	"github.com/Shougo/goda.vim/sdk"
)

type hello struct{}

func (hello) Name() string { return "hello" }

func (hello) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		"greet": func(ctx context.Context, h sdk.Host, params []any) (any, error) {
			// Ask the editor for something: its current working directory.
			cwd, err := h.Call(ctx, "getcwd")
			if err != nil {
				return nil, err
			}
			return fmt.Sprintf("hello, %v, from %s (cwd %v)", params[0], h.Meta().Host, cwd), nil
		},
	}
}

func main() { host.Main(hello{}) }
```

Build it:

```sh
go get github.com/Shougo/goda.vim@latest
go mod tidy
go build -o hello .
```

Then, in Vim or Neovim (adjust the paths):

```vim
set runtimepath+=/path/to/goda.vim
call goda#start('/path/to/hello/hello')
echo goda#request('hello', 'greet', 'world')
" hello, world, from vim (cwd /home/you)      (from nvim in Neovim)
```

If something goes wrong, `echo goda#log()` shows what the binary wrote to
stderr.

Without an editor, the development transport answers one JSON request per
line. Calls into the editor (like `getcwd` above) fail in this mode, but
everything else can be exercised:

```sh
echo '{"method":"goda.plugins"}' | ./hello --transport=jsonl
# {"result":{"hello":["greet"]}}
```

## Writing a plugin

A plugin implements `sdk.Plugin`:

```go
type Plugin interface {
	Name() string                      // [a-z][a-z0-9_]*; "goda" is reserved
	Handlers() map[string]sdk.Handler  // method name -> handler
}

type Handler func(ctx context.Context, h sdk.Host, params []any) (any, error)
```

`host.Main(plugins...)` serves any number of plugins from one binary and
exits when the editor closes the connection.

* **Parameters and results** are generic values: `nil`, `bool`, `int64`,
  `float64`, `string`, `[]any` and `map[string]any`, the same in both editors.
  `sdk.Decode(params[0], &typed)` copies one into a struct (json tags apply);
  `sdk.Generic(v)` converts a struct into a result.
* **The editor** is `sdk.Host`: `Call(ctx, fn, args...)` calls a Vim function,
  `Eval` evaluates an expression, `Cmd` runs an Ex command, `Meta().Host` is
  `"vim"` or `"nvim"`. They may be used from inside a handler while the editor
  waits for its answer. Errors from the editor come back as Go errors.
* **Requests run concurrently**, each in its own goroutine, and are not
  ordered: two notifications can be handled in the opposite order. Carry a
  sequence number if order matters. Use `sdk.Guard` around code you do not
  control, to recover from panics and enforce a deadline.
* **stdout belongs to the protocol.** `fmt.Println` is redirected to stderr
  so it cannot corrupt the channel; use stderr or `log` for diagnostics, and
  read it with `goda#log()`.
* **Closing.** A plugin that holds something to release (a child process, a
  connection) implements `sdk.Closer` (`Close(ctx) error`). The host calls it
  once, when the editor closed the connection or the process got SIGTERM or an
  interrupt (which is how `goda#stop()` ends a binary), last registered plugin
  first. The editor is gone then, so there is no `Host`, and requests may still
  be running. All plugins together get `host.CloseTimeout` (3 seconds).
* **Errors** returned by a handler reach the editor as an exception
  (`goda#request`) or are logged (notifications).

## Vim script API

| function | |
|---|---|
| `goda#start(bin)` | Start a plugin binary; returns the names of its plugins. Throws if it cannot be started, speaks another protocol version, or contains a plugin name another running binary serves. Calling it again for a running binary does nothing. |
| `goda#stop([plugin])` | Stop the binary serving `plugin`, or all binaries. |
| `goda#request(plugin, method, args...)` | Blocking call; returns the result or throws. |
| `goda#notify(plugin, method, args...)` | Fire and forget. |
| `goda#plugins()` | `{plugin: binary}` of what is running. |
| `goda#is_running([plugin])` | |
| `goda#log([plugin])` | What the binary wrote to stderr (kept after it exited). |

Any number of binaries can run side by side, each started independently. A
request is routed by plugin name to the binary that contains the plugin, so
`goda-cmp`, `goda-ff` and so on are built, started, updated and restarted
independently of each other. If one crashes the others are unaffected.

`g:goda#request_timeout` (default 10000, in ms) bounds a blocking request in
Vim. There are no commands; everything is a function.

## Help

`:help goda` (doc/goda.txt).

## Versions

When a binary is started, the editor and the binary exchange a handshake
(`goda.hello`): the protocol version, the version of goda.vim the binary was
built with, and the plugin names. If the protocol versions differ, `goda#start`
throws and asks you to rebuild the binary: after updating goda.vim (or a plugin
that bundles it), rebuild. The protocol version changes only when an old binary
and a new Vim script can no longer work together.

## How it works

```
Neovim ── msgpack-rpc over stdio ─┐
                                  ▼
Vim ───── JSON channel over stdio ─► plugin binary ── your plugins and their
                                     (one process       extensions, linked in
                                      per binary)       as ordinary Go packages
```

Extensions are not loaded at run time; they are Go packages imported by the
plugin's own `main`, so versions are pinned by `go.mod` and the compiler checks
the whole program. Building needs Go; running does not. Pure Go, no cgo.

| directory | |
|---|---|
| `autoload/goda.vim` | the Vim script side, for both editors |
| `sdk/` | the contract plugins are written against |
| `host/` | `host.Main` and the transports `nvim/`, `vim/`, `jsonl/` |
| `internal/` | the msgpack codec and JSON helpers |

## Tests

```sh
go test ./...        # includes a real Vim if one is installed (+job +channel)
go test -short ./... # without starting Vim
```

The Vim transport is tested against a real Vim; the Neovim transport only
against a fake editor written from the msgpack-rpc specification.

## AI-assisted, not AI-dependent

AI is used as a tool to speed up development, but goda.vim and the plugins in
this project are not built on the assumption that AI exists.

## Status

Checked: `go vet`, `go test` (and `-race`), the Vim transport and the Vim
script against Vim 9.1 on Linux, several binaries at once, the handshake.
Not checked by the automated tests: Neovim against a real Neovim (a fake editor
only), Vim 9.2, Windows.

## License

MIT, see [LICENSE](LICENSE).
