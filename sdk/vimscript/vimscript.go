// Package vimscript gives the Vim script of a plugin (or of an extension of
// one) to the editor from its binary: the file is embedded in the Go package
// (go:embed), written in the cache directory at the path of an autoload
// script, and sourced before the first call of its functions. Nothing needs
// to be in 'runtimepath', and the Vim script is always that of the Go code
// built with it.
//
//	//go:embed file.vim
//	var src []byte
//
//	var script = vimscript.New("autoload/myplugin/kind/file.vim", src)
//
//	script.Call(ctx, h, "myplugin#kind#file#open", targets)
//
// The functions of the script are named as those of its autoload path
// (myplugin#kind#file#*); they may call its script-local functions.
package vimscript

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/Shougo/goda.vim/sdk"
)

// Script is a Vim script file of an ext.
type Script struct {
	path string
	src  []byte

	mu      sync.Mutex
	sourced bool
}

// New returns the script src, whose functions are named as those of the
// autoload script path ("autoload/myplugin/kind/file.vim":
// myplugin#kind#file#*).
func New(path string, src []byte) *Script {
	return &Script{path: path, src: src}
}

// Call calls the function fn of the script, after it is sourced.
func (s *Script) Call(ctx context.Context, h sdk.Host, fn string, args ...any) (any, error) {
	if err := s.Source(ctx, h); err != nil {
		return nil, err
	}
	return h.Call(ctx, fn, args...)
}

// Source sources the script once (again after a failure).
func (s *Script) Source(ctx context.Context, h sdk.Host) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sourced {
		return nil
	}
	file, err := s.write()
	if err != nil {
		return err
	}
	if err := h.Cmd(ctx, "source "+fnameescape(file)); err != nil {
		return fmt.Errorf("vimscript: %s: %w", s.path, err)
	}
	// Where it is, by its path: g:goda#vimscripts (to read it, or to
	// source it again).
	_ = h.Cmd(ctx, fmt.Sprintf("let g:goda#vimscripts = extend(get(g:, 'goda#vimscripts', {}), {%s: %s})",
		quote(s.path), quote(file)))
	s.sourced = true
	return nil
}

// fnameescape is Vim's fnameescape() of an absolute path.
func fnameescape(path string) string {
	special := " \t\n*?[{`$%#'\"|!<"
	if runtime.GOOS != "windows" {
		special += "\\"
	}
	var b strings.Builder
	for _, r := range path {
		if strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// quote is a Vim string literal of s.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// write writes the script in the cache directory (goda/vimscript/, a
// directory of its contents: the editors of other binaries keep theirs), at
// its autoload path: the names of its functions must be those of the path.
func (s *Script) write() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	sum := sha256.Sum256(s.src)
	file := filepath.Join(base, "goda", "vimscript", hex.EncodeToString(sum[:8]), filepath.FromSlash(s.path))
	if b, err := os.ReadFile(file); err == nil && string(b) == string(s.src) {
		return file, nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".vimscript-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(s.src); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		os.Remove(tmp.Name())
		// Another binary wrote it meanwhile.
		if b, rerr := os.ReadFile(file); rerr == nil && string(b) == string(s.src) {
			return file, nil
		}
		return "", err
	}
	return file, nil
}
