package manifest

import (
	"strings"
	"testing"
)

const sample = `
# comment
[goda]
replace = "."   # local checkout

[[source]]
name = "around"
package = "github.com/Shougo/goda.vim/ext/source/around"

[[source]]
name = "file"
package = "github.com/example/goda-cmp-source-file"
version = "v1.2.3"

[[filter]]
name = "matcher_head"
package = "github.com/example/goda-cmp-filter-matcher_head/pkg/head"
module = "github.com/example/goda-cmp-filter-matcher_head"
replace = 'C:\dev\head'
`

func TestParse(t *testing.T) {
	m, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	if m.Goda.Module != DefaultModule || m.Goda.Version != DefaultVersion || m.Goda.Replace != "." {
		t.Fatalf("goda = %+v", m.Goda)
	}
	if len(m.Exts) != 3 {
		t.Fatalf("exts = %+v", m.Exts)
	}
	around, file, head := m.Exts[0], m.Exts[1], m.Exts[2]
	if around.Module != DefaultModule {
		t.Errorf("in-tree module = %q", around.Module)
	}
	if file.Module != "github.com/example/goda-cmp-source-file" || file.Version != "v1.2.3" {
		t.Errorf("file = %+v", file)
	}
	if head.Kind != "filter" || head.Replace != `C:\dev\head` {
		t.Errorf("head = %+v", head)
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"unknown table":    "[nope]\n",
		"key before table": "name = \"x\"\n",
		"unknown key":      "[[source]]\nname = \"a\"\npackage = \"p\"\ncolor = \"red\"\n",
		"unquoted":         "[[source]]\nname = a\n",
		"unterminated":     "[[source]]\nname = \"a\n",
		"trailing":         "[[source]]\nname = \"a\" junk\n",
		"no package":       "[[source]]\nname = \"a\"\n",
		"bad name":         "[[source]]\nname = \"a-b\"\npackage = \"p\"\n",
		"duplicate":        "[[source]]\nname = \"a\"\npackage = \"p\"\n[[source]]\nname = \"a\"\npackage = \"q\"\n",
		"in-tree version":  "[[source]]\nname = \"a\"\npackage = \"github.com/Shougo/goda.vim/ext/source/around\"\nversion = \"v1.0.0\"\n",
	}
	for name, src := range tests {
		if _, err := Parse(src); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestErrorMentionsLine(t *testing.T) {
	_, err := Parse("[goda]\n\nbogus = 1\n")
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("error should name line 3, got %v", err)
	}
}

func TestSameNameDifferentKind(t *testing.T) {
	src := "[[source]]\nname = \"x\"\npackage = \"p1\"\nversion = \"v1.0.0\"\n[[filter]]\nname = \"x\"\npackage = \"p2\"\nversion = \"v1.0.0\"\n"
	if _, err := Parse(src); err != nil {
		t.Fatalf("a source and a filter may share a name: %v", err)
	}
}
