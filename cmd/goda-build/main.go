// Command goda-build links the extensions listed in a manifest into a single
// plugin binary:
//
//	goda-build -manifest goda.toml -o goda-cmp
//
// It writes a small generated Go module (go.mod + main.go) under
// <manifest dir>/.goda/build and runs `go build` there with CGO_ENABLED=0.
// Module resolution, checksum verification and caching are left to the Go
// toolchain. `go mod tidy` runs only when the generated go.mod changes.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Shougo/goda.vim/internal/gen"
	"github.com/Shougo/goda.vim/internal/manifest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "goda-build:", err)
		os.Exit(1)
	}
}

type options struct {
	manifest  string
	out       string
	work      string // default <manifest dir>/.goda/build
	transport string
}

func run() error {
	var o options
	flag.StringVar(&o.manifest, "manifest", "goda.toml", "manifest listing the extensions")
	flag.StringVar(&o.out, "o", "goda-host", "output binary")
	flag.StringVar(&o.work, "work", "", "generated module directory (default <manifest dir>/.goda/build)")
	flag.StringVar(&o.transport, "transport", "nvim",
		"transport the binary uses when run without --transport (nvim, vim or jsonl); all are linked in")
	flag.Parse()
	return build(o)
}

func build(o options) error {
	mp, err := filepath.Abs(o.manifest)
	if err != nil {
		return err
	}
	m, err := manifest.ParseFile(mp)
	if err != nil {
		return err
	}
	baseDir := filepath.Dir(mp)

	goMod, mainGo, err := gen.Render(m, baseDir, o.transport)
	if err != nil {
		return err
	}

	workDir := o.work
	if workDir == "" {
		workDir = filepath.Join(baseDir, ".goda", "build")
	}
	if workDir, err = filepath.Abs(workDir); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}

	// main.go must exist before `go mod tidy`: tidy derives the requirements
	// from the imports and would otherwise drop every one of them.
	if err := writeIfChanged(filepath.Join(workDir, "main.go"), mainGo); err != nil {
		return err
	}

	// `go mod tidy` rewrites go.mod, so the generated go.mod must not be
	// written back over it on every run. A marker holding the hash of the
	// generated go.mod + main.go tells whether this manifest was already tidied.
	sum := sha256.Sum256([]byte(goMod + "\x00" + mainGo))
	want := hex.EncodeToString(sum[:])
	marker := filepath.Join(workDir, ".tidied")
	modPath := filepath.Join(workDir, "go.mod")
	if prev, _ := os.ReadFile(marker); string(prev) != want || !exists(modPath) {
		if err := os.WriteFile(modPath, []byte(goMod), 0o644); err != nil {
			return err
		}
		if err := goCmd(workDir, "mod", "tidy"); err != nil {
			return err
		}
		if err := os.WriteFile(marker, []byte(want), 0o644); err != nil {
			return err
		}
	}

	outAbs, err := filepath.Abs(o.out)
	if err != nil {
		return err
	}
	return goCmd(workDir, "build", "-trimpath", "-o", outAbs, ".")
}

func goCmd(dir string, args ...string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	// Pure Go by contract: no C toolchain needed, easy cross-compilation.
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s: %w", args[0], err)
	}
	return nil
}

func writeIfChanged(path, content string) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == content {
		return nil // keep the mtime stable
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
