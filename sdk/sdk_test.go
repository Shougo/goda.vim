package sdk

import (
	"context"
	"reflect"
	"testing"
)

type nopSource struct{}

func (nopSource) Gather(context.Context, Host, Context) ([]Candidate, error) { return nil, nil }

type nopFilter struct{}

func (nopFilter) Filter(_ context.Context, _ Context, in []Candidate) ([]Candidate, error) {
	return in, nil
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.RegisterSource("b", nopSource{})
	r.RegisterSource("a", nopSource{})
	r.RegisterFilter("f", nopFilter{})

	if got, want := r.SourceNames(), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SourceNames = %v, want %v", got, want)
	}
	if _, ok := r.Source("a"); !ok {
		t.Fatal("source a not found")
	}
	if _, ok := r.Filter("nope"); ok {
		t.Fatal("unexpected filter")
	}
}

func TestRegistryDuplicatePanics(t *testing.T) {
	r := NewRegistry()
	r.RegisterSource("a", nopSource{})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	r.RegisterSource("a", nopSource{})
}
