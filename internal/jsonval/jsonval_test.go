package jsonval

import (
	"reflect"
	"testing"
)

func TestDecodeNormalizesNumbers(t *testing.T) {
	got, err := Decode([]byte(`[1, 2.5, -3, {"a": [4]}, "s", true, null, 1e3]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []any{int64(1), 2.5, int64(-3), map[string]any{"a": []any{int64(4)}}, "s", true, nil, 1000.0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	for _, s := range []string{``, `{`, `[1,`, `nope`} {
		if _, err := Decode([]byte(s)); err == nil {
			t.Errorf("%q must be rejected", s)
		}
	}
}
