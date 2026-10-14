// Package vim completes Vim function names via getcompletion(). It exists to
// show an extension that needs the editor: it uses sdk.Host.
package vim

import (
	"context"
	"fmt"

	"github.com/Shougo/goda.vim/sdk"
)

type source struct{}

func New() sdk.Source { return source{} }

func (source) Gather(ctx context.Context, h sdk.Host, c sdk.Context) ([]sdk.Candidate, error) {
	if c.CompleteStr == "" {
		return nil, nil
	}
	res, err := h.Call(ctx, "getcompletion", c.CompleteStr, "function")
	if err != nil {
		return nil, err
	}
	items, ok := res.([]any)
	if !ok {
		return nil, fmt.Errorf("vim: unexpected getcompletion result %T", res)
	}
	out := make([]sdk.Candidate, 0, len(items))
	for _, it := range items {
		switch w := it.(type) {
		case string:
			out = append(out, sdk.Candidate{Word: w, Kind: "f", Menu: "[vim]"})
		case []byte:
			out = append(out, sdk.Candidate{Word: string(w), Kind: "f", Menu: "[vim]"})
		}
	}
	return out, nil
}
