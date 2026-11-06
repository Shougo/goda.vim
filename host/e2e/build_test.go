package e2e_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestBuild builds a configuration with goda#build(): the module it
// requires comes from g:goda#workspace.
func TestBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries and starts editors")
	}
	eds := editors(t)
	if len(eds) == 0 {
		t.Skip("neither vim (with +job and +channel) nor nvim is installed")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/build.vim")
	if err != nil {
		t.Fatal(err)
	}
	for _, ed := range eds {
		t.Run(ed.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, map[string]string{
				// The configuration requires a version of example.com/godalib
				// that cannot be downloaded (GOPROXY=off).
				"cfg/go.mod": "module example.com/cfg\n\ngo 1.21\n\nrequire example.com/godalib v1.0.0 // a comment\n",
				"cfg/main.go": "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/godalib\"\n)\n\n" +
					"func main() { fmt.Println(godalib.Where) }\n",
				"lib/go.mod": "module example.com/godalib\n\ngo 1.22.1\n",
				"lib/lib.go": "package godalib\n\nconst Where = \"workspace\"\n",
				// Another directory of the same module: the first one is used.
				"lib2/go.mod": "module example.com/godalib\n\ngo 1.21\n",
				"lib2/lib.go": "package godalib\n\nconst Where = \"second\"\n",
				// Not required by the configuration, and broken.
				"other/go.mod": "module example.com/other\n\ngo 1.99\n\nrequire nonexistent.invalid/x v1.0.0\n",
			})
			out := filepath.Join(dir, "out.txt")
			log := filepath.Join(dir, "messages.txt")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, ed.bin, ed.args(script, log)...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GODA_RTP="+root, "GODA_OUT="+out, "GODA_BUILD_DIR="+dir,
				"GOPROXY=off", "GOFLAGS=", "GOTOOLCHAIN=local")
			if b, err := cmd.CombinedOutput(); err != nil {
				msgs, _ := os.ReadFile(log)
				t.Fatalf("%v\n%s\n%s", err, b, msgs)
			}
			b, err := os.ReadFile(out)
			if err != nil {
				msgs, _ := os.ReadFile(log)
				t.Fatalf("no results: %v\n%s", err, msgs)
			}
			got := map[string]string{}
			for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
				k, v, _ := strings.Cut(line, "\t")
				got[k] = v
			}
			want := map[string]string{
				"no_workspace":     "failed",
				"no_workspace_bin": "0",
				"workspace":        "workspace",
				"gowork_restored":  "kept",
				"go_version":       "['go 1.22.1']",
				"uses":             "2",
				"other_used":       "0",
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
