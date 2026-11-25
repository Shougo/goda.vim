// Package jsonval decodes JSON into the generic values every goda.vim
// transport hands to plugin handlers: nil, bool, int64, float64, string, []any
// and map[string]any. The msgpack transport produces the same kinds, so a
// handler sees identical types whichever editor it runs in.
package jsonval

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Decode parses one JSON value. Integers become int64, other numbers float64.
// A number out of the range of float64 ("1e700") is an error, as for
// json.Unmarshal: it would be ±Inf, which JSON cannot carry back.
func Decode(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return normalize(v)
}

func normalize(v any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i, nil
		}
		f, err := x.Float64()
		if err != nil {
			return nil, fmt.Errorf("jsonval: number %s out of range", x)
		}
		return f, nil
	case []any:
		for i := range x {
			e, err := normalize(x[i])
			if err != nil {
				return nil, err
			}
			x[i] = e
		}
		return x, nil
	case map[string]any:
		for k := range x {
			e, err := normalize(x[k])
			if err != nil {
				return nil, err
			}
			x[k] = e
		}
		return x, nil
	default:
		return v, nil
	}
}
