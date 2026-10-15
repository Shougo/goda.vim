// Package jsonval decodes JSON into the generic values every goda.vim
// transport hands to plugin handlers: nil, bool, int64, float64, string, []any
// and map[string]any. The msgpack transport produces the same kinds, so a
// handler sees identical types whichever editor it runs in.
package jsonval

import (
	"bytes"
	"encoding/json"
)

// Decode parses one JSON value. Integers become int64, other numbers float64.
func Decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return normalize(v), nil
}

func normalize(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case []any:
		for i := range x {
			x[i] = normalize(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = normalize(x[k])
		}
		return x
	default:
		return v
	}
}
