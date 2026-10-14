// Package around completes words found near the cursor.
package around

import (
	"context"
	"regexp"
	"unicode/utf8"

	"github.com/Shougo/goda.vim/sdk"
)

// maxDistance is how many lines above and below the cursor are scanned.
const maxDistance = 200

var wordRe = regexp.MustCompile(`[\p{L}_][\p{L}\p{N}_]*`)

type source struct{}

func New() sdk.Source { return source{} }

// Gather scans lines in order of distance from the cursor (the cursor line
// first, then the line below, then the line above, ...), so that nearby words
// come first. Duplicates and the word being completed are dropped.
func (source) Gather(ctx context.Context, _ sdk.Host, c sdk.Context) ([]sdk.Candidate, error) {
	seen := map[string]struct{}{c.CompleteStr: {}}
	var out []sdk.Candidate

	scan := func(idx int) {
		if idx < 0 || idx >= len(c.Lines) {
			return
		}
		for _, w := range wordRe.FindAllString(c.Lines[idx], -1) {
			if utf8.RuneCountInString(w) < 2 {
				continue
			}
			if _, dup := seen[w]; dup {
				continue
			}
			seen[w] = struct{}{}
			out = append(out, sdk.Candidate{Word: w, Menu: "[around]"})
		}
	}

	cur := c.Line - 1
	for d := 0; d <= maxDistance; d++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cur+d >= len(c.Lines) && cur-d < 0 {
			break
		}
		scan(cur + d)
		if d > 0 {
			scan(cur - d)
		}
	}
	return out, nil
}
