// Package jsonl is a dev/test transport: one JSON GatherRequest per line on
// stdin, one JSON GatherResult per line on stdout. There is no editor behind
// it, so sdk.Host calls fail. It is handy for exercising a generated binary
// without Neovim and for build benchmarks.
package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/Shougo/goda.vim/internal/core"
	"github.com/Shougo/goda.vim/sdk"
)

func Serve(reg *sdk.Registry) error { return ServeIO(os.Stdin, os.Stdout, reg) }

func ServeIO(r io.Reader, w io.Writer, reg *sdk.Registry) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var req sdk.GatherRequest
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			res := sdk.GatherResult{
				Candidates: []sdk.Candidate{},
				Errors:     []sdk.ExtError{{Kind: "request", Message: err.Error()}},
			}
			if err := enc.Encode(res); err != nil {
				return err
			}
			continue
		}
		if err := enc.Encode(core.Gather(context.Background(), reg, noHost{}, req)); err != nil {
			return err
		}
	}
	return sc.Err()
}

type noHost struct{}

var errNoHost = errors.New("jsonl transport has no editor host")

func (noHost) Call(context.Context, string, ...any) (any, error) { return nil, errNoHost }
func (noHost) Eval(context.Context, string) (any, error)         { return nil, errNoHost }
func (noHost) Cmd(context.Context, string) error                 { return errNoHost }
