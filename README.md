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

## Try it

This builds and calls a plugin of ten lines. You need Go 1.26 or newer and
Vim 9.1 or newer (with `+job` and `+channel`) or Neovim.

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

(While the code is on the `wip` branch, use `go get github.com/Shougo/goda.vim@wip`.)

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

## Documentation

`:help goda` ([doc/goda.txt](doc/goda.txt)): the Vim script functions and
variables, writing a plugin (the `sdk` interfaces, `sdk.Closer`), and versions
and the handshake.

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
