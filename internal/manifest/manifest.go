// Package manifest parses goda.toml: which extensions to link into a plugin
// binary. It understands only the small TOML subset the manifest needs
// ([goda], [[source]], [[filter]] tables with string values), so goda-build
// stays dependency-free.
package manifest

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	DefaultModule  = "github.com/Shougo/goda.vim"
	DefaultVersion = "v0.0.0"
)

// Goda locates the goda.vim module itself (SDK and transports).
type Goda struct {
	Module  string
	Version string
	Replace string // local checkout, for development
}

// Ext is one extension: a Go package exporting New().
type Ext struct {
	Kind    string // "source" or "filter"
	Name    string // name used in configuration
	Package string // import path of the package
	Module  string // module containing it (defaults to Package, or the goda module for in-tree packages)
	Version string
	Replace string // local directory, relative to the manifest
	Line    int
}

type Manifest struct {
	Goda Goda
	Exts []Ext
}

func ParseFile(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

func Parse(src string) (*Manifest, error) {
	m := &Manifest{}
	section := ""
	cur := -1

	for n, raw := range strings.Split(src, "\n") {
		ln := n + 1
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") {
			header := line
			if i := strings.Index(header, "#"); i >= 0 {
				header = strings.TrimSpace(header[:i])
			}
			switch header {
			case "[goda]":
				section, cur = "goda", -1
			case "[[source]]", "[[filter]]":
				kind := strings.Trim(header, "[]")
				m.Exts = append(m.Exts, Ext{Kind: kind, Line: ln})
				section, cur = "ext", len(m.Exts)-1
			default:
				return nil, fmt.Errorf("line %d: unknown table %s", ln, header)
			}
			continue
		}

		key, val, err := parseKV(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", ln, err)
		}
		switch section {
		case "":
			return nil, fmt.Errorf("line %d: %q appears before any table", ln, key)
		case "goda":
			switch key {
			case "module":
				m.Goda.Module = val
			case "version":
				m.Goda.Version = val
			case "replace":
				m.Goda.Replace = val
			default:
				return nil, fmt.Errorf("line %d: unknown key %q in [goda]", ln, key)
			}
		case "ext":
			e := &m.Exts[cur]
			switch key {
			case "name":
				e.Name = val
			case "package":
				e.Package = val
			case "module":
				e.Module = val
			case "version":
				e.Version = val
			case "replace":
				e.Replace = val
			default:
				return nil, fmt.Errorf("line %d: unknown key %q in [[%s]]", ln, key, e.Kind)
			}
		}
	}
	return m, m.finish()
}

// parseKV parses `key = "value"` or `key = 'literal'`, with an optional
// trailing comment.
func parseKV(line string) (key, val string, err error) {
	i := strings.Index(line, "=")
	if i < 0 {
		return "", "", fmt.Errorf("expected key = \"value\", got %q", line)
	}
	key = strings.TrimSpace(line[:i])
	rest := strings.TrimSpace(line[i+1:])
	if key == "" || rest == "" {
		return "", "", fmt.Errorf("expected key = \"value\", got %q", line)
	}

	var tail string
	switch rest[0] {
	case '"':
		j := 1
		for j < len(rest) && rest[j] != '"' {
			if rest[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(rest) {
			return "", "", fmt.Errorf("unterminated string in %q", line)
		}
		val, err = strconv.Unquote(rest[:j+1])
		if err != nil {
			return "", "", fmt.Errorf("bad string %s: %w", rest[:j+1], err)
		}
		tail = rest[j+1:]
	case '\'':
		j := strings.IndexByte(rest[1:], '\'')
		if j < 0 {
			return "", "", fmt.Errorf("unterminated string in %q", line)
		}
		val = rest[1 : 1+j]
		tail = rest[j+2:]
	default:
		return "", "", fmt.Errorf("value of %q must be a quoted string", key)
	}
	if tail = strings.TrimSpace(tail); tail != "" && !strings.HasPrefix(tail, "#") {
		return "", "", fmt.Errorf("unexpected text after value: %q", tail)
	}
	return key, val, nil
}

// finish applies defaults and validates.
func (m *Manifest) finish() error {
	if m.Goda.Module == "" {
		m.Goda.Module = DefaultModule
	}
	if m.Goda.Version == "" {
		m.Goda.Version = DefaultVersion
	}

	seen := map[string]int{}
	for i := range m.Exts {
		e := &m.Exts[i]
		if !nameRe.MatchString(e.Name) {
			return fmt.Errorf("line %d: [[%s]] needs a name matching %s, got %q", e.Line, e.Kind, nameRe, e.Name)
		}
		if e.Package == "" {
			return fmt.Errorf("line %d: %s %q needs a package", e.Line, e.Kind, e.Name)
		}
		id := e.Kind + "/" + e.Name
		if prev, dup := seen[id]; dup {
			return fmt.Errorf("line %d: %s %q already defined at line %d", e.Line, e.Kind, e.Name, prev)
		}
		seen[id] = e.Line

		inTree := e.Package == m.Goda.Module || strings.HasPrefix(e.Package, m.Goda.Module+"/")
		switch {
		case e.Module == "" && inTree:
			e.Module = m.Goda.Module
		case e.Module == "":
			e.Module = e.Package
		}
		if e.Module == m.Goda.Module && (e.Version != "" || e.Replace != "") {
			return fmt.Errorf("line %d: %s %q lives in the goda module; set version/replace under [goda] instead", e.Line, e.Kind, e.Name)
		}
	}
	return nil
}
