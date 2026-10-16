package vim_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestRealVim drives a plugin binary from an actual Vim, through the shipped
// autoload/goda.vim. It is the check that the wire format in the package
// comment matches what Vim really does. Skipped without a Vim that has +job and
// +channel.
func TestRealVim(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts Vim")
	}
	vimBin, err := exec.LookPath("vim")
	if err != nil {
		t.Skip("vim is not installed")
	}
	ver, _ := exec.Command(vimBin, "--version").Output()
	if !bytes.Contains(ver, []byte("+job")) || !bytes.Contains(ver, []byte("+channel")) {
		t.Skip("this vim lacks +job/+channel")
	}
	t.Logf("using %s", bytes.SplitN(ver, []byte("\n"), 2)[0])

	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/e2e.vim")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "plugin")
	out := filepath.Join(dir, "out.txt")

	build := exec.Command("go", "build", "-o", bin, "./host/vim/testdata/plugin")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the plugin: %v\n%s", err, b)
	}

	bin2 := filepath.Join(dir, "plugin2")
	build2 := exec.Command("go", "build", "-o", bin2, "./host/vim/testdata/plugin2")
	build2.Dir = root
	build2.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := build2.CombinedOutput(); err != nil {
		t.Fatalf("building the second plugin: %v\n%s", err, b)
	}
	// Same plugin name as bin, other path.
	binCopy := filepath.Join(dir, "plugin-copy")
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binCopy, data, 0o755); err != nil {
		t.Fatal(err)
	}
	// Answers the handshake with a protocol version nobody speaks.
	binOld := filepath.Join(dir, "plugin-old")
	oldScript := "#!/bin/sh\nread line\necho '[1,{\"result\":{\"protocol\":99,\"version\":\"x\",\"plugins\":{}}}]'\nsleep 3\n"
	if err := os.WriteFile(binOld, []byte(oldScript), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, vimBin, "-u", "NONE", "-i", "NONE", "-N", "-es", "-S", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GODA_BIN="+bin, "GODA_RTP="+root, "GODA_OUT="+out,
		"GODA_BIN2="+bin2, "GODA_BIN_COPY="+binCopy, "GODA_BIN_OLD="+binOld)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vim: %v\n%s", err, b)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("vim wrote no results: %v", err)
	}
	res := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		name, val, _ := strings.Cut(line, "\t")
		res[name] = val
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
		"meta":                  `"vim"`,
		// Still usable after a request timed out, after a notification, and
		// after the late answer of the timed-out request arrived.
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
	if v := res["slow"]; !strings.HasPrefix(v, "EXC ") || !strings.Contains(v, "no answer") {
		t.Errorf("slow = %q, want a 'no answer' exception", v)
	}
	if strings.HasPrefix(res["notify"], "EXC") {
		t.Errorf("notify failed: %s", res["notify"])
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
