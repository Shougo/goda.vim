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
