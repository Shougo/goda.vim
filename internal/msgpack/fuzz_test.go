package msgpack

import (
	"bytes"
	"math"
	"testing"
)

// FuzzDecode feeds arbitrary bytes, as a misbehaving peer could send: the
// decoder must not panic, and every value it accepts (but Ext, which Encode
// does not write) must survive Encode and Decode again.
func FuzzDecode(f *testing.F) {
	for _, v := range []any{
		nil, true, int64(-1), int64(math.MaxInt64), uint64(math.MaxUint64), 3.25,
		"", "日本語", []any{int64(1), "x"},
		map[string]any{"k": []any{int64(1), map[string]any{"n": nil}}},
	} {
		b, err := Encode(v)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte{0xca, 0x7f, 0xc0, 0x00, 0x00}) // float32 NaN
	f.Add([]byte{0x81, 0x01, 0xc0})             // a map with an integer key
	f.Add([]byte{0xd4, 0x01, 0x02})             // fixext 1
	f.Add([]byte{0xdc, 0xff, 0xff})             // a truncated array
	f.Add([]byte{0xc1})                         // never used
	f.Fuzz(func(t *testing.T, b []byte) {
		d := NewDecoder(bytes.NewReader(b))
		for {
			v, err := d.Decode()
			if err != nil {
				return
			}
			if hasExt(v) {
				continue
			}
			e, err := Encode(v)
			if err != nil {
				t.Fatalf("Encode(%#v): %v", v, err)
			}
			w, err := NewDecoder(bytes.NewReader(e)).Decode()
			if err != nil {
				t.Fatalf("Decode(Encode(%#v)): %v", v, err)
			}
			if !same(v, w) {
				t.Fatalf("round trip:\n got %#v\nwant %#v", w, v)
			}
		}
	})
}

func hasExt(v any) bool {
	switch x := v.(type) {
	case Ext:
		return true
	case []any:
		for _, e := range x {
			if hasExt(e) {
				return true
			}
		}
	case map[string]any:
		for _, e := range x {
			if hasExt(e) {
				return true
			}
		}
	}
	return false
}

// same is reflect.DeepEqual, but NaN equals NaN.
func same(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && (x == y || math.IsNaN(x) && math.IsNaN(y))
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
