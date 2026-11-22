package custom_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Shougo/goda.vim/sdk/custom"
)

type params map[string]any

type options struct {
	Name     string            `json:"name,omitempty"`
	UI       string            `json:"ui,omitempty"`
	UIParams map[string]params `json:"uiParams,omitempty"`
	Debug    *bool             `json:"debug,omitempty"`
}

// The layers in order, and the name of the session.
func TestResolve(t *testing.T) {
	c := custom.New[options](custom.Params("uiParams"))
	if err := c.PatchGlobal(options{UI: "a", UIParams: map[string]params{"a": {"x": 1, "y": 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := c.PatchLocal("s", options{UI: "b"}); err != nil {
		t.Fatal(err)
	}
	r := custom.Request{
		Global: custom.Map{"uiParams": custom.Map{"a": custom.Map{"y": 2}}},
		Local:  map[string]custom.Map{"s": {"uiParams": custom.Map{"a": custom.Map{"z": 3}}}},
	}

	o, m, err := c.Resolve(custom.Map{"name": "s"}, r)
	if err != nil {
		t.Fatal(err)
	}
	want := options{Name: "s", UI: "b", UIParams: map[string]params{"a": {"x": 1.0, "y": 2.0, "z": 3.0}}}
	if !reflect.DeepEqual(o, want) {
		t.Errorf("Resolve = %+v, want %+v", o, want)
	}
	if m["name"] != "s" || m["ui"] != "b" {
		t.Errorf("map = %v", m)
	}

	// No name: "default" (the local options are not those of "s").
	if o, _, _ := c.Resolve(nil, custom.Request{}); o.Name != "default" || o.UI != "a" {
		t.Errorf("default: %+v", o)
	}
	// The name of the request, then of the configuration.
	if o, _, _ := c.Resolve(nil, custom.Request{Global: custom.Map{"name": "s"}}); o.Name != "s" || o.UI != "b" {
		t.Errorf("name of the request: %+v", o)
	}
	_ = c.PatchGlobal(options{Name: "g"})
	if o, _, _ := c.Resolve(nil, custom.Request{}); o.Name != "g" {
		t.Errorf("name of the configuration: %+v", o)
	}
}

// By default an option replaces that of before; Params merges param by
// param (a map in a param replaced whole); Deep merges every map and keeps
// that of before for nil.
func TestMerge(t *testing.T) {
	dst := custom.Map{"o": custom.Map{"a": 1}, "p": custom.Map{"ui": custom.Map{"m": custom.Map{"k": 1}, "x": 1}}, "n": 1}
	src := custom.Map{"o": custom.Map{"b": 2}, "p": custom.Map{"ui": custom.Map{"m": custom.Map{"j": 2}}}, "n": nil}
	for _, tc := range []struct {
		name string
		c    *custom.Patches[options]
		want custom.Map
	}{
		{"replace", custom.New[options](), custom.Map{
			"o": custom.Map{"b": 2}, "p": custom.Map{"ui": custom.Map{"m": custom.Map{"j": 2}}}, "n": nil}},
		{"params", custom.New[options](custom.Params("p")), custom.Map{
			"o": custom.Map{"b": 2}, "p": custom.Map{"ui": custom.Map{"m": custom.Map{"j": 2}, "x": 1}}, "n": nil}},
		{"deep", custom.New[options](custom.Deep()), custom.Map{
			"o": custom.Map{"a": 1, "b": 2}, "p": custom.Map{"ui": custom.Map{"m": custom.Map{"k": 1, "j": 2}, "x": 1}}, "n": 1}},
	} {
		if got := tc.c.Merge(dst, src); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if !reflect.DeepEqual(dst["o"], custom.Map{"a": 1}) {
		t.Errorf("dst modified: %v", dst)
	}
}

// A param map of a named type (Params) is merged too.
func TestMergeNamedParams(t *testing.T) {
	c := custom.New[options](custom.Params("uiParams"))
	got := c.Merge(custom.Map{"uiParams": map[string]params{"a": {"x": 1}}}, custom.Map{"uiParams": custom.Map{"a": params{"y": 2}}})
	want := custom.Map{"uiParams": custom.Map{"a": custom.Map{"x": 1, "y": 2}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}

func TestStrict(t *testing.T) {
	if _, _, err := custom.New[options]().Resolve(custom.Map{"unknown": 1}, custom.Request{}); err != nil {
		t.Errorf("not strict: %v", err)
	}
	_, _, err := custom.New[options](custom.Strict()).Resolve(custom.Map{"unknown": 1}, custom.Request{})
	if err == nil || !strings.Contains(err.Error(), "invalid options") {
		t.Errorf("strict: %v", err)
	}
}

func TestDecodeParams(t *testing.T) {
	type p struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	got, err := custom.DecodeParams(custom.Map{"b": "x"}, p{A: 1, B: "y"})
	if err != nil || got != (p{A: 1, B: "x"}) {
		t.Errorf("DecodeParams = %+v, %v", got, err)
	}
	if _, err := custom.DecodeParams(custom.Map{"a": "x"}, p{}); err == nil {
		t.Error("a bad param is accepted")
	}
}
