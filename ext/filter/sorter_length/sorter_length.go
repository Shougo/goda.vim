// Package sorterlength orders candidates by length, shortest first (stable).
package sorterlength

import (
	"context"
	"sort"
	"unicode/utf8"

	"github.com/Shougo/goda.vim/sdk"
)

type filter struct{}

func New() sdk.Filter { return filter{} }

func (filter) Filter(_ context.Context, _ sdk.Context, in []sdk.Candidate) ([]sdk.Candidate, error) {
	out := make([]sdk.Candidate, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		return utf8.RuneCountInString(out[i].Word) < utf8.RuneCountInString(out[j].Word)
	})
	return out, nil
}
