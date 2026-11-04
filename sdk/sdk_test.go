package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecode(t *testing.T) {
	type T struct {
		N int      `json:"n"`
		S []string `json:"s"`
		F float64  `json:"f"`
	}
	// The same value as each transport would deliver it.
	for name, src := range map[string]any{
		"int64":       map[string]any{"n": int64(7), "s": []any{"a"}, "f": 2.5},
		"float64":     map[string]any{"n": float64(7), "s": []any{"a"}, "f": 2.5},
		"json.Number": map[string]any{"n": json.Number("7"), "s": []any{"a"}, "f": json.Number("2.5")},
	} {
		var dst T
		if err := Decode(src, &dst); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if dst.N != 7 || len(dst.S) != 1 || dst.S[0] != "a" || dst.F != 2.5 {
			t.Errorf("%s: got %+v", name, dst)
		}
	}
	if err := Decode("not an object", &T{}); err == nil {
		t.Error("a mismatched shape must be an error")
	}
}

func TestGenericKeepsIntegers(t *testing.T) {
	type T struct {
		N int `json:"n"`
		F float64
	}
	g, err := Generic(T{N: 5, F: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	m := g.(map[string]any)
	if m["n"] != json.Number("5") || m["F"] != json.Number("1.5") {
		t.Fatalf("got %#v", m)
	}
	b, _ := json.Marshal(g)
	if string(b) != `{"F":1.5,"n":5}` {
		t.Fatalf("an integer must not turn into a float: %s", b)
	}
	if _, err := Generic(make(chan int)); err == nil {
		t.Error("an unmarshalable value must be an error")
	}
}

func TestGuard(t *testing.T) {
	ctx := context.Background()

	if v, err := Guard(ctx, func() (int, error) { return 3, nil }); v != 3 || err != nil {
		t.Errorf("success: %v, %v", v, err)
	}

	want := errors.New("plain failure")
	if _, err := Guard(ctx, func() (int, error) { return 0, want }); err != want {
		t.Errorf("an error must pass through unchanged, got %v", err)
	}

	_, err := Guard(ctx, func() (int, error) { panic("kaboom") })
	if err == nil || !strings.Contains(err.Error(), "panic: kaboom") {
		t.Errorf("a panic must become an error, got %v", err)
	}
}

func TestGuardDeadlineDoesNotBlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Guard(ctx, func() (int, error) {
		time.Sleep(2 * time.Second) // deliberately ignores ctx
		return 1, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Guard blocked for %v", d)
	}
}

func TestGuardCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Guard(ctx, func() (int, error) {
		time.Sleep(time.Second)
		return 1, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want Canceled, got %v", err)
	}
}

func TestDecodeArgs(t *testing.T) {
	type opts struct {
		Upper bool `json:"upper"`
	}
	var name string
	var o opts
	times := 1
	params := []any{"Shougo", map[string]any{"upper": true}}
	if err := DecodeArgs(params, &name, &o, Optional(&times)); err != nil {
		t.Fatal(err)
	}
	if name != "Shougo" || !o.Upper || times != 1 {
		t.Errorf("%q %+v %d", name, o, times)
	}
	if err := DecodeArgs([]any{"x", nil, int64(3)}, &name, &o, Optional(&times)); err != nil || times != 3 {
		t.Errorf("times %d: %v", times, err)
	}
	for _, c := range []struct {
		params []any
		dsts   []any
		err    string
	}{
		{[]any{}, []any{&name}, "1 argument expected, got 0"},
		{[]any{"a", "b"}, []any{&name}, "1 argument expected, got 2"},
		{[]any{"a", "b", "c"}, []any{&name, Optional(&name)}, "1 to 2 arguments expected, got 3"},
		{[]any{"a", "b"}, []any{&name, &times}, "argument 2: json: cannot unmarshal"},
		{[]any{"a"}, []any{Optional(&name), &times}, "argument 2 is required after an optional one"},
	} {
		err := DecodeArgs(c.params, c.dsts...)
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%v: %v, want %q", c.params, err, c.err)
		}
	}
}

// The generic values that dst holds as they are keep their integers.
func TestDecodeGeneric(t *testing.T) {
	type params map[string]any
	src := map[string]any{"n": int64(1), "l": []any{int64(2)}}
	var m map[string]any
	var p params
	var a any
	var l []any
	for _, dst := range []any{&m, &p, &a} {
		if err := Decode(src, dst); err != nil {
			t.Fatal(err)
		}
	}
	if err := Decode(src["l"], &l); err != nil {
		t.Fatal(err)
	}
	if m["n"] != int64(1) || p["n"] != int64(1) || a.(map[string]any)["n"] != int64(1) || l[0] != int64(2) {
		t.Errorf("%#v %#v %#v %#v", m, p, a, l)
	}
	var n int
	if err := Decode(int64(3), &n); err != nil || n != 3 {
		t.Errorf("int: %d %v", n, err)
	}
	s := "keep"
	if err := Decode(nil, &s); err != nil || s != "keep" {
		t.Errorf("nil: %q %v", s, err)
	}
	if err := Decode(src, &s); err == nil {
		t.Error("a map into a string must be an error")
	}
}
