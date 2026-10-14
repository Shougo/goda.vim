// Package core runs the completion pipeline. It knows nothing about any
// editor: transports (host/nvim, host/jsonl, ...) feed it requests.
//
// Because extensions share the plugin's process, core is also the isolation
// layer: every extension call is guarded by recover and a deadline, so one
// broken extension is reported in Result.Errors instead of taking the whole
// plugin down or stalling the popup menu.
package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Shougo/goda.vim/sdk"
)

// DefaultTimeout bounds the whole pipeline when the request sets none.
const DefaultTimeout = 500 * time.Millisecond

// Gather runs the requested sources concurrently, then applies the filters in
// order to the concatenated candidates (source order is preserved).
func Gather(ctx context.Context, reg *sdk.Registry, h sdk.Host, req sdk.GatherRequest) sdk.GatherResult {
	timeout := DefaultTimeout
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res := sdk.GatherResult{Candidates: []sdk.Candidate{}, Errors: []sdk.ExtError{}}

	type out struct {
		cands []sdk.Candidate
		err   *sdk.ExtError
	}
	outs := make([]out, len(req.Sources))

	var wg sync.WaitGroup
	for i, name := range req.Sources {
		src, ok := reg.Source(name)
		if !ok {
			outs[i].err = &sdk.ExtError{Kind: "source", Name: name, Message: "not registered"}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			cands, err := guard(ctx, func() ([]sdk.Candidate, error) {
				return src.Gather(ctx, h, req.Context)
			})
			if err != nil {
				outs[i].err = &sdk.ExtError{Kind: "source", Name: name, Message: describe(err, timeout)}
				return
			}
			outs[i].cands = cands
		}()
	}
	wg.Wait()

	for _, o := range outs {
		if o.err != nil {
			res.Errors = append(res.Errors, *o.err)
			continue
		}
		res.Candidates = append(res.Candidates, o.cands...)
	}

	for _, name := range req.Filters {
		f, ok := reg.Filter(name)
		if !ok {
			res.Errors = append(res.Errors, sdk.ExtError{Kind: "filter", Name: name, Message: "not registered"})
			continue
		}
		in := res.Candidates
		next, err := guard(ctx, func() ([]sdk.Candidate, error) {
			return f.Filter(ctx, req.Context, in)
		})
		if err != nil {
			// A failing filter is skipped; its input flows on unchanged.
			res.Errors = append(res.Errors, sdk.ExtError{Kind: "filter", Name: name, Message: describe(err, timeout)})
			continue
		}
		if next == nil {
			next = []sdk.Candidate{}
		}
		res.Candidates = next
	}
	return res
}

// guard runs fn in its own goroutine so that a panic is recovered and an
// extension that ignores ctx cannot block the caller past the deadline.
// (Such a goroutine is leaked until it returns; Go cannot kill it.)
func guard[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				var zero T
				ch <- result{zero, fmt.Errorf("panic: %v", p)}
			}
		}()
		v, err := fn()
		ch <- result{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

func describe(err error, timeout time.Duration) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %s", timeout)
	}
	return err.Error()
}
