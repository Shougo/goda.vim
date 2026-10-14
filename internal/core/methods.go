package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Shougo/goda.vim/sdk"
)

// Handler serves one editor-facing method. params are generic decoded values
// (int64, float64, string, bool, nil, []any, map[string]any, or json.Number),
// whichever the transport's codec produces.
type Handler func(ctx context.Context, params []any) (any, error)

// Methods returns the methods every transport serves to the editor:
//
//	goda_gather(request)  run the completion pipeline (see sdk.GatherRequest)
//	goda_list()           names of the registered sources and filters
//
// The transports differ only in framing; what the methods mean lives here.
func Methods(reg *sdk.Registry, h sdk.Host) map[string]Handler {
	return map[string]Handler{
		"goda_gather": func(ctx context.Context, params []any) (any, error) {
			if len(params) < 1 {
				return nil, errors.New("goda_gather: request required")
			}
			var req sdk.GatherRequest
			if err := into(params[0], &req); err != nil {
				return nil, fmt.Errorf("goda_gather: bad request: %w", err)
			}
			return generic(Gather(ctx, reg, h, req))
		},
		"goda_list": func(context.Context, []any) (any, error) {
			return map[string]any{
				"sources": reg.SourceNames(),
				"filters": reg.FilterNames(),
			}, nil
		},
	}
}

// into converts a generic decoded value into a typed destination (via JSON).
func into(v any, dst any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

// generic converts a typed value (with json tags) into generic form. Numbers
// become json.Number so integers stay integers through any codec.
func generic(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
