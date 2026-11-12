//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTree builds the ShellSpec suite's initial tree under src.
func makeTree(t *testing.T, src string) {
	t.Helper()
	key := filepath.Join(workDir, strings.ReplaceAll(src, "/", "_")+".key")
	sh(t, "dd if=/dev/urandom bs=32 count=1 of="+key+" 2>/dev/null")
	for _, ds := range []string{"sub1", "sub2", "sub2/orphan", "sub3", "sub3/space name", "sub4"} {
		sh(t, "zfs create -u '"+src+"/"+ds+"'")
	}
	sh(t, "zfs create -sV 8M "+src+"/sub4/zvol")
	sh(t, "zfs create -u -o encryption=on -o keyformat=raw -o keylocation=file://"+key+" "+src+"/sub4/encrypted")
}

func TestDivergentTree(t *testing.T) {
	src, tgt := tree(t, "divergent")
	makeTree(t, src)
	r := zelta(t, nil, "backup", "--snap-name", "@start", src, tgt)
	expectOK(t, r)
	if !strings.Contains(r.Stdout, "syncing 9 datasets") || strings.Contains(r.Stderr, "error:") {
		t.Fatalf("initial backup:\n%s\n%s", r.Stdout, r.Stderr)
	}
	sh(t, "zfs create -u "+src+"/sub1/child")
	sh(t, "zfs create -u "+tgt+"/sub1/kid")
	sh(t, "zfs destroy "+src+"/sub2@start")
	sh(t, "zfs snapshot '"+tgt+"/sub3/space name@blocker'")
	sh(t, "zfs destroy "+tgt+"/sub4/zvol@start")
	sh(t, "zfs snapshot "+src+"/sub3@two")
	sh(t, "zfs snapshot "+src+"/sub2@two")
	sh(t, "zfs snapshot "+tgt+"/sub2@two")

	r = zelta(t, nil, "match", src, tgt)
	expectOK(t, r)
	expectLines(t, r.Stdout,
		"DS_SUFFIX MATCH SRC_LAST TGT_LAST INFO",
		"[divergent] @start @start @start up-to-date",
		"/sub1 @start @start @start up-to-date",
		"/sub1/child - - - syncable (full)",
		"/sub1/kid - - - no source (target only)",
		"/sub2 - @two @two blocked sync: target diverged",
		"/sub2/orphan @start @start @start up-to-date",
		"/sub3 @start @two @start syncable (incremental)",
		"/sub3/space name @start @start @blocker blocked sync: target diverged",
		"/sub4 @start @start @start up-to-date",
		"/sub4/encrypted @start @start @start up-to-date",
		"/sub4/zvol - @start - blocked sync: no target snapshots",
		"5 up-to-date, 2 syncable, 4 blocked",
		"11 total datasets compared")

	r = zelta(t, nil, "rotate", src, tgt)
	expectOK(t, r)
	if r.Stderr != "warning: insufficient snapshots; performing full backup for 3 datasets\n" {
		t.Errorf("rotate stderr = %q", r.Stderr)
	}
	for _, want := range []string{"renaming '" + tgt + "' to '" + tgt + "_start'", "to ensure target is up-to-date, run: zelta backup " + src + " " + tgt, "10 streams received"} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("rotate output missing %q:\n%s", want, r.Stdout)
		}
	}
	r = zelta(t, nil, "match", src, tgt)
	expectOK(t, r)
	if !strings.Contains(r.Stdout, "7 up-to-date, 3 syncable") {
		t.Errorf("match after rotate:\n%s", r.Stdout)
	}
	r = zelta(t, nil, "backup", src, tgt)
	expectOK(t, r)
	for _, want := range []string{"syncing 10 datasets", "10 datasets up-to-date", "3 streams received"} {
		if !strings.Contains(r.Stdout, want) {
			t.Errorf("backup after rotate missing %q:\n%s", want, r.Stdout)
		}
	}
}

func TestIncrementalAndJSON(t *testing.T) {
	src, tgt := tree(t, "incr")
	sh(t, "zfs set mountpoint="+filepath.Join(mountDir, "incr")+" "+src)
	sh(t, "zfs create "+src+"/child")
	expectOK(t, zelta(t, nil, "backup", src, tgt))
	if got := snapshots(t, tgt); len(got) != 1 {
		t.Fatalf("target snapshots = %v", got)
	}
	sh(t, "dd if=/dev/urandom of="+filepath.Join(mountDir, "incr", "data")+" bs=64k count=4 2>/dev/null && sync")

	r := zelta(t, nil, "backup", "--json", src, tgt)
	expectOK(t, r)
	var doc map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &doc); err != nil {
		t.Fatalf("json: %v\n%s", err, r.Stdout)
	}
	if doc["replicationErrorCode"] != float64(0) || doc["sourceDataset"] != src || doc["targetDataset"] != tgt {
		t.Errorf("json fields: %v", doc)
	}
	if streams, _ := doc["sentStreams"].([]any); len(streams) == 0 {
		t.Errorf("json sentStreams: %v", doc)
	}
	if !strings.Contains(r.Stderr, "notice: source is written; snapshotting") {
		t.Errorf("json mode notices go to stderr: %q", r.Stderr)
	}
	r = zelta(t, nil, "match", "-H", "-o", "relname,info", src, tgt)
	expectOK(t, r)
	if r.Stdout != "\tup-to-date\n/child\tup-to-date\n" {
		t.Errorf("match -H = %q", r.Stdout)
	}
	if got := zelta(t, nil, "backup", src, tgt); !strings.Contains(got.Stdout, "2 datasets up-to-date") {
		t.Errorf("idempotent backup: %q", got.Stdout)
	}
}

func TestRevertAndClone(t *testing.T) {
	src, _ := tree(t, "revert")
	sh(t, "zfs create -u "+src+"/a && zfs snapshot -r "+src+"@one")
	r := zelta(t, nil, "clone", src, srcPool+"/revert_clone")
	expectOK(t, r)
	if !exists(srcPool+"/revert_clone/a") || !strings.Contains(r.Stdout, "cloned 2/2 datasets to "+srcPool+"/revert_clone") {
		t.Errorf("clone:\n%s\n%s", r.Stdout, r.Stderr)
	}
	sh(t, "zfs destroy -r "+srcPool+"/revert_clone")

	sh(t, "zfs create -u "+src+"/new")
	r = zelta(t, nil, "revert", "-qq", src+"@one")
	if r.Code == 0 || !exists(src+"/new") || exists(src+"_one") {
		t.Errorf("mixed tree revert must be rejected: code=%d", r.Code)
	}
	sh(t, "zfs destroy "+src+"/new")
	r = zelta(t, nil, "revert", src+"@one")
	expectOK(t, r)
	if !exists(src+"_one") || !exists(src+"/a") || !strings.Contains(r.Stdout, "to retain replica history, run: zelta rotate") {
		t.Errorf("revert:\n%s\n%s", r.Stdout, r.Stderr)
	}
}

func TestPruneAndZprune(t *testing.T) {
	src, _ := tree(t, "prune")
	for i := range 8 {
		sh(t, "zfs snapshot "+src+"@s"+string(rune('0'+i)))
	}
	r := zelta(t, nil, "prune", "--prune-num=3", src)
	expectOK(t, r)
	if strings.TrimSpace(r.Stdout) != src+"@s0%s4" {
		t.Errorf("prune candidates = %q", r.Stdout)
	}
	r = zelta(t, nil, "prune", "--visual", "--prune-num=3", src)
	if !strings.Contains(r.Stdout, "❌❌❌❌❌🔹🔹🔹") {
		t.Errorf("visual = %q", r.Stdout)
	}
	r = runProg(t, filepath.Join(filepath.Dir(zeltaBin), "zprune"), nil, "--prune-num=3", src)
	if r.Code == 0 || !strings.Contains(r.Stderr, "match endpoint required") {
		t.Errorf("zprune without guard: %d %q", r.Code, r.Stderr)
	}
	r = runProg(t, filepath.Join(filepath.Dir(zeltaBin), "zprune"), nil, "--force", "--no-prune-guard", "--prune-num=3", src)
	expectOK(t, r)
	if got := snapshots(t, src); len(got) != 3 {
		t.Errorf("snapshots after zprune = %v\n%s", got, r.Stdout)
	}
}

func TestRemoteTopologies(t *testing.T) {
	src, tgt := tree(t, "remote")
	sh(t, "zfs create -u '"+src+"/with space' && zfs snapshot -r "+src+"@r1")
	env := []string{"ZELTA_REMOTE_COMMAND=" + fakeSSH}
	cases := []struct {
		name     string
		src, tgt string
		flags    []string
	}{
		{"pull", "srchost:" + src, "tgthost:" + tgt + "/pull", nil},
		{"push", "srchost:" + src, "tgthost:" + tgt + "/push", []string{"--push"}},
		{"proxy", "srchost:" + src, "tgthost:" + tgt + "/proxy", []string{"--no-pull"}},
		{"remote-source", "srchost:" + src, tgt + "/rsrc", nil},
		{"remote-target", src, "tgthost:" + tgt + "/rtgt", nil},
		{"hairpin", "box:" + src, "box:" + tgt + "/hairpin", nil},
	}
	sh(t, "zfs create -u "+tgt)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := zelta(t, env, append(append([]string{"backup"}, c.flags...), c.src, c.tgt)...)
			expectOK(t, r)
			ds := c.tgt[strings.Index(c.tgt, ":")+1:]
			if snaps := snapshots(t, ds+"/with space"); len(snaps) != 1 {
				t.Errorf("replica snapshots = %q\n%s\n%s", snaps, r.Stdout, r.Stderr)
			}
			m := zelta(t, env, "match", c.src, c.tgt)
			if !strings.Contains(m.Stdout, "2 up-to-date") {
				t.Errorf("match:\n%s\n%s", m.Stdout, m.Stderr)
			}
		})
	}
}

func TestPolicy(t *testing.T) {
	src, tgt := tree(t, "policy")
	sh(t, "zfs create -u "+src+"/app1 && zfs create -u "+src+"/app2 && zfs create -u "+tgt)
	conf := filepath.Join(workDir, "policy.conf")
	os.WriteFile(conf, []byte("BACKUP_ROOT: "+tgt+"\nSNAP_MODE: ALWAYS\nSITE1:\n  localhost:\n  - "+src+"/app1\n  - "+src+"/app2\n"), 0o644)
	r := zelta(t, nil, "policy", "-n", "-C", conf)
	expectOK(t, r)
	expectLines(t, r.Stdout, "SOURCE TARGET", "localhost:"+src+"/app1 "+tgt+"/app1", "localhost:"+src+"/app2 "+tgt+"/app2")
	r = zelta(t, nil, "policy", "-C", conf, "app2")
	expectOK(t, r)
	if exists(tgt+"/app1") || !exists(tgt+"/app2") {
		t.Errorf("policy operand filter:\n%s\n%s", r.Stdout, r.Stderr)
	}
	expectOK(t, zelta(t, nil, "policy", "-C", conf))
	if !exists(tgt + "/app1") {
		t.Errorf("policy run did not back up app1")
	}
}

func TestLockUnlockFailover(t *testing.T) {
	src, tgt := tree(t, "failover")
	sh(t, "zfs set mountpoint="+filepath.Join(mountDir, "fo-src")+" "+src+" && zfs set compression=lz4 "+src)
	expectOK(t, zelta(t, nil, "backup", src, tgt))
	r := zelta(t, nil, "failover", src, tgt)
	expectOK(t, r)
	prop := func(ds, p string) string { return strings.TrimSpace(sh(t, "zfs get -H -o value "+p+" "+ds)) }
	if prop(src, "readonly") != "on" || prop(src, "mounted") != "no" {
		t.Errorf("source not locked: readonly=%s mounted=%s", prop(src, "readonly"), prop(src, "mounted"))
	}
	if prop(tgt, "readonly") != "off" || prop(tgt, "compression") != "lz4" {
		t.Errorf("target not promoted: readonly=%s compression=%s", prop(tgt, "readonly"), prop(tgt, "compression"))
	}
	expectOK(t, zelta(t, nil, "unlock", src))
	if prop(src, "readonly") != "off" {
		t.Errorf("unlock did not clear readonly")
	}
}

func TestCompletionAgainstRealPools(t *testing.T) {
	src, _ := tree(t, "complete")
	sh(t, "zfs create -u "+src+"/alpha && zfs snapshot "+src+"/alpha@x")
	r := zelta(t, nil, "__complete", "backup", src+"/al")
	if !strings.Contains(r.Stdout, src+"/alpha\n") {
		t.Errorf("dataset completion:\n%s", r.Stdout)
	}
	r = zelta(t, nil, "__complete", "backup", src+"/alpha@")
	if !strings.Contains(r.Stdout, src+"/alpha@x\n") {
		t.Errorf("snapshot completion:\n%s", r.Stdout)
	}
}
