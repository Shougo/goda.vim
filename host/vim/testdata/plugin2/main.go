// Command plugin2 is a second, independent plugin binary for vim_e2e_test.go:
// one plugin ("other") with one method.
package main

import (
	"context"

	"github.com/Shougo/goda.vim/host"
	"github.com/Shougo/goda.vim/sdk"
)

type other struct{}

func (other) Name() string { return "other" }

func (other) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		"ping": func(context.Context, sdk.Host, []any) (any, error) { return "pong", nil },
	}
}

func main() { host.Main(other{}) }
