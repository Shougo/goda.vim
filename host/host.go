// Package host is the entry point of a goda.vim program. A plugin binary's
// main is one line:
//
//	func main() { host.Main(myplugin.New(), otherplugin.New()) }
//
// Main picks the editor transport (--transport=nvim|vim|jsonl, which the
// Vim script passes for you), and serves every plugin's methods to the editor.
package host

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/Shougo/goda.vim/host/jsonl"
	"github.com/Shougo/goda.vim/host/nvim"
	"github.com/Shougo/goda.vim/host/vim"
	"github.com/Shougo/goda.vim/sdk"
)

// Transports lists the editor transports linked into every program.
var Transports = []string{"nvim", "vim", "jsonl"}

// Reserved is the plugin name of the methods goda.vim itself provides
// (currently goda.plugins).
const Reserved = "goda"

var (
	pluginName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	methodName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Main serves the plugins and exits the process. It returns normally only
// when the editor closed the channel.
func Main(plugins ...sdk.Plugin) {
	transport := flag.String("transport", "", "editor transport: "+strings.Join(Transports, ", "))
	flag.Parse()
	if err := Serve(*transport, plugins...); err != nil {
		fmt.Fprintln(os.Stderr, "goda:", err)
		os.Exit(1)
	}
}

// Serve is Main without flag parsing and without exiting.
func Serve(transport string, plugins ...sdk.Plugin) error {
	table, err := Table(plugins...)
	if err != nil {
		return err
	}
	switch transport {
	case "nvim":
		return nvim.Serve(table)
	case "vim":
		return vim.Serve(table)
	case "jsonl":
		return jsonl.Serve(table)
	case "":
		return fmt.Errorf("--transport is required (%s)", strings.Join(Transports, ", "))
	}
	return fmt.Errorf("unknown transport %q (want %s)", transport, strings.Join(Transports, ", "))
}

// Table flattens the plugins into the method table the transports serve, keyed
// by "plugin.method". It also adds goda.plugins, which answers with
// {plugin: [methods...]}.
func Table(plugins ...sdk.Plugin) (map[string]sdk.Handler, error) {
	table := map[string]sdk.Handler{}
	listing := map[string]any{}
	seen := map[string]bool{}

	for i, p := range plugins {
		if p == nil {
			return nil, fmt.Errorf("plugin #%d is nil", i)
		}
		name := p.Name()
		switch {
		case !pluginName.MatchString(name):
			return nil, fmt.Errorf("plugin name %q must match %s", name, pluginName)
		case name == Reserved:
			return nil, fmt.Errorf("plugin name %q is reserved", name)
		case seen[name]:
			return nil, fmt.Errorf("plugin %q registered twice", name)
		}
		seen[name] = true

		var methods []string
		for m, h := range p.Handlers() {
			if !methodName.MatchString(m) {
				return nil, fmt.Errorf("plugin %q: method name %q must match %s", name, m, methodName)
			}
			if h == nil {
				return nil, fmt.Errorf("plugin %q: method %q has a nil handler", name, m)
			}
			table[name+"."+m] = h
			methods = append(methods, m)
		}
		sort.Strings(methods)
		names := make([]any, len(methods))
		for i, m := range methods {
			names[i] = m
		}
		listing[name] = names
	}

	table[Reserved+".plugins"] = func(context.Context, sdk.Host, []any) (any, error) {
		return listing, nil
	}
	return table, nil
}
