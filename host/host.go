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
	"os/signal"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Shougo/goda.vim/host/jsonl"
	"github.com/Shougo/goda.vim/host/nvim"
	"github.com/Shougo/goda.vim/host/vim"
	"github.com/Shougo/goda.vim/sdk"
)

// Transports lists the editor transports linked into every program.
var Transports = []string{"nvim", "vim", "jsonl"}

// Reserved is the plugin name of the methods goda.vim itself provides
// (goda.plugins and the goda.hello handshake).
const Reserved = "goda"

var (
	pluginName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	methodName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// CloseTimeout bounds the time all plugins together get for sdk.Closer.
var CloseTimeout = 3 * time.Second

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

// Serve is Main without flag parsing and without exiting. It returns when the
// editor closed the connection, the transport failed, or the process got
// SIGTERM or an interrupt; in every case the plugins implementing sdk.Closer
// have been closed by then.
func Serve(transport string, plugins ...sdk.Plugin) error {
	table, err := Table(plugins...)
	if err != nil {
		return err
	}
	var serve func(map[string]sdk.Handler) error
	switch transport {
	case "nvim":
		serve = nvim.Serve
	case "vim":
		serve = vim.Serve
	case "jsonl":
		serve = jsonl.Serve
	case "":
		return fmt.Errorf("--transport is required (%s)", strings.Join(Transports, ", "))
	default:
		return fmt.Errorf("unknown transport %q (want %s)", transport, strings.Join(Transports, ", "))
	}

	// goda#stop() closes the input (serve returns), and the editor may stop
	// the job with SIGTERM (job_stop(), jobstop(), and when it exits); on
	// MS-Windows it kills it instead, so the input is closed first.
	sig, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- serve(table) }()
	var serveErr error
	select {
	case serveErr = <-errc:
	case <-sig.Done():
	}
	closePlugins(plugins, CloseTimeout)
	return serveErr
}

// closePlugins calls sdk.Closer on the plugins, last registered first, within
// timeout altogether. A panic or an error is reported on stderr and does not
// stop the others.
func closePlugins(plugins []sdk.Plugin, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for i := len(plugins) - 1; i >= 0; i-- {
		c, ok := plugins[i].(sdk.Closer)
		if !ok {
			continue
		}
		_, err := sdk.Guard(ctx, func() (struct{}, error) { return struct{}{}, c.Close(ctx) })
		if err != nil {
			fmt.Fprintf(os.Stderr, "goda: plugin %s: close: %v\n", plugins[i].Name(), err)
		}
	}
}

// Table flattens the plugins into the method table the transports serve, keyed
// by "plugin.method". It also adds goda.plugins, which answers with
// {plugin: [methods...]}, and goda.hello, the handshake the editor performs
// right after starting the binary: {protocol, version, plugins}.
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
	table[Reserved+".hello"] = func(context.Context, sdk.Host, []any) (any, error) {
		return map[string]any{
			"protocol": int64(sdk.Protocol),
			"version":  moduleVersion(),
			"plugins":  listing,
		}, nil
	}
	return table, nil
}

// moduleVersion is the version of goda.vim this binary was built with, as far
// as the Go toolchain recorded it. It is informational; compatibility is
// decided by sdk.Protocol.
func moduleVersion() string {
	const path = "github.com/Shougo/goda.vim"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if info.Main.Path == path {
		return info.Main.Version // "(devel)" when built inside the module
	}
	for _, d := range info.Deps {
		if d.Path == path {
			if d.Replace != nil {
				return d.Replace.Version + " (replaced)"
			}
			return d.Version
		}
	}
	return "unknown"
}
