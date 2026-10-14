# goda.vim

**Go based dark powered framework for Vim/Neovim.** (読み: ゴッダ)

A successor-in-spirit to denops.vim that does not depend on Deno. Plugins are
Go programs; their extensions (completion sources, filters, UIs, ...) live in
separate repositories and are linked in at build time.

> Status: **spike**. Neovim and Vim both work end to end for the pipeline and
> the host API; everything above that is still open. See [Status](#status).

## Naming

| what | example | notes |
|---|---|---|
| framework | `goda.vim` | this repository |
| a Vim plugin built on goda | `goda-cmp.vim` | has `.vim` |
| an extension | `goda-cmp-source-file` | a Go module, no `.vim` |

`goda-cmp-filter-*` and `goda-cmp-ui-*` follow the same pattern (assumed).

Do not name the plugin binary `goda`: `go install github.com/loov/goda@latest`
(an unrelated dependency-analysis tool) installs that name.

## Architecture

```
Neovim ── msgpack-rpc ──┐
                        ▼
Vim ───── JSON channel ─► plugin process  (e.g. goda-cmp, built by goda-build)
                          ├── host/{nvim,vim}  transport, implements sdk.Host
                          ├── core             pipeline, per-extension recover + deadline
                          ├── sdk              Source / Filter / Host interfaces
                          └── extensions       plain Go packages, linked in, same process
```

* **One process per plugin, not per extension.** 32 completion extensions are
  32 packages in one binary, not 32 processes.
* **One binary for both editors.** Every transport is linked in and chosen
  with `--transport`; the Vim script passes it, so you build once.
* **Extensions are ordinary Go modules** exporting `New() sdk.Source` or
  `New() sdk.Filter`. Versions, checksums and caching are Go's own.
* **`core` is the isolation layer.** A panicking or hanging extension is
  reported in `Result.Errors`; the others still answer. (A source that ignores
  its context keeps its goroutine until it returns; Go cannot kill it.)
* **The transports differ only in framing.** The methods the editor can call
  (`goda_gather`, `goda_list`) live once in `core.Methods`, and an extension
  sees the same Go types (`int64`, `float64`, `string`, `[]any`, ...) whichever
  editor it runs in.
* **Pure Go.** `goda-build` sets `CGO_ENABLED=0`. No C toolchain needed.
* **Zero third-party dependencies** in this module (own msgpack codec,
  standard-library JSON).

## Usage

Requires Go 1.24+ and Neovim, or a Vim with `+job +channel`. From the
repository root:

```sh
go build -o goda-build ./cmd/goda-build
cp goda.toml.example goda.toml          # [goda] replace = "." already points here
./goda-build -manifest goda.toml -o goda-host
```

The same commands work in Vim and Neovim:

```vim
set rtp+=/path/to/goda.vim
GodaStart /path/to/goda-host
GodaList                                " registered sources and filters
GodaLog                                 " what the plugin wrote to stderr

" Exercises the nested path: the plugin calls back into the editor (getcompletion)
echo goda#gather({'sources': ['vim'], 'filters': [], 'context': {'complete_str': 'strl'}})
```

`g:goda_request_timeout` (default 10000 ms) bounds a blocking request on Vim.

Without an editor, the `jsonl` transport exercises a binary directly:

```sh
echo '{"sources":["around"],"context":{"complete_str":"fo","line":1,"lines":["foo bar"]}}' \
  | ./goda-host --transport=jsonl
```

`goda-build -transport X` only sets which transport the binary uses when run
without `--transport` (default `nvim`).

## Layout

```
autoload/ plugin/     Vim script side (both editors); must stay at the root,
                      plugin managers add the repository root to 'runtimepath'
sdk/                  extension contract (public: extensions import this)
cmd/goda-build/       the builder (public: go install)
host/nvim/            Neovim transport (msgpack-rpc over stdio)
host/vim/             Vim transport (JSON channel over stdio)
host/jsonl/           dev/test transport
ext/{source,filter}/  example extensions
internal/core/        pipeline, isolation, the methods every transport serves
internal/msgpack/     codec
internal/manifest/    goda.toml parser
internal/gen/         manifest -> go.mod + main.go
bench/run.sh          rebuild-time benchmark
```

Public means importable by other modules: `sdk` by extensions, `host/*` by the
generated `main.go`. Everything else is under `internal/`, so it can change
without breaking anyone.

## Status

Checked here with `go vet ./...`, `go test ./...` and `-race` on core and
hosts:

* pipeline semantics, panic/timeout isolation, failing-filter skipping
* msgpack codec against spec vectors, truncation and hostile input
* manifest -> generated module -> binary -> answered request, with an
  extension in its own module; the builder rebuilds without re-tidying

**Neovim.** Reported working by the author on a real Neovim: `GodaStart`,
`GodaList`, and the `vim` (editor callback) and `around` sources. The
automated tests for this transport run against a *fake* editor written from
the msgpack-rpc spec, because the sandbox this was built in has no `nvim`.

**Vim.** An automated end-to-end test (`host/vim/vim_e2e_test.go`) starts a
real Vim (9.1 here), loads the shipped `autoload/goda.vim`, runs the plugin
binary and checks: start/stop, `goda_list`, `goda_gather`, a source calling
back into Vim mid-request, `Eval`/`Cmd`/`Call` including their error paths,
the timeout cut-off, a stray `fmt.Println` not corrupting the protocol, the
stderr log, notifications. The wire format in `host/vim/vim.go` was measured
against that Vim, not taken from memory. **Not yet tried:** the author's
Vim 9.2, and Windows (either editor).

**Not done:** per-extension options, a real `ui` extension kind, hot reload on
file change (rebuild + restart is fast, see below, but not automated), the
completion popup itself.

### Rebuild time

`bench/run.sh N` links N extensions, each in its own module (2 vCPU sandbox,
go1.24.7). Run twice on the same machine; the spread is machine noise (the
`std only` step, which this project does not influence, moved from 24.7 s to
39.8 s between the runs):

| | 32 ext (2 runs) | 100 ext (1 run) |
|---|---|---|
| first build, std cached | 6.4 – 10.3 s | 7.1 s |
| rebuild, nothing changed | 0.08 – 0.13 s | 0.09 s |
| rebuild, one extension edited | 0.24 – 0.38 s | 0.25 s |
| rebuild, one extension added | 0.30 – 0.46 s | 0.31 s |

The synthetic extensions are tiny and have no dependencies, so real ones
(especially with large dependencies) cost more on the *first* build. Edits
only recompile the changed package and re-link, which is why those numbers
barely move with N.
