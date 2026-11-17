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

// fileURL is the file:// URL of the directory path (C:/x is /C:/x).
func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
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
				// whose source cannot be downloaded: the proxy has its go.mod
				// only, as go reads it when a package is missing.
				"cfg/go.mod": "module example.com/cfg\n\ngo 1.21\n\nrequire example.com/godalib v1.0.0 // a comment\n",
				// With an argument, it runs a while: a binary that runs
				// (in another Vim, say) when it is built again.
				"cfg/main.go": "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"time\"\n\n" +
					"\t\"example.com/godalib\"\n)\n\n" +
					"func main() {\n\tif len(os.Args) > 1 {\n\t\ttime.Sleep(time.Minute)\n\t}\n" +
					"\tfmt.Println(godalib.Where)\n}\n",
				"lib/go.mod": "module example.com/godalib\n\ngo 1.22.1\n",
				"lib/lib.go": "package godalib\n\nconst Where = \"workspace\"\n",
				// Another directory of the same module: the first one is used.
				"lib2/go.mod": "module example.com/godalib\n\ngo 1.21\n",
				"lib2/lib.go": "package godalib\n\nconst Where = \"second\"\n",
				// A configuration that needs a package the module installed
				// (lib) does not have yet: an older plugin installed.
				"newer/go.mod":                             "module example.com/newer\n\ngo 1.21\n\nrequire example.com/godalib v1.0.0\n",
				"newer/main.go":                            "package main\n\nimport \"example.com/godalib/sub\"\n\nfunc main() { sub.F() }\n",
				"proxy/example.com/godalib/@v/list":        "v1.0.0\n",
				"proxy/example.com/godalib/@v/v1.0.0.info": "{\"Version\":\"v1.0.0\"}\n",
				"proxy/example.com/godalib/@v/v1.0.0.mod":  "module example.com/godalib\n\ngo 1.21\n",
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
				"GOPROXY="+fileURL(filepath.Join(dir, "proxy")), "GOSUMDB=off", "GOFLAGS=", "GOTOOLCHAIN=local")
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
				"no_workspace":      "failed",
				"no_workspace_bin":  "0",
				"no_workspace_hint": "0",
				"user_go_work":      "failed",
				"user_go_work_bin":  "0",
				"workspace":         "workspace",
				"gowork_restored":   "kept",
				"go_version":        "['go 1.22.1']",
				"uses":              "2",
				"other_used":        "0",
				"older_plugin":      "example.com/godalib: " + filepath.Join(dir, "lib") + " (without example.com/godalib/sub)",
				"older_hint":        "1",
				"running":           "workspace",
				"running_tmp":       "0",
				"aside_removed":     "[]",
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
