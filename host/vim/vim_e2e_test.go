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

// TestRealVim drives the actual plugin binary from an actual Vim, through the
// shipped autoload/goda.vim. It is the check that the wire format in the
// package comment matches what Vim really does. Skipped without a Vim that has
// +job and +channel.
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, vimBin, "-u", "NONE", "-i", "NONE", "-N", "-es", "-S", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GODA_BIN="+bin, "GODA_RTP="+root, "GODA_OUT="+out)
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

	type gathered struct {
		Candidates []struct{ Word string } `json:"candidates"`
		Errors     []struct{ Name, Message string }
	}
	gather := func(name string) (words []string, errs []string) {
		t.Helper()
		var g gathered
		if err := json.Unmarshal([]byte(res[name]), &g); err != nil {
			t.Fatalf("%s: %q is not a gather result: %v", name, res[name], err)
		}
		for _, c := range g.Candidates {
			words = append(words, c.Word)
		}
		for _, e := range g.Errors {
			errs = append(errs, e.Name+": "+e.Message)
		}
		return words, errs
	}

	if res["start"] != "" {
		t.Fatalf("goda#start failed: %s", res["start"])
	}
	if res["running"] != "1" {
		t.Errorf("running = %q, want 1", res["running"])
	}

	var list struct{ Sources, Filters []string }
	if err := json.Unmarshal([]byte(res["list"]), &list); err != nil {
		t.Fatalf("list: %q: %v", res["list"], err)
	}
	if want := []string{"around", "probe", "shout", "slow", "vim"}; !reflect.DeepEqual(list.Sources, want) {
		t.Errorf("sources = %v, want %v", list.Sources, want)
	}

	if w, e := gather("around"); !reflect.DeepEqual(w, []string{"foo", "foobar"}) || len(e) != 0 {
		t.Errorf("around: words %v errors %v", w, e)
	}

	// The nested path: the plugin's getcompletion call ran inside this Vim.
	// (Vim's function completion appends the opening parenthesis.)
	if w, e := gather("callback"); len(e) != 0 || !contains(w, "strlen(") {
		t.Errorf("callback: words %v errors %v", w, e)
	}

	wantProbe := []string{
		"eval=3:int64:<nil>",
		"cmd=<nil>:42:<nil>",
		"cmd-error=true",
		"call-error=true",
		"types=int64,float64,string,bool,<nil>",
		"error-string=ERROR:<nil>",
	}
	if w, e := gather("probe"); !reflect.DeepEqual(w, wantProbe) || len(e) != 0 {
		t.Errorf("probe:\n got %q\nwant %q\nerrors %v", w, wantProbe, e)
	}

	// A source that ignores its context is cut off; the others still answer.
	if w, e := gather("slow"); !reflect.DeepEqual(w, []string{"xray"}) ||
		len(e) != 1 || !strings.Contains(e[0], "timed out") {
		t.Errorf("slow: words %v errors %v", w, e)
	}

	if w, _ := gather("stdout"); !reflect.DeepEqual(w, []string{"shout"}) {
		t.Errorf("stdout: a stray Println must not break the protocol, got %v", w)
	}
	if !strings.Contains(res["log"], "this goes to stdout") {
		t.Errorf("stdout should be redirected to the log (stderr), log = %s", res["log"])
	}

	if v := res["unknown_method"]; !strings.HasPrefix(v, "EXC ") || !strings.Contains(v, "unknown method") {
		t.Errorf("unknown_method = %q, want an exception", v)
	}
	if _, e := gather("unknown_source"); len(e) != 1 || !strings.Contains(e[0], "not registered") {
		t.Errorf("unknown_source errors = %v", e)
	}

	if strings.HasPrefix(res["notify"], "EXC") {
		t.Errorf("notify failed: %s", res["notify"])
	}
	if !strings.Contains(res["after_notify"], `"sources"`) {
		t.Errorf("a request after a notification must still work: %s", res["after_notify"])
	}
	if res["stopped"] != "0" {
		t.Errorf("stopped = %q, want 0", res["stopped"])
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
