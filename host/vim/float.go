package vim

import (
	"bytes"
	"encoding/json"
)

// Vim reads a JSON number without a fraction or an exponent ("1") as a
// Number, and json.Marshal writes a whole float64 so.  The floats of the
// generic values (as nil, []any and map[string]any hold them) are written
// with ".0" instead, so that they reach Vim as Floats, as they reach Neovim
// through msgpack.  A float in a typed value (a struct) is written as JSON
// writes it.

type float64JSON float64

func (f float64JSON) MarshalJSON() ([]byte, error) {
	return withFraction(json.Marshal(float64(f)))
}

type float32JSON float32

func (f float32JSON) MarshalJSON() ([]byte, error) {
	return withFraction(json.Marshal(float32(f)))
}

func withFraction(b []byte, err error) ([]byte, error) {
	if err == nil && !bytes.ContainsAny(b, ".eE") {
		b = append(b, ".0"...)
	}
	return b, err
}

// floats is V with its floats as float64JSON and float32JSON.  V is not
// changed: what holds a float is copied.
func floats(v any) any {
	if !hasFloat(v) {
		return v
	}
	switch x := v.(type) {
	case float64:
		return float64JSON(x)
	case float32:
		return float32JSON(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = floats(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = floats(e)
		}
		return out
	}
	return v
}

func hasFloat(v any) bool {
	switch x := v.(type) {
	case float64, float32:
		return true
	case []any:
		for _, e := range x {
			if hasFloat(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if hasFloat(e) {
				return true
			}
		}
	}
	return false
}
