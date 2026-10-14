package gen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Shougo/goda.vim/internal/manifest"
)

func parse(t *testing.T, src string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

const src = `
[goda]
replace = "."

[[source]]
name = "around"
package = "github.com/Shougo/goda.vim/ext/source/around"

[[source]]
name = "file"
package = "github.com/example/goda-cmp-source-file"
version = "v1.2.3"

[[filter]]
name = "matcher_head"
package = "github.com/Shougo/goda.vim/ext/filter/matcher_head"

[[filter]]
name = "local"
package = "example.com/local/filter"
replace = "../local-filter"
`

func TestRender(t *testing.T) {
	goMod, mainGo, err := Render(parse(t, src), "/work/proj", "nvim")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"module goda-host",
		"github.com/Shougo/goda.vim v0.0.0",
		"github.com/example/goda-cmp-source-file v1.2.3",
		"example.com/local/filter v0.0.0", // replace without version
		`github.com/Shougo/goda.vim => "/work/proj"`,
		`example.com/local/filter => "/work/local-filter"`,
	} {
		if !strings.Contains(goMod, want) {
			t.Errorf("go.mod lacks %q:\n%s", want, goMod)
		}
	}
	if strings.Contains(goMod, "goda-cmp-source-file =>") {
		t.Errorf("a version-pinned module must not get a replace:\n%s", goMod)
	}

	for _, want := range []string{
		`reg.RegisterSource("around", e0.New())`,
		`reg.RegisterSource("file", e1.New())`,
		`reg.RegisterFilter("matcher_head", e2.New())`,
		`reg.RegisterFilter("local", e3.New())`,
		// every transport is linked in; the default is only a flag default
		"nvim.Serve(reg)",
		"vim.Serve(reg)",
		"jsonl.Serve(reg)",
		`"github.com/Shougo/goda.vim/host/nvim"`,
		`"github.com/Shougo/goda.vim/host/vim"`,
		`"github.com/Shougo/goda.vim/host/jsonl"`,
		`flag.String("transport", "nvim"`,
	} {
		if !strings.Contains(mainGo, want) {
			t.Errorf("main.go lacks %q:\n%s", want, mainGo)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "main.go", mainGo, 0); err != nil {
		t.Fatalf("main.go does not parse: %v", err)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	m := parse(t, src)
	a1, b1, _ := Render(m, "/w", "jsonl")
	a2, b2, _ := Render(m, "/w", "jsonl")
	if a1 != a2 || b1 != b2 {
		t.Fatal("Render output must be stable so the build cache and tidy marker work")
	}
}

func TestDefaultTransport(t *testing.T) {
	_, mainGo, err := Render(parse(t, src), "/w", "vim")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainGo, `flag.String("transport", "vim"`) {
		t.Fatalf("the requested default must become the flag default:\n%s", mainGo)
	}
}

func TestRenderErrors(t *testing.T) {
	if _, _, err := Render(parse(t, src), "/w", "emacs"); err == nil {
		t.Error("unknown transport must fail")
	}
	noVersion := "[[source]]\nname = \"x\"\npackage = \"example.com/x\"\n"
	if _, _, err := Render(parse(t, noVersion), "/w", "nvim"); err == nil {
		t.Error("a remote module without a version must fail")
	}
	conflict := "[[source]]\nname = \"a\"\npackage = \"example.com/m/a\"\nmodule = \"example.com/m\"\nversion = \"v1.0.0\"\n" +
		"[[source]]\nname = \"b\"\npackage = \"example.com/m/b\"\nmodule = \"example.com/m\"\nversion = \"v2.0.0\"\n"
	if _, _, err := Render(parse(t, conflict), "/w", "nvim"); err == nil {
		t.Error("two versions of one module must fail")
	}
}

func TestSameModuleTwice(t *testing.T) {
	ok := "[[source]]\nname = \"a\"\npackage = \"example.com/m/a\"\nmodule = \"example.com/m\"\nversion = \"v1.0.0\"\n" +
		"[[filter]]\nname = \"b\"\npackage = \"example.com/m/b\"\nmodule = \"example.com/m\"\nversion = \"v1.0.0\"\n"
	goMod, _, err := Render(parse(t, ok), "/w", "nvim")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(goMod, "example.com/m v1.0.0") != 1 {
		t.Fatalf("module must be required once:\n%s", goMod)
	}
}
