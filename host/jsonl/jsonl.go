// Package jsonl is a development transport: one JSON request per line on
// stdin, one JSON answer per line on stdout. There is no editor behind it, so
// every sdk.Host call fails. It exercises a plugin binary without an editor.
//
//	{"method": "plugin.method", "params": [...]}   ->   {"result": ...}
//	                                                    {"error": "message"}
//
// Requests are served one at a time, in order.
package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/Shougo/goda.vim/internal/jsonval"
	"github.com/Shougo/goda.vim/sdk"
)

func Serve(table map[string]sdk.Handler) error {
	return ServeIO(context.Background(), os.Stdin, os.Stdout, table)
}

func ServeIO(ctx context.Context, r io.Reader, w io.Writer, table map[string]sdk.Handler) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		res, err := serveOne(ctx, sc.Bytes(), table)
		out := map[string]any{"result": res}
		if err != nil {
			out = map[string]any{"error": err.Error()}
		}
		if err := enc.Encode(out); err != nil {
			// Most likely the result is not encodable: answer with that.
			if err2 := enc.Encode(map[string]any{"error": err.Error()}); err2 != nil {
				return err2
			}
		}
	}
	return sc.Err()
}

func serveOne(ctx context.Context, line []byte, table map[string]sdk.Handler) (res any, err error) {
	v, err := jsonval.Decode(line)
	req, ok := v.(map[string]any)
	if err != nil || !ok {
		return nil, errors.New(`malformed request: want {"method": ..., "params": [...]}`)
	}
	method, ok := req["method"].(string)
	if !ok {
		return nil, errors.New("malformed request: method must be a string")
	}
	var params []any
	if p, present := req["params"]; present {
		if params, ok = p.([]any); !ok {
			return nil, errors.New("malformed request: params must be an array")
		}
	}
	fn, ok := table[method]
	if !ok {
		return nil, fmt.Errorf("unknown method %q", method)
	}
	defer func() {
		if p := recover(); p != nil {
			res, err = nil, fmt.Errorf("panic in %s: %v", method, p)
		}
	}()
	return fn(ctx, noHost{}, params)
}

type noHost struct{}

var errNoHost = errors.New("jsonl transport has no editor host")

func (noHost) Meta() sdk.Meta                                    { return sdk.Meta{Host: sdk.HostNone} }
func (noHost) Call(context.Context, string, ...any) (any, error) { return nil, errNoHost }
func (noHost) Notify(context.Context, string, ...any) error      { return errNoHost }
func (noHost) Eval(context.Context, string) (any, error)         { return nil, errNoHost }
func (noHost) Cmd(context.Context, string) error                 { return errNoHost }
