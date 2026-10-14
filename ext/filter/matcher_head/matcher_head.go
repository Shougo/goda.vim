// Package matcherhead keeps candidates that start with the completed string
// (case-insensitive).
package matcherhead

import (
	"context"
	"strings"

	"github.com/Shougo/goda.vim/sdk"
)

type filter struct{}

func New() sdk.Filter { return filter{} }

func (filter) Filter(_ context.Context, c sdk.Context, in []sdk.Candidate) ([]sdk.Candidate, error) {
	prefix := strings.ToLower(c.CompleteStr)
	if prefix == "" {
		return in, nil
	}
	out := make([]sdk.Candidate, 0, len(in))
	for _, cand := range in {
		if strings.HasPrefix(strings.ToLower(cand.Word), prefix) {
			out = append(out, cand)
		}
	}
	return out, nil
}
