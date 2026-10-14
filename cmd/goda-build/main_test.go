package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// End to end: manifest -> generated module -> binary -> a request answered.
// Needs the go toolchain and no network (everything is replaced locally).
func TestBuildEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	// An extension in its own module, as it would live in its own repository.
	ext := filepath.Join(dir, "goda-cmp-source-hello")
	write(t, filepath.Join(ext, "go.mod"), "module example.com/goda-cmp-source-hello\n\ngo 1.24\n\n"+
		"require github.com/Shougo/goda.vim v0.0.0\n\nreplace github.com/Shougo/goda.vim => "+root+"\n")
	write(t, filepath.Join(ext, "hello.go"), `package hello

import (
	"context"

	"github.com/Shougo/goda.vim/sdk"
)

type s struct{}

func New() sdk.Source { return s{} }

func (s) Gather(context.Context, sdk.Host, sdk.Context) ([]sdk.Candidate, error) {
	return []sdk.Candidate{{Word: "hello-from-extension"}}, nil
}
`)

	manifest := filepath.Join(dir, "goda.toml")
	write(t, manifest, `
[goda]
replace = "`+root+`"

[[source]]
name = "hello"
package = "example.com/goda-cmp-source-hello"
replace = "./goda-cmp-source-hello"

[[filter]]
name = "matcher_head"
package = "github.com/Shougo/goda.vim/ext/filter/matcher_head"
`)

	bin := filepath.Join(dir, "plugin")
	o := options{manifest: manifest, out: bin, transport: "jsonl"}
	if err := build(o); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin)
	cmd.Stdin = strings.NewReader(`{"sources":["hello"],"filters":["matcher_head"],"context":{"complete_str":"hel"}}` + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "hello-from-extension") {
		t.Fatalf("unexpected output: %s", out)
	}

	// Rebuilding the same manifest must succeed without re-tidying.
	if err := build(o); err != nil {
		t.Fatalf("second build: %v", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
