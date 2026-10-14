// Package sdk is the contract between goda.vim and its extensions.
//
// An extension is an ordinary Go package that exports
//
//	func New() sdk.Source // or sdk.Filter
//
// and is linked into the plugin binary by goda-build. Extensions run inside
// the plugin's process, so they must be cheap and must honour ctx.
package sdk

import (
	"context"
	"fmt"
	"sort"
)

// Candidate is a single completion item.
type Candidate struct {
	Word string `json:"word"`
	Abbr string `json:"abbr,omitempty"`
	Kind string `json:"kind,omitempty"`
	Menu string `json:"menu,omitempty"`
	Info string `json:"info,omitempty"`
}

// Context describes the editing state at the time of completion.
type Context struct {
	Input       string   `json:"input"`        // text before the cursor
	CompleteStr string   `json:"complete_str"` // the word being completed
	Filetype    string   `json:"filetype"`
	Line        int      `json:"line"`  // 1-based cursor line, index into Lines
	Lines       []string `json:"lines"` // buffer lines around the cursor
}

// Host is the editor. Implementations exist per transport (Neovim, Vim, ...).
type Host interface {
	// Call calls a Vim function and returns its decoded result.
	Call(ctx context.Context, fn string, args ...any) (any, error)
	// Eval evaluates a Vim expression.
	Eval(ctx context.Context, expr string) (any, error)
	// Cmd executes an Ex command.
	Cmd(ctx context.Context, cmd string) error
}

// Source produces candidates.
type Source interface {
	Gather(ctx context.Context, h Host, c Context) ([]Candidate, error)
}

// Filter transforms a candidate list (match, sort, convert).
type Filter interface {
	Filter(ctx context.Context, c Context, in []Candidate) ([]Candidate, error)
}

// GatherRequest is what the editor sends to run the completion pipeline.
type GatherRequest struct {
	Sources   []string `json:"sources"`
	Filters   []string `json:"filters"`
	Context   Context  `json:"context"`
	TimeoutMs int      `json:"timeout_ms,omitempty"`
}

// ExtError reports a failure of a single extension without failing the rest.
type ExtError struct {
	Kind    string `json:"kind"` // "source", "filter" or "request"
	Name    string `json:"name"`
	Message string `json:"message"`
}

// GatherResult is the pipeline output.
type GatherResult struct {
	Candidates []Candidate `json:"candidates"`
	Errors     []ExtError  `json:"errors"`
}

// Registry maps names used in configuration to extension instances.
// It is filled once at startup and read-only afterwards.
type Registry struct {
	sources map[string]Source
	filters map[string]Filter
}

func NewRegistry() *Registry {
	return &Registry{sources: map[string]Source{}, filters: map[string]Filter{}}
}

// RegisterSource panics on an empty or duplicate name: that is a programming
// error in the generated main, not a runtime condition.
func (r *Registry) RegisterSource(name string, s Source) {
	if name == "" || s == nil {
		panic("sdk: RegisterSource: empty name or nil source")
	}
	if _, dup := r.sources[name]; dup {
		panic(fmt.Sprintf("sdk: source %q registered twice", name))
	}
	r.sources[name] = s
}

func (r *Registry) RegisterFilter(name string, f Filter) {
	if name == "" || f == nil {
		panic("sdk: RegisterFilter: empty name or nil filter")
	}
	if _, dup := r.filters[name]; dup {
		panic(fmt.Sprintf("sdk: filter %q registered twice", name))
	}
	r.filters[name] = f
}

func (r *Registry) Source(name string) (Source, bool) { s, ok := r.sources[name]; return s, ok }
func (r *Registry) Filter(name string) (Filter, bool) { f, ok := r.filters[name]; return f, ok }

func (r *Registry) SourceNames() []string { return sortedKeys(r.sources) }
func (r *Registry) FilterNames() []string { return sortedKeys(r.filters) }

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
