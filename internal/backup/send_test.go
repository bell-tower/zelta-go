package backup

import (
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/execx"
)

func encryptedFx(t *testing.T, opts map[string]string, tgtIVSet string) *fx {
	f := newFx(t, "backup", opts)
	f.props("apool/enc", ". type filesystem", ". encryption aes-256-gcm").props("bpool/enc", ". type filesystem", ". encryption aes-256-gcm").
		listOn("", "apool/enc", "name,guid,ivsetguid", "", "@s2 2 77", "@s1 1 66", ". 9 -").
		listOn("", "bpool/enc", "name,guid,ivsetguid", "", "@s1 1 "+tgtIVSet, ". 8 -")
	return f
}

func TestEncryptedRawAndDecryptedFallback(t *testing.T) {
	// The first backup of an encrypted dataset is raw.
	f := newFx(t, "backup", map[string]string{"DRYRUN": "1"})
	f.props("apool/enc", ". type filesystem", ". encryption aes-256-gcm").missing("bpool/enc").list("apool/enc", "@s1 1", ". 9")
	if err := f.backup("apool/enc", "bpool/enc"); err != nil {
		t.Fatal(err)
	}
	contains(t, "first", f.tr.Out.String(), "+ { zfs send -P --raw 'apool/enc@s1'|zfs recv -v "+fullRecvTop+" 'bpool/enc' ; }")

	// Matching IV sets keep raw incrementals.
	f = encryptedFx(t, map[string]string{"DRYRUN": "1"}, "66")
	if err := f.backup("apool/enc", "bpool/enc"); err != nil {
		t.Fatal(err)
	}
	f.noUnmatched()
	contains(t, "raw", f.tr.Out.String(), "zfs send -P --raw -I 'apool/enc@s1' 'apool/enc@s2'")
	notContains(t, "raw", f.tr.Err.String(), "raw incremental unavailable")

	// A broken IV set chain falls back to a decrypted send with a warning.
	f = encryptedFx(t, map[string]string{"DRYRUN": "1"}, "55")
	f.backup("apool/enc", "bpool/enc")
	contains(t, "fallback", f.tr.Out.String(), "zfs send -P -L -c -I 'apool/enc@s1' 'apool/enc@s2'")
	eqLines(t, "fallback stderr", f.tr.Err.String(), "warning: raw incremental unavailable at @s1; falling back to decrypted send: apool/enc")

	// An explicit override is used as is, without the warning.
	f = encryptedFx(t, map[string]string{"DRYRUN": "1", "SEND_OVERRIDE": "-L -c"}, "55")
	f.backup("apool/enc", "bpool/enc")
	contains(t, "override", f.tr.Out.String(), "zfs send -P -L -c -I 'apool/enc@s1'")
	eqLines(t, "override stderr", f.tr.Err.String())

	// Replicate mode never falls back.
	f = newFx(t, "replicate", map[string]string{"DRYRUN": "1", "VERB": "replicate"})
	f.propsOn("", "apool/enc", "0", ". type filesystem", ". encryption aes-256-gcm").propsOn("", "bpool/enc", "0", ". type filesystem").
		listOn("", "apool/enc", "name,guid,ivsetguid", "1", "@s2 2 77", "@s1 1 66", ". 9 -").
		listOn("", "bpool/enc", "name,guid,ivsetguid", "1", "@s1 1 55", ". 8 -")
	if err := f.backup("apool/enc", "bpool/enc"); err != nil {
		t.Fatal(err)
	}
	f.noUnmatched()
	contains(t, "replicate", f.tr.Out.String(), "zfs send -P --raw -s -R -I 'apool/enc@s1' 'apool/enc@s2'")
}

func TestSendCheck(t *testing.T) {
	f := incrementalOn(t, "", "", map[string]string{"SEND_CHECK": "1"})
	f.tr.Fake.Once("zfs send -nv "+incr, execx.Fail(2, "invalid option 'e'", "usage: zfs send ..."))
	f.on("zfs send -nv -L -c -I 'apool/src@a' 'apool/src@b'", execx.Out())
	f.tr.Fake.Once("{ zfs send -P -L -c -I 'apool/src@a' 'apool/src@b'|zfs recv -v -s 'bpool/tgt' ; }", execx.Fail(2, "invalid option -- c"))
	f.on("{ zfs send -P -L -I 'apool/src@a' 'apool/src@b'|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("%v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "stderr", f.tr.Err.String(),
		"warning: send-check dropping unsupported zfs send option: -e",
		"error: invalid option -- c: bpool/tgt",
		"warning: send-check dropping unsupported zfs send option: -c")
	eqLines(t, "stdout", f.tr.Out.String(), "syncing 1 datasets", "dataset up-to-date", "1K sent, 1 streams received in 0.5 seconds")

	// --raw defaults are normalized to their short options for checking.
	f = incrementalOn(t, "", "", map[string]string{"SEND_CHECK": "1", "SEND_DEFAULT": "--raw", "DRYRUN": "1"})
	f.backup("apool/src", "bpool/tgt")
	contains(t, "normalized", f.tr.Out.String(), "zfs send -P -L -c -e -I")
}

func TestSendFeatureHelpers(t *testing.T) {
	if !optionErrorMatchesFeature("cannot: unrecognized option '--embed'", "-e") == false {
		t.Errorf("long option names do not match short features")
	}
	for _, tt := range []struct {
		line, feature string
		want          bool
	}{
		{"invalid option 'e'", "-e", true},
		{"invalid option 'c'", "-e", false},
		{"invalid option ''", "-L", true},
		{"illegal option -- L", "-L", true},
		{"illegal option -- L", "-c", false},
		{"usage: zfs send", "-c", true},
	} {
		if got := optionErrorMatchesFeature(tt.line, tt.feature); got != tt.want {
			t.Errorf("optionErrorMatchesFeature(%q, %q) = %v", tt.line, tt.feature, got)
		}
	}
	flags := []string{"-Lce", "--compressed", "-p"}
	if !flagsHaveFeature(flags, "-e") || !flagsHaveFeature(flags, "-c") || flagsHaveFeature([]string{"-p"}, "-L") || !flagsHaveFeature([]string{"--large-block"}, "-L") || !flagsHaveFeature([]string{"--embedded-data"}, "-e") {
		t.Errorf("flagsHaveFeature")
	}
	if got := strings.Join(removeFeature([]string{"-Lce", "--compressed", "-c", "-p"}, "-c"), " "); got != "-Le -p" {
		t.Errorf("removeFeature = %q", got)
	}
	if featureLongOpt("-x", "--x") {
		t.Errorf("unknown feature")
	}
}

func TestResumeAndRecvOptions(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"LOG_LEVEL": "3"})
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem", ". receive_resume_token 1-abc-def").
		list("apool/src", "@b 2", "@a 1", ". 9").list("bpool/tgt", "@a 1", ". 8").
		on("{ zfs send -P -t 1-abc-def|zfs recv -v -s 'bpool/tgt' ; }", execx.Out("resume token contents:", "nvlist version: 0", "\tobject = 1", "size\t512", "receiving incremental stream of apool/src@b into bpool/tgt@b", "received 512B stream in 0.25 seconds (2K/sec)"))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatal(err)
	}
	f.noUnmatched()
	contains(t, "stdout", f.tr.Out.String(), "resuming transfer for: apool/src@b", "512B sent, 1 streams received in 0.25 seconds")
	contains(t, "stderr", f.tr.Err.String(), "to abort a failed resume, run: 'zfs receive -A bpool/tgt'", "512B incremental stream received in 0.25 seconds")

	f = newFx(t, "backup", map[string]string{"DRYRUN": "1", "RECV_DEFAULT": "-o compression=zstd", "RECV_PROPS_ADD": "a=b,c=d", "RECV_PROPS_DEL": "e", "RECV_VOL": "-o volmode=none", "RESUME": "0"})
	f.props("apool/src", ". type filesystem", "/vol type volume").missing("bpool/tgt").list("apool/src", "/vol@a 3", "/vol 4", "@a 1", ". 9")
	f.backup("apool/src", "bpool/tgt")
	f.noUnmatched()
	contains(t, "recv flags", f.tr.Out.String(),
		"zfs recv -v -o compression=zstd -o readonly=on -u -x mountpoint -o canmount=noauto -o a=b -o c=d -x e 'bpool/tgt' ; }",
		"zfs recv -v -o compression=zstd -o volmode=none -o a=b -o c=d -x e 'bpool/tgt/vol' ; }")

	f = incrementalOn(t, "", "", map[string]string{"DRYRUN": "1", "RECV_OVERRIDE": "-F -u", "SEND_OVERRIDE": "-L --raw"})
	f.backup("apool/src", "bpool/tgt")
	contains(t, "overrides", f.tr.Out.String(), "{ zfs send -P -L --raw -I 'apool/src@a' 'apool/src@b'|zfs recv -v -F -u 'bpool/tgt' ; }")
}

func TestSyncFailures(t *testing.T) {
	f := incrementalOn(t, "", "", nil)
	pipe := "{ zfs send -P " + incr + "|zfs recv -v -s 'bpool/tgt' ; }"
	f.on(pipe, execx.Fail(1, "cannot receive incremental stream: destination bpool/tgt has been modified", "cannot receive mountpoint property on bpool/tgt: permission denied", "12:00:01   1024   apool/src@b"))
	err := f.backup("apool/src", "bpool/tgt")
	if exitCode(err) != 2 {
		t.Fatalf("code = %d", exitCode(err))
	}
	eqLines(t, "stderr", f.tr.Err.String(),
		"error: cannot receive incremental stream: destination bpool/tgt has been modified: bpool/tgt",
		"warning: missing `zfs allow` permissions: mountpoint")
	notContains(t, "stdout", f.tr.Out.String(), "up-to-date")

	f = incrementalOn(t, "", "", nil)
	f.on(pipe, execx.Fail(1))
	if exitCode(f.backup("apool/src", "bpool/tgt")) != 2 {
		t.Errorf("silent failure must exit 2")
	}
	contains(t, "stderr", f.tr.Err.String(), "error: sync command failed before receiving stream: apool/src@b -> bpool/tgt")

	f = incrementalOn(t, "", "", nil)
	f.on(pipe, execx.Out("ssh: connect to host x: Connection refused"))
	if exitCode(f.backup("apool/src", "bpool/tgt")) != 2 {
		t.Errorf("connection failure must exit 2")
	}
	contains(t, "stderr", f.tr.Err.String(), "error: ssh: connect to host x: Connection refused")

	f = incrementalOn(t, "", "", nil)
	f.on(pipe, execx.Out("full\tapool/src@b\t100", "something odd"))
	if exitCode(f.backup("apool/src", "bpool/tgt")) != 2 {
		t.Errorf("no stream must exit 2")
	}
	contains(t, "stderr", f.tr.Err.String(), "error: sync failed before receiving stream: apool/src@b -> bpool/tgt")

	f = incrementalOn(t, "", "", nil)
	f.on(pipe, execx.Out("usage:", "\tsend [-DLPbcehnpsVvw] ..."))
	if exitCode(f.backup("apool/src", "bpool/tgt")) != 2 {
		t.Errorf("usage must exit 2")
	}
	contains(t, "stderr", f.tr.Err.String(), "error: usage: bpool/tgt")
}
