package msgpack

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
)

// Vectors come from the MessagePack specification.
func TestEncodeVectors(t *testing.T) {
	long := strings.Repeat("x", 300)
	arr20 := make([]any, 20)
	for i := range arr20 {
		arr20[i] = int64(0)
	}
	tests := []struct {
		name string
		in   any
		want string // hex
	}{
		{"nil", nil, "c0"},
		{"true", true, "c3"},
		{"false", false, "c2"},
		{"fixint", 1, "01"},
		{"uint8", 200, "ccc8"},
		{"uint16", 300, "cd012c"},
		{"uint32", 70000, "ce00011170"},
		{"negfix", -1, "ff"},
		{"int8", -33, "d0df"},
		{"int16", -200, "d1ff38"},
		{"int32", -70000, "d2fffeee90"},
		{"float64", 1.5, "cb3ff8000000000000"},
		{"fixstr", "a", "a161"},
		{"str8", strings.Repeat("y", 32), "d920" + strings.Repeat("79", 32)},
		{"str16", long, "da012c" + strings.Repeat("78", 300)},
		{"fixarray", []any{1, 2}, "920102"},
		{"array16", arr20, "dc0014" + strings.Repeat("00", 20)},
		{"fixmap", map[string]any{"a": 1}, "81a16101"},
		{"bin", []byte{1, 2}, "c4020102"},
		{"strslice", []string{"a", "b"}, "92a161a162"},
	}
	for _, tt := range tests {
		got, err := Encode(tt.in)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if h := hex.EncodeToString(got); h != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, h, tt.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := []any{
		int64(0), int64(-1), int64(127), int64(128), int64(65536), int64(-129), int64(-40000),
		int64(math.MaxInt64), int64(math.MinInt64), uint64(math.MaxUint64),
		"", "日本語", strings.Repeat("z", 70000),
		true, false, nil, 3.25,
		map[string]any{"k": []any{int64(1), map[string]any{"n": nil}}},
	}
	b, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewDecoder(bytes.NewReader(b)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, any(in)) {
		t.Fatalf("round trip mismatch:\n got %v\nwant %v", got, in)
	}
}

func TestDecodeStream(t *testing.T) {
	var buf bytes.Buffer
	for _, v := range []any{1, "two", []any{3}} {
		b, _ := Encode(v)
		buf.Write(b)
	}
	dec := NewDecoder(&buf)
	want := []any{int64(1), "two", []any{int64(3)}}
	for i, w := range want {
		got, err := dec.Decode()
		if err != nil || !reflect.DeepEqual(got, w) {
			t.Fatalf("value %d: got %v, %v; want %v", i, got, err, w)
		}
	}
	if _, err := dec.Decode(); err != io.EOF {
		t.Fatalf("want io.EOF at boundary, got %v", err)
	}
}

func TestDecodeTruncated(t *testing.T) {
	b, _ := Encode([]any{"hello", "world"})
	_, err := NewDecoder(bytes.NewReader(b[:len(b)-2])).Decode()
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("want ErrUnexpectedEOF, got %v", err)
	}
}

func TestDecodeExtAndBin(t *testing.T) {
	// fixext1 type 1 data 0x05; bin8 "ab"
	got, err := NewDecoder(bytes.NewReader([]byte{0xd4, 0x01, 0x05})).Decode()
	if err != nil || !reflect.DeepEqual(got, Ext{Type: 1, Data: []byte{5}}) {
		t.Fatalf("ext: %v, %v", got, err)
	}
	got, err = NewDecoder(bytes.NewReader([]byte{0xc4, 0x02, 'a', 'b'})).Decode()
	if err != nil || got != "ab" {
		t.Fatalf("bin: %v, %v", got, err)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := NewDecoder(bytes.NewReader([]byte{0xc1})).Decode(); err == nil {
		t.Fatal("0xc1 must be rejected")
	}
	// 4 GiB string claim must not allocate
	if _, err := NewDecoder(bytes.NewReader([]byte{0xdb, 0xff, 0xff, 0xff, 0xff})).Decode(); err == nil {
		t.Fatal("oversized length must be rejected")
	}
	deep := bytes.Repeat([]byte{0x91}, 2000)
	if _, err := NewDecoder(bytes.NewReader(deep)).Decode(); err == nil {
		t.Fatal("deep nesting must be rejected")
	}
}

// core hands results over as json.Number so that integers stay integers.
func TestEncodeJSONNumber(t *testing.T) {
	v := map[string]any{"n": json.Number("5"), "f": json.Number("1.5"), "s": []any{"x"}}
	b, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	// {"f":1.5,"n":5,"s":["x"]}: 5 is an integer, not a float
	want := "83a166cb3ff8000000000000a16e05a17391a178"
	if h := hex.EncodeToString(b); h != want {
		t.Fatalf("got %s, want %s", h, want)
	}
	if _, err := Encode(json.Number("not a number")); err == nil {
		t.Fatal("a malformed json.Number must be rejected")
	}
}
