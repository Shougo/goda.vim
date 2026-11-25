package vim

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// Vim reads "1" as a Number: the whole floats of generic values get ".0".
func TestFloats(t *testing.T) {
	in := []any{
		1.0, float32(2), -3.0, 0.5, 1e21, float32(0.1), math.SmallestNonzeroFloat64,
		map[string]any{"f": 0.0, "l": []any{4.0}}, int64(1), json.Number("5"), "6.0", nil,
	}
	orig := []any{
		1.0, float32(2), -3.0, 0.5, 1e21, float32(0.1), math.SmallestNonzeroFloat64,
		map[string]any{"f": 0.0, "l": []any{4.0}}, int64(1), json.Number("5"), "6.0", nil,
	}
	b, err := json.Marshal(floats(in))
	if err != nil {
		t.Fatal(err)
	}
	want := `[1.0,2.0,-3.0,0.5,1e+21,0.1,5e-324,{"f":0.0,"l":[4.0]},1,5,"6.0",null]`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	if !reflect.DeepEqual(in, orig) {
		t.Errorf("the value was changed: %#v", in)
	}
	// Nothing to change, nothing copied.
	plain := []any{int64(1), "x"}
	if got := floats(plain).([]any); &got[0] != &plain[0] {
		t.Error("a value without floats was copied")
	}
	// A float Go cannot write stays an error.
	if _, err := json.Marshal(floats([]any{math.Inf(1)})); err == nil {
		t.Error("+Inf was written")
	}
}
