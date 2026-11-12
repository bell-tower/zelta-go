package backup

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/execx"
)

func TestUsageAndValidation(t *testing.T) {
	tests := []struct {
		verb  string
		opts  map[string]string
		args  []string
		usage string
		fail  string
		log   string
	}{
		{"backup", nil, nil, "no endpoints given", "", ""},
		{"backup", nil, []string{"a", "b", "c"}, "too many operands: c", "", ""},
		{"clone", nil, []string{"a", "b", "c"}, "too many operands: c", "", ""},
		{"backup", nil, []string{"a"}, "missing target endpoint argument", "", ""},
		{"rotate", nil, []string{"apool/x"}, "missing target endpoint argument", "", "error: did you mean 'zelta revert 'apool/x' ?"},
		{"revert", nil, []string{"apool/x", "bpool/y"}, "'revert' does not take a target argument", "", "error: did you mean 'zelta rotate' apool/x bpool/y ?"},
		{"backup", map[string]string{"USAGE": "1"}, []string{"a", "b"}, "", "", ""},
		{"backup", map[string]string{"DEPTH": "-1"}, []string{"a", "b"}, "", "depth of '-1' invalid; must be positive", ""},
		{"backup", map[string]string{"DEPTH": "x"}, []string{"a", "b"}, "", "depth of 'x' invalid; must be positive", ""},
		{"backup", map[string]string{"BOOKMARK_MODE": "2"}, []string{"a", "b"}, "", "invalid bookmark mode: 2", ""},
		{"clone", nil, []string{"apool/x", "bpool/y"}, "", "cannot clone: target pool doesn't match source", ""},
	}
	for _, tt := range tests {
		opts := map[string]string{"VERB": tt.verb}
		maps.Copy(opts, tt.opts)
		f := newFx(t, tt.verb, opts)
		err := f.backup(tt.args...)
		var ue *app.UsageError
		if tt.fail == "" {
			if !errors.As(err, &ue) || ue.Msg != tt.usage {
				t.Errorf("%s %v: err = %#v", tt.verb, tt.args, err)
			}
		} else if err == nil || err.Error() != tt.fail {
			t.Errorf("%s %v: err = %v want %q", tt.verb, tt.args, err, tt.fail)
		}
		eqLines(t, "log", f.tr.Err.String(), trimNL(tt.log+nl(tt.log))...)
		if len(f.tr.Fake.Calls) != 0 && tt.fail != "cannot clone: target pool doesn't match source" {
			t.Errorf("%s %v executed %v", tt.verb, tt.args, f.tr.Fake.Calls)
		}
	}
}

func nl(s string) string {
	if s == "" {
		return ""
	}
	return "\n"
}

func TestDatasetErrors(t *testing.T) {
	f := newFx(t, "backup", nil)
	f.missing("apool/src")
	if err := f.backup("apool/src", "bpool/tgt"); err == nil || err.Error() != "source dataset 'apool/src' does not exist" {
		t.Errorf("err = %v", err)
	}

	f = newFx(t, "backup", nil)
	f.on(getCmd("ssh -n me@h ", "apool/src", ""), execx.Fail(255, "ssh: Could not resolve hostname h: Name does not resolve"))
	if code := finish(f, f.backup("me@h:apool/src", "bpool/tgt")); code != 1 {
		t.Errorf("code = %d", code)
	}
	eqLines(t, "stderr", f.tr.Err.String(), "error: ssh: Could not resolve hostname h: Name does not resolve")

	f = newFx(t, "clone", map[string]string{"VERB": "clone"})
	f.props("apool/src", ". type filesystem").props("apool/copy", ". type filesystem")
	if err := f.backup("apool/src", "apool/copy"); err == nil || err.Error() != "cannot clone: target dataset 'apool/copy' already exists" {
		t.Errorf("clone exists err = %v", err)
	}

	f = newFx(t, "rotate", map[string]string{"VERB": "rotate"})
	f.props("apool/src", ". type filesystem").missing("bpool/tgt")
	if err := f.backup("apool/src", "bpool/tgt"); err == nil || err.Error() != "cannot 'rotate': target dataset 'bpool/tgt' does not exist" {
		t.Errorf("rotate missing err = %v", err)
	}

	// Unexpected property output is informational.
	f = newFx(t, "backup", map[string]string{"LOG_LEVEL": "3"})
	f.on(getCmd("", "apool/src", ""), execx.Out("apool/src\ttype\tfilesystem", "strange line")).missing("bpool/tgt").list("apool/src", "@a 1", ". 9").
		on("{ zfs send -P -L -c -e 'apool/src@a'|zfs recv -v "+fullRecvTop+" 'bpool/tgt' ; }", received("full", 1, 1))
	f.backup("apool/src", "bpool/tgt")
	contains(t, "info", f.tr.Err.String(), "strange line\n")
}

func TestParentCreation(t *testing.T) {
	create := "zfs create -vupo canmount=noauto 'bpool/a/b'"
	f := newFx(t, "backup", map[string]string{"LOG_LEVEL": "4"})
	f.props("apool/src", ". type filesystem").missing("bpool/a/b/tgt").list("apool/src", "@a 1", ". 9").
		on("{ zfs send -P -L -c -e 'apool/src@a'|zfs recv -v "+fullRecvTop+" 'bpool/a/b/tgt' ; }", received("full", 1, 1))
	f.tr.Fake.Once(create, execx.Fail(1, "create ancestors", "create bpool/a", "cannot create 'bpool/a/b': Read-only file system", "  canmount=noauto", ""))
	f.on(create, execx.Out("create bpool/a/b", "odd"))
	if err := f.backup("apool/src", "bpool/a/b/tgt"); err != nil {
		t.Fatal(err)
	}
	if n := len(f.tr.Fake.CallsMatching("zfs create")); n != 2 {
		t.Errorf("create attempts = %d", n)
	}
	contains(t, "stderr", f.tr.Err.String(), "validating target parent dataset: 'bpool/a/b'", "`zfs create` read-only bug detected",
		"attempted to create ancestor(s)", "property info: '  canmount=noauto'", "incomplete `zfs create`; retrying", "warning: unexpected `zfs create` output: 'odd'")

	for line, msg := range map[string]string{
		"cannot create 'bpool/a/b': permission denied":    "permission denied creating target 'bpool/a/b'",
		"cannot create 'bpool/a/b': no such pool 'bpool'": "no such pool in target path: 'bpool/a/b'",
	} {
		f = newFx(t, "backup", nil)
		f.props("apool/src", ". type filesystem").missing("bpool/a/b/tgt").on(create, execx.Fail(1, line))
		if err := f.backup("apool/src", "bpool/a/b/tgt"); err == nil || err.Error() != msg {
			t.Errorf("err = %v want %q", err, msg)
		}
	}

	f = newFx(t, "backup", map[string]string{"DRYRUN": "1"})
	f.props("apool/src", ". type filesystem").missing("bpool/a/b/tgt").list("apool/src", "@a 1", ". 9").on("zfs list -Ho name 'bpool/a/b'", execx.Fail(1, "cannot open 'bpool/a/b': dataset does not exist"))
	f.backup("apool/src", "bpool/a/b/tgt")
	contains(t, "dry run", f.tr.Out.String(), "+ "+create)
	if len(f.tr.Fake.CallsMatching("zfs create")) != 0 {
		t.Errorf("dry run created a parent")
	}

	f = newFx(t, "backup", map[string]string{"CREATE_PARENT": "0"})
	f.props("apool/src", ". type filesystem").missing("bpool/a/tgt").on("zfs list -Ho name 'bpool/a'", execx.Fail(1, "cannot open 'bpool/a': dataset does not exist"))
	if err := f.backup("apool/src", "bpool/a/tgt"); err == nil || err.Error() != "target has no parent dataset: 'bpool/a'" {
		t.Errorf("err = %v", err)
	}
	f = newFx(t, "backup", map[string]string{"CREATE_PARENT": "0", "DRYRUN": "1"})
	f.props("apool/src", ". type filesystem").missing("bpool/a/tgt").on("zfs list -Ho name 'bpool/a'", execx.Out("bpool/a")).list("apool/src", "@a 1", ". 9")
	if err := f.backup("apool/src", "bpool/a/tgt"); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestJSONSummary(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"LOG_MODE": "json"})
	f.props("apool/src", ". type filesystem", ". written 0").missing("bpool/tgt").list("apool/src", "@a 1", ". 9").
		on("{ zfs send -P -L -c -e 'apool/src@a'|zfs recv -v "+fullRecvTop+" 'bpool/tgt' ; }",
			execx.Out("size\t2048", "using provided clone origin x", "received 2K stream in 0.1 seconds (20K/sec)", "cannot receive compression property on bpool/tgt: permission denied"))
	code := finish(f, f.backup("apool/src", "bpool/tgt"))
	if code != 0 {
		t.Fatalf("code = %d %s", code, f.tr.Err.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(f.tr.RawOut.Bytes(), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, f.tr.RawOut.String())
	}
	want := map[string]any{
		"startTime": float64(testNow), "endTime": float64(testNow), "runTime": float64(0), "sourceDataset": "apool/src",
		"sourceEndpoint": "apool/src", "targetDataset": "bpool/tgt", "targetHost": "testhost", "sourceUser": "tester",
		"replicationSize": float64(2048), "replicationStreamsSent": float64(1), "replicationStreamsReceived": float64(1),
		"replicationErrorCode": float64(0), "replicationTime": 0.1, "targetsCloned": float64(1), "failed_props": "compression",
	}
	for k, v := range want {
		if doc[k] != v {
			t.Errorf("%s = %#v want %#v", k, doc[k], v)
		}
	}
	if streams, _ := doc["sentStreams"].([]any); len(streams) != 1 || streams[0] != "apool/src@a" {
		t.Errorf("sentStreams = %#v", doc["sentStreams"])
	}
	if msgs, _ := doc["errorMessages"].([]any); len(msgs) != 1 || !strings.Contains(msgs[0].(string), "missing `zfs allow` permissions: compression") {
		t.Errorf("errorMessages = %#v", doc["errorMessages"])
	}
	contains(t, "json-mode stderr", f.tr.Err.String(), "notice: syncing 1 datasets")

	// Early failures still report the run's identity.
	f = newFx(t, "backup", map[string]string{"LOG_MODE": "json"})
	f.missing("apool/src")
	finish(f, f.backup("apool/src", "bpool/tgt"))
	doc = map[string]any{}
	if err := json.Unmarshal(f.tr.RawOut.Bytes(), &doc); err != nil || doc["replicationErrorCode"] != float64(0) || doc["endTime"] != nil {
		t.Errorf("early json = %v %s", err, f.tr.RawOut.String())
	}
}

func TestDryRunExistingParent(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"DRYRUN": "1"})
	f.props("apool/src", ". type filesystem").missing("bpool/a/tgt").list("apool/src", "@a 1", ". 9").
		on("zfs list -Ho name 'bpool/a'", execx.Out("bpool/a"))
	f.backup("apool/src", "bpool/a/tgt")
	f.noUnmatched()
	notContains(t, "stdout", f.tr.Out.String(), "zfs create")
}

// TestPropertiesFallBackToAll covers zfs releases that reject the planned
// property list (snapshots_changed is OpenZFS 2.2+): the planner retries
// with "all" and plans exactly as before.
func TestPropertiesFallBackToAll(t *testing.T) {
	tr := app.NewTest("backup", map[string]string{"DRYRUN": "1"})
	planned := getCmd("", "apool/src", "")
	all := strings.Replace(planned, plannedProperties, "all", 1)
	tr.Fake.Once(planned, execx.Fail(2, "bad property list: invalid property 'snapshots_changed'", "usage:"))
	tr.Fake.On(all, execx.Out("apool/src\ttype\tfilesystem", "apool/src\twritten\t4096"))
	r := newRun(context.Background(), tr.Runtime, []string{"apool/src", "bpool/dst"})
	r.loadEndpoints()
	r.resetState()
	ok, err := r.loadProperties(src)
	if !ok || err != nil {
		t.Fatalf("loadProperties = %v, %v (calls %v)", ok, err, tr.Fake.Calls)
	}
	if got := r.dataset(src, "").prop("written"); got != "4096" || r.sum.sourceWritten != 4096 {
		t.Errorf("written = %q (%d)", got, r.sum.sourceWritten)
	}
	if len(tr.Fake.Calls) != 2 || tr.Fake.Calls[1] != all || strings.Contains(tr.Err.String(), "bad property") {
		t.Errorf("calls = %v, log = %q", tr.Fake.Calls, tr.Err.String())
	}
}

// TestPlannedPropertiesIgnoreUnsetValues pins the "-" rows that zfs prints
// for unset properties requested by name (but omits from "all"): an unset
// receive_resume_token must not look like a resumable transfer.
func TestPlannedPropertiesIgnoreUnsetValues(t *testing.T) {
	tr := app.NewTest("backup", nil)
	tr.Fake.On(getCmd("", "apool/src", ""), execx.Out(
		"apool/src\ttype\tfilesystem", "apool/src\twritten\t0", "apool/src\torigin\t-",
		"apool/src\treceive_resume_token\t-", "apool/src\tsnapshots_changed\t-",
		"apool/src/c\ttype\tfilesystem", "apool/src/c\torigin\tapool/x@s"))
	r := newRun(context.Background(), tr.Runtime, []string{"apool/src", "bpool/dst"})
	r.loadEndpoints()
	r.resetState()
	if ok, err := r.loadProperties(src); !ok || err != nil {
		t.Fatalf("loadProperties = %v, %v", ok, err)
	}
	top := r.dataset(src, "")
	for _, p := range []string{"origin", "receive_resume_token", "snapshots_changed"} {
		if v, set := top.props[p]; set {
			t.Errorf("%s = %q, want unset", p, v)
		}
	}
	if r.tree.snapshotsChangedMissing || r.dataset(src, "/c").prop("origin") != "apool/x@s" {
		t.Errorf("snapshotsChangedMissing=%v origin=%q", r.tree.snapshotsChangedMissing, r.dataset(src, "/c").prop("origin"))
	}
}
