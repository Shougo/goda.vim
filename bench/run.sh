#!/usr/bin/env bash
# Rebuild-time benchmark for goda-build.
#
# Generates N tiny extensions, each in its OWN Go module (as if each lived in
# its own repository), links them with goda-build and times:
#
#   std only          populating a fresh GOCACHE with the standard library
#   first build       first manifest build, std already cached
#   no-op rebuild     nothing changed
#   edit one ext      one extension's source changed
#   add one ext       manifest gains one more extension
#
# usage: bench/run.sh [N=32]
#
# The synthetic extensions are far smaller than real ones, so absolute
# numbers understate compile cost; the per-extension overhead of resolving,
# linking and re-linking is what this measures. Uses the jsonl transport so no
# network is needed.
set -euo pipefail

N=${1:-32}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/goda-bench.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

NSRC=$(( (N + 1) / 2 ))
NFIL=$(( N - NSRC ))

gen_ext() { # index kind
  local i=$1 kind=$2 d="$WORK/ext/ext$1"
  mkdir -p "$d"
  cat >"$d/go.mod" <<EOF
module example.com/bench/ext$i

go 1.24

require github.com/Shougo/goda.vim v0.0.0

replace github.com/Shougo/goda.vim => "$ROOT"
EOF
  if [ "$kind" = source ]; then
    cat >"$d/ext.go" <<EOF
package ext$i

import (
	"context"

	"github.com/Shougo/goda.vim/sdk"
)

type s struct{}

func New() sdk.Source { return s{} }

func (s) Gather(ctx context.Context, h sdk.Host, c sdk.Context) ([]sdk.Candidate, error) {
	return []sdk.Candidate{{Word: "ext$i" + c.CompleteStr}}, nil
}
EOF
  else
    cat >"$d/ext.go" <<EOF
package ext$i

import (
	"context"

	"github.com/Shougo/goda.vim/sdk"
)

type f struct{}

func New() sdk.Filter { return f{} }

func (f) Filter(ctx context.Context, c sdk.Context, in []sdk.Candidate) ([]sdk.Candidate, error) {
	return in, nil
}
EOF
  fi
}

manifest_entry() { # index kind name
  cat <<EOF

[[$2]]
name = "$3"
package = "example.com/bench/ext$1"
replace = "$WORK/ext/ext$1"
EOF
}

# Build the tool itself with the caller's normal cache, before measuring.
(cd "$ROOT" && go build -o "$WORK/goda-build" ./cmd/goda-build)

printf '[goda]\nreplace = "%s"\n' "$ROOT" >"$WORK/goda.toml"
for i in $(seq 1 "$NSRC"); do
  gen_ext "$i" source
  manifest_entry "$i" source "s$i" >>"$WORK/goda.toml"
done
for j in $(seq 1 "$NFIL"); do
  gen_ext $((NSRC + j)) filter
  manifest_entry $((NSRC + j)) filter "f$j" >>"$WORK/goda.toml"
done

export GOCACHE="$WORK/gocache" # empty: nothing compiled yet
export GOFLAGS=
BIN="$WORK/plugin"

timeit() { # label cmd...
  local label=$1 s e
  shift
  s=$(date +%s.%N)
  if ! "$@" >"$WORK/last.log" 2>&1; then cat "$WORK/last.log"; exit 1; fi
  e=$(date +%s.%N)
  awk -v l="$label" -v a="$s" -v b="$e" 'BEGIN { printf "  %-26s %7.2fs\n", l, b - a }'
}

build() { "$WORK/goda-build" -manifest "$WORK/goda.toml" -o "$BIN" -transport jsonl; }

echo "goda-build benchmark: $N extensions in $N separate modules ($NSRC sources, $NFIL filters)"
echo "go: $(go version | awk '{print $3, $4}'), cpus: $(nproc)"
echo
timeit "std only (fresh cache)" go build std
timeit "first build" build
echo "  binary size: $(wc -c <"$BIN") bytes"
timeit "no-op rebuild" build
sed -i 's/"ext1"/"ext1-edited"/' "$WORK/ext/ext1/ext.go"
timeit "edit one ext" build
NEW=$((N + 1))
gen_ext "$NEW" source
manifest_entry "$NEW" source "s$NEW" >>"$WORK/goda.toml"
timeit "add one ext" build

echo
echo "smoke test:"
printf '{"sources":["s1","s2","s%s"],"filters":["f1"],"context":{"complete_str":"x"}}\n' "$NEW" | "$BIN"
