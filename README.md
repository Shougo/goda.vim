# goda.vim

**Go based dark powered framework for Vim/Neovim.** (読み: ゴッダ)

A successor-in-spirit to denops.vim that does not depend on Deno. Plugins are
Go programs; their extensions (completion sources, filters, UIs, ...) live in
separate repositories and are linked in at build time.

> Status: **spike**. It answers two design questions with running code and
> leaves everything else open.

## Naming

| what | example | notes |
|---|---|---|
| framework | `goda.vim` | this repository |
| a Vim plugin built on goda | `goda-cmp.vim` | has `.vim` |
| an extension | `goda-cmp-source-file` | a Go module, no `.vim` |

## Architecture

```
Neovim / Vim
   │  msgpack-rpc (nvim) / JSON channel (vim, not yet)
   ▼
plugin process  (e.g. goda-cmp, built by goda-build)
   ├── core        pipeline, per-extension recover + deadline
   ├── sdk         Source / Filter / Host interfaces
   └── extensions  plain Go packages, linked in, same process
```

* **One process per plugin, not per extension.**
* **Extensions are ordinary Go modules** exporting `New() sdk.Source` or
  `New() sdk.Filter`. Versions, checksums and caching are Go's own.
* **`core` is the isolation layer.** A panicking or hanging extension is
  reported in `Result.Errors`; the others still answer.
* **Pure Go.** `goda-build` sets `CGO_ENABLED=0`. No C toolchain needed.
* **Zero third-party dependencies** in this module (own msgpack codec).
