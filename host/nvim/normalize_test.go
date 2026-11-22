package nvim

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/Shougo/goda.vim/internal/msgpack"
	"github.com/Shougo/goda.vim/sdk"
)

// The args sent as they are encode as those of sdk.Generic.
func TestNormalizeSameAsGeneric(t *testing.T) {
	type item struct {
		Word string `json:"word"`
		N    int    `json:"n,omitempty"`
	}
	for _, tc := range []struct {
		name    string
		args    []any
		generic bool // sent as they are
	}{
		{"nil", nil, false},
		{"empty", []any{}, true},
		{"scalars", []any{nil, true, false, "abc", "日本語", int64(-40), 300, uint8(255), json.Number("12")}, true},
		{"nested", []any{map[string]any{"items": []any{map[string]any{"word": "foo", "n": int64(1)}}, "lnum": 3}}, true},
		{"strings", []any{[]string{"a", "b"}, "<&>"}, true},
		{"big ints", []any{int64(math.MaxInt64), int64(math.MinInt64), uint64(math.MaxInt64)}, true},
		{"float", []any{2.0}, false},
		{"float inside", []any{map[string]any{"ratio": 0.5}}, false},
		{"bad utf-8", []any{"a\xffb"}, false},
		{"bad utf-8 key", []any{map[string]any{"\xff": 1}}, false},
		{"bad utf-8 in []string", []any{[]string{"\xfe"}}, false},
		{"uint64 past int64", []any{uint64(math.MaxUint64)}, false},
		{"bytes", []any{[]byte("abc")}, false},
		{"struct", []any{item{Word: "w", N: 2}}, false},
		{"typed slice", []any{[]item{{Word: "w"}}}, false},
		{"typed map", []any{map[string]int{"a": 1}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.args != nil && generic(tc.args); got != tc.generic {
				t.Errorf("generic = %v, want %v", got, tc.generic)
			}
			norm, err := normalize(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.args
			if want == nil {
				want = []any{}
			}
			g, err := sdk.Generic(want)
			if err != nil {
				t.Fatal(err)
			}
			a, err := msgpack.Encode(norm)
			if err != nil {
				t.Fatal(err)
			}
			b, err := msgpack.Encode(g)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) {
				t.Errorf("encoded %x, sdk.Generic %x", a, b)
			}
		})
	}
}

func BenchmarkNormalize(b *testing.B) {
	items := []any{}
	for i := range 500 {
		items = append(items, map[string]any{"word": "word" + string(rune('a'+i%26)), "abbr": "word(x int)", "kind": "func", "menu": "[A]", "n": int64(i)})
	}
	args := []any{map[string]any{"items": items, "seq": int64(3)}}
	b.Run("normalize", func(b *testing.B) {
		for b.Loop() {
			normalize(args)
		}
	})
	b.Run("sdk.Generic", func(b *testing.B) {
		for b.Loop() {
			sdk.Generic(args)
		}
	})
}
