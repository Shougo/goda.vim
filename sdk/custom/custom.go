// Package custom is the options of a plugin in the style of ddu.vim,
// ddt.vim and ddx.vim: those of the configuration (PatchGlobal,
// PatchLocal), of the plugin's #custom#patch_global() and
// #custom#patch_local() (a Request), and of the start, merged in that order
// (Resolve). The options of type T are a struct whose json tags are the
// names of the options.
//
//	type Options struct {
//		Name     string                    `json:"name,omitempty"`
//		UI       string                    `json:"ui,omitempty"`
//		UIParams map[string]map[string]any `json:"uiParams,omitempty"`
//	}
//
//	c := custom.New[Options](custom.Params("uiParams"))
//	c.PatchGlobal(Options{UI: "shell"})
//	o, _, err := c.Resolve(user, request)
//
// How options are merged is set by New: by default, an option replaces
// that of before; with Params, the named options are maps of params (by the
// name of a UI, say) merged param by param, as ddt.vim; with Deep, every map
// is merged recursively and a nil value keeps that of before, as ddu.vim.
package custom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
)

// Map is options as Vim sends them.
type Map = map[string]any

// Request is the patches of the plugin's #custom#patch_global() and
// #custom#patch_local(), which every request carries.
type Request struct {
	Global Map            `json:"global"`
	Local  map[string]Map `json:"local"`
}

// Option is how New merges and decodes the options.
type Option func(*config)

type config struct {
	params map[string]bool
	deep   bool
	strict bool
}

// Params merges the options named param by param: each is a map of maps
// (by the name of a UI, say), each of them merged key by key. A value of a
// param that is a map itself is replaced whole. (As ddt.vim and ddx.vim.)
func Params(names ...string) Option {
	return func(c *config) {
		for _, name := range names {
			c.params[name] = true
		}
	}
}

// Deep merges every map recursively, and a nil value keeps that of before.
// (As ddu.vim.)
func Deep() Option { return func(c *config) { c.deep = true } }

// Strict makes an option that T does not have an error when the options are
// decoded.
func Strict() Option { return func(c *config) { c.strict = true } }

// Patches are the options of the configuration, for options of type T.
type Patches[T any] struct {
	config
	global Map
	local  map[string]Map
}

// New returns empty Patches.
func New[T any](opts ...Option) *Patches[T] {
	p := &Patches[T]{config: config{params: map[string]bool{}}, local: map[string]Map{}}
	for _, o := range opts {
		o(&p.config)
	}
	return p
}

// PatchGlobal patches the options of every session.
func (p *Patches[T]) PatchGlobal(o T) error {
	m, err := ToMap(o)
	if err != nil {
		return err
	}
	p.global = p.Merge(p.global, m)
	return nil
}

// PatchLocal patches the options of the session name.
func (p *Patches[T]) PatchLocal(name string, o T) error {
	m, err := ToMap(o)
	if err != nil {
		return err
	}
	p.local[name] = p.Merge(p.local[name], m)
	return nil
}

// Global is the options of every session, as patched (do not modify it).
func (p *Patches[T]) Global() Map { return p.global }

// Locals is the options of each session, as patched (do not modify them).
func (p *Patches[T]) Locals() map[string]Map { return p.local }

// Resolve is the options of a start with the options user, of the session
// they name: that of user, of the request, or of the configuration, or
// "default". The options of the configuration, its options of the session,
// those of the request, its options of the session, and user are merged in
// that order: Resolve gives them merged as a Map too.
func (p *Patches[T]) Resolve(user Map, r Request) (T, Map, error) {
	name, _ := user["name"].(string)
	if name == "" {
		name, _ = r.Global["name"].(string)
	}
	if name == "" {
		name, _ = p.global["name"].(string)
	}
	if name == "" {
		name = "default"
	}
	m := p.Merge(p.global, p.local[name])
	m = p.Merge(m, r.Global)
	m = p.Merge(m, r.Local[name])
	m = p.Merge(m, user)
	m["name"] = name
	o, err := p.Decode(m)
	return o, m, err
}

// Merge is the options of dst with those of src over them: a new map (those
// of dst are not modified).
func (p *Patches[T]) Merge(dst, src Map) Map {
	if p.deep {
		return mergeDeep(dst, src)
	}
	out := maps.Clone(dst)
	if out == nil {
		out = Map{}
	}
	for k, v := range src {
		if p.params[k] {
			out[k] = mergeParams(asMap(out[k]), asMap(v))
			continue
		}
		out[k] = v
	}
	return out
}

// Decode is the options m as a T (by its json tags).
func (p *Patches[T]) Decode(m Map) (T, error) {
	var o T
	b, err := json.Marshal(m)
	if err != nil {
		return o, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if p.strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(&o); err != nil {
		return o, fmt.Errorf("invalid options: %w", err)
	}
	return o, nil
}

func mergeParams(dst, src Map) Map {
	out := maps.Clone(dst)
	if out == nil {
		out = Map{}
	}
	for name, params := range src {
		m := maps.Clone(asMap(out[name]))
		if m == nil {
			m = Map{}
		}
		maps.Copy(m, asMap(params))
		out[name] = m
	}
	return out
}

// mergeDeep merges src into dst: nil values are unset, maps are merged
// recursively, the rest is replaced.
func mergeDeep(dst, src Map) Map {
	out := maps.Clone(dst)
	if out == nil {
		out = Map{}
	}
	for k, v := range src {
		if v == nil {
			continue
		}
		if sm, ok := v.(Map); ok {
			if dm, ok := out[k].(Map); ok {
				out[k] = mergeDeep(dm, sm)
				continue
			}
			out[k] = mergeDeep(nil, sm)
			continue
		}
		out[k] = v
	}
	return out
}

// asMap is v as a Map: a map[string]any, or another map of string keys
// (Params, map[string]Params, say).
func asMap(v any) Map {
	if m, ok := v.(Map); ok {
		return m
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	m := make(Map, rv.Len())
	for it := rv.MapRange(); it.Next(); {
		m[it.Key().String()] = it.Value().Interface()
	}
	return m
}

// ToMap is the options o as a Map (by their json tags). A Map is itself.
func ToMap(o any) (Map, error) {
	if m, ok := o.(Map); ok {
		return m, nil
	}
	b, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	m := Map{}
	return m, json.Unmarshal(b, &m)
}

// DecodeParams is defaults with params over it: the params of a UI, say, as
// its struct (json tags are the names of the params).
func DecodeParams[T any](params Map, defaults T) (T, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return defaults, err
	}
	p := defaults
	if err := json.Unmarshal(b, &p); err != nil {
		return defaults, fmt.Errorf("invalid params: %w", err)
	}
	return p, nil
}
