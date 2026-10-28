// Package e2e_test drives plugin binaries from an actual Vim and an actual
// Neovim, through the shipped autoload/goda.vim: the check that the wire
// formats of host/vim and host/nvim match what the editors really do. An
// editor that is not installed is skipped.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// editor is one editor the scenario runs in.
type editor struct {
	name string // "vim" or "nvim", as Meta().Host reports it
	bin  string
	// args runs a script and exits, without a UI, with the messages
	// written to log.
	args func(script, log string) []string
}

// find is the editor $env names, or name in PATH.
func find(env, name string) (string, error) {
	if bin := os.Getenv(env); bin != "" {
		return bin, nil
	}
	return exec.LookPath(name)
}

func editors(t *testing.T) []editor {
	var eds []editor
	// GODA_VIM picks a Vim (on MS-Windows, the one in Git Bash's PATH is
	// MSYS2's, which reads its arguments as Unix paths).
	if bin, err := find("GODA_VIM", "vim"); err == nil {
		ver, _ := exec.Command(bin, "--version").Output()
		if bytes.Contains(ver, []byte("+job")) && bytes.Contains(ver, []byte("+channel")) {
			t.Logf("using %s (%s)", bytes.SplitN(ver, []byte("\n"), 2)[0], bin)
			eds = append(eds, editor{"vim", bin, func(s, log string) []string {
				return []string{"-u", "NONE", "-i", "NONE", "-N", "-es", "-V1" + log, "-S", s}
			}})
		}
	}
	if bin, err := find("GODA_NVIM", "nvim"); err == nil {
		ver, _ := exec.Command(bin, "--version").Output()
		t.Logf("using %s (%s)", bytes.SplitN(ver, []byte("\n"), 2)[0], bin)
		eds = append(eds, editor{"nvim", bin, func(s, log string) []string {
			return []string{"--headless", "-u", "NONE", "-i", "NONE", "-n", "-V1" + log, "-S", s}
		}})
	}
	return eds
}

// fixtures are the binaries the scenario starts.
type fixtures struct {
	bin, bin2, binCopy, binOld, binSilent, binExits string
}

func build(t *testing.T, root, dir string) fixtures {
	t.Helper()
	// (A binary needs its extension on MS-Windows.)
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	f := fixtures{
		bin:       filepath.Join(dir, "plugin"+exe),
		bin2:      filepath.Join(dir, "plugin2"+exe),
		binCopy:   filepath.Join(dir, "plugin-copy"+exe), // same plugin name as bin, other path
		binOld:    filepath.Join(dir, "plugin-old"+exe),  // answers with protocol 99
		binSilent: filepath.Join(dir, "plugin-silent"+exe),
		binExits:  filepath.Join(dir, "plugin-exits"+exe),
	}
	for out, pkg := range map[string]string{
		f.bin: "plugin", f.bin2: "plugin2", f.binOld: "oldproto",
		// Never answers: not a goda.vim binary at all.
		f.binSilent: "silent",
		// Exits before the handshake with a reason on stderr.
		f.binExits: "exits",
	} {
		cmd := exec.Command("go", "build", "-o", out, "./host/e2e/testdata/"+pkg)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("building %s: %v\n%s", pkg, err, b)
		}
	}
	data, err := os.ReadFile(f.bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.binCopy, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRealEditors(t *testing.T) {
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
	script, err := filepath.Abs("testdata/e2e.vim")
	if err != nil {
		t.Fatal(err)
	}
	f := build(t, root, t.TempDir())
	for _, ed := range eds {
		t.Run(ed.name, func(t *testing.T) { run(t, ed, root, script, f) })
	}
}

func run(t *testing.T, ed editor, root, script string, f fixtures) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	closeFile := filepath.Join(dir, "closed.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	log := filepath.Join(dir, "messages.txt")
	cmd := exec.CommandContext(ctx, ed.bin, ed.args(script, log)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GODA_CLOSE_FILE="+closeFile, "GODA_BIN="+f.bin, "GODA_RTP="+root, "GODA_OUT="+out,
		"GODA_BIN2="+f.bin2, "GODA_BIN_COPY="+f.binCopy, "GODA_BIN_OLD="+f.binOld,
		"GODA_BIN_SILENT="+f.binSilent, "GODA_BIN_EXITS="+f.binExits)
	if b, err := cmd.CombinedOutput(); err != nil {
		msgs, _ := os.ReadFile(log)
		res, _ := os.ReadFile(out)
		t.Fatalf("%s: %v\n%s\nmessages:\n%s\nresults so far:\n%s", ed.name, err, b, msgs, res)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("%s wrote no results: %v", ed.name, err)
	}
	res := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(raw), "\n"), "\n") {
		name, val, _ := strings.Cut(line, "\t")
		res[name] = compact(val)
	}
	t.Logf("results:\n%s", raw)

	if res["start"] != `["test"]` {
		t.Fatalf("goda#start = %s, want [\"test\"]", res["start"])
	}
	if res["running"] != "1" {
		t.Errorf("running = %q, want 1", res["running"])
	}

	want := map[string]string{
		"start_again":           `["test"]`,
		"start_other":           `["other"]`,
		"plugins":               `["other","test"]`,
		"other_ping":            `"pong"`,
		"other_running":         "0",
		"test_after_other_stop": `["alive"]`,
		"echo":                  `[1,2.5,"x",[true,null],{"k":["日本語"]}]`,
		"meta":                  `"` + ed.name + `"`,
		// Still usable after a request timed out (Vim) or took long (Neovim),
		// after a notification, and after the late answer arrived.
		"after_slow":        `["still alive"]`,
		"after_notify":      `["after notify"]`,
		"after_late_answer": `["after the late answer"]`,
		"stopped":           "0",
	}
	for name, w := range want {
		if res[name] != w {
			t.Errorf("%s = %s\n  want %s", name, res[name], w)
		}
	}

	// goda#stop() closes the input of the job, and the host must have called
	// the plugin's Close (sdk.Closer) before exiting.
	var closed []byte
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if closed, _ = os.ReadFile(closeFile); len(closed) > 0 {
			break
		}
	}
	if !strings.Contains(string(closed), "closed") {
		t.Errorf("Close was not called when the job was stopped (file: %q)", closed)
	}

	// The plugin's getcompletion call ran inside this Vim. (Vim's function
	// completion appends the opening parenthesis.)
	var cb []string
	if err := json.Unmarshal([]byte(res["callback"]), &cb); err != nil || !contains(cb, "strlen(") {
		t.Errorf("callback = %s, want a list containing strlen(", res["callback"])
	}

	// A struct passed to Call reaches Vim with its json tags as dict keys.
	var st []map[string]string
	if err := json.Unmarshal([]byte(res["struct"]), &st); err != nil ||
		!reflect.DeepEqual(st, []map[string]string{{"word": "w", "kind": "k"}}) {
		t.Errorf("struct = %s", res["struct"])
	}

	wantProbe := []string{
		"eval=3:int64:<nil>",
		"cmd=<nil>:42:<nil>",
		"cmd-error=true",
		"call-error=true",
		"types=int64,float64,string,bool,<nil>",
		"error-string=ERROR:<nil>",
	}
	var probe []string
	if err := json.Unmarshal([]byte(res["probe"]), &probe); err != nil || !reflect.DeepEqual(probe, wantProbe) {
		t.Errorf("probe:\n got %s\nwant %q", res["probe"], wantProbe)
	}

	// What Notify sent ran before the Eval that followed it.
	if res["notify_order"] != `"yes"` {
		t.Errorf("notify_order = %s, want \"yes\"", res["notify_order"])
	}
	if !strings.Contains(res["notify_log"], "[notify] goda_no_such_function: ") {
		t.Errorf("notify_log = %s", res["notify_log"])
	}
	if res["stdout"] != `"shout"` {
		t.Errorf("a stray Println must not break the protocol, got %s", res["stdout"])
	}
	if !strings.Contains(res["log"], "this goes to stdout") {
		t.Errorf("stdout should be redirected to the log (stderr), log = %s", res["log"])
	}

	for name, want := range map[string]string{
		"unknown_method":   "unknown method",
		"unknown_plugin":   "unknown plugin",
		"reserved":         "reserved",
		"conflict":         "already served",
		"old_protocol":     "protocol 99",
		"other_after_stop": "unknown plugin",
	} {
		if v := res[name]; !strings.HasPrefix(v, "EXC ") || !strings.Contains(v, want) {
			t.Errorf("%s = %q, want an exception mentioning %q", name, v, want)
		}
	}
	// The reason the binary gave, not a guess about module versions.
	if v := res["exits"]; !strings.Contains(v, "exited during the handshake (status 3)") ||
		!strings.Contains(v, `unknown filter "zz"`) || strings.Contains(v, "older goda.vim") {
		t.Errorf("exits = %q", v)
	}
	if v := res["exits_log"]; !strings.Contains(v, "["+filepath.Base(f.binExits)+"] configuration: unknown filter") {
		t.Errorf("goda#log() after a failed start = %s", v)
	}
	// Vim gives up after g:goda#request_timeout; Neovim cannot and waits.
	if ed.name == "vim" {
		if v := res["slow"]; !strings.HasPrefix(v, "EXC ") || !strings.Contains(v, "no answer") {
			t.Errorf("slow = %q, want a 'no answer' exception", v)
		}
	} else if v := res["slow"]; v != `"late"` {
		t.Errorf("slow = %q, want \"late\"", v)
	}
	// A binary that never answers the handshake does not hang the editor,
	// Neovim included (whose rpcrequest() cannot time out).
	silentWant := map[string]string{"vim": "no answer", "nvim": "did not answer the handshake"}[ed.name]
	if v := res["silent"]; !strings.HasPrefix(v, "EXC ") || !strings.Contains(v, silentWant) {
		t.Errorf("silent = %q, want an exception mentioning %q", v, silentWant)
	}
	if strings.HasPrefix(res["notify"], "EXC") {
		t.Errorf("notify failed: %s", res["notify"])
	}
}

func contains(ss []string, s string) bool {
	return slices.Contains(ss, s)
}

// compact removes the spaces Neovim's json_encode() puts after separators,
// so that results compare the same in both editors.
func compact(v string) string {
	var b bytes.Buffer
	if json.Compact(&b, []byte(v)) == nil {
		return b.String()
	}
	return v
}
