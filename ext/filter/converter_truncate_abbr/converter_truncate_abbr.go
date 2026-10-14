// Package convertertruncateabbr shortens long abbr fields for the popup menu.
// The limit is fixed for now; per-extension parameters are a later addition.
package convertertruncateabbr

import (
	"context"

	"github.com/Shougo/goda.vim/sdk"
)

const maxAbbr = 40

type filter struct{}

func New() sdk.Filter { return filter{} }

func (filter) Filter(_ context.Context, _ sdk.Context, in []sdk.Candidate) ([]sdk.Candidate, error) {
	out := make([]sdk.Candidate, len(in))
	copy(out, in)
	for i := range out {
		abbr := out[i].Abbr
		if abbr == "" {
			abbr = out[i].Word
		}
		if r := []rune(abbr); len(r) > maxAbbr {
			out[i].Abbr = string(r[:maxAbbr-1]) + "…"
		}
	}
	return out, nil
}
