package jsonl

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Shougo/goda.vim/ext/filter/matcher_head"
	"github.com/Shougo/goda.vim/ext/source/around"
	"github.com/Shougo/goda.vim/sdk"
)

func TestServeIO(t *testing.T) {
	reg := sdk.NewRegistry()
	reg.RegisterSource("around", around.New())
	reg.RegisterFilter("matcher_head", matcherhead.New())

	in := strings.Join([]string{
		`{"sources":["around"],"filters":["matcher_head"],"context":{"complete_str":"fo","line":1,"lines":["foo bar foobar"]}}`,
		`not json`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := ServeIO(strings.NewReader(in), &out, reg); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 output lines, got %d: %q", len(lines), out.String())
	}

	var r1, r2 sdk.GatherResult
	if err := json.Unmarshal([]byte(lines[0]), &r1); err != nil {
		t.Fatal(err)
	}
	if len(r1.Candidates) != 2 || r1.Candidates[0].Word != "foo" || r1.Candidates[1].Word != "foobar" {
		t.Fatalf("unexpected candidates: %+v", r1.Candidates)
	}
	if err := json.Unmarshal([]byte(lines[1]), &r2); err != nil {
		t.Fatal(err)
	}
	if len(r2.Errors) != 1 || r2.Errors[0].Kind != "request" {
		t.Fatalf("expected a request error, got %+v", r2.Errors)
	}
}
