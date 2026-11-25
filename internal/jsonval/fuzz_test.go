package jsonval

import (
	"encoding/json"
	"math"
	"testing"
)

// FuzzDecode feeds arbitrary bytes: Decode must not panic, and every value it
// accepts must survive json.Marshal (what the transports send back) and
// Decode again.  A number keeps its value, not its kind: json.Marshal writes
// a whole float64 as an integer ("0e0" comes back as int64(0)).
func FuzzDecode(f *testing.F) {
	for _, s := range []string{
		`null`, `true`, `-1`, `9223372036854775807`, `9223372036854775808`,
		`3.25`, `1e308`, `"日本語"`, `[1,"x",{"k":[null]}]`, `{"a":{"b":[]}}`,
		`"\ud800"`, `[1,`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := Decode(b)
		if err != nil {
			return
		}
		if hasInf(v) {
			// Decode turns a number out of the range of float64 ("1e700")
			// into ±Inf, which JSON cannot carry back.  The editors do not
			// send one (Vim writes inf as "Infinity", which Decode rejects).
			return
		}
		e, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal(%#v) of %q: %v", v, b, err)
		}
		w, err := Decode(e)
		if err != nil {
			t.Fatalf("Decode(%q): %v", e, err)
		}
		if !same(v, w) {
			t.Fatalf("round trip of %q:\n got %#v\nwant %#v", b, w, v)
		}
	})
}

// same is reflect.DeepEqual, but numbers are compared by value.
func same(a, b any) bool {
	switch x := a.(type) {
	case int64, float64:
		return number(x) == number(b)
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !same(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !same(v, w) {
				return false
			}
		}
		return true
	}
	return a == b
}

// number is V as a float64, or NaN when V is not a number.
func number(v any) float64 {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case float64:
		return x
	}
	return math.NaN()
}

func hasInf(v any) bool {
	switch x := v.(type) {
	case float64:
		return math.IsInf(x, 0)
	case []any:
		for _, e := range x {
			if hasInf(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if hasInf(e) {
				return true
			}
		}
	}
	return false
}
