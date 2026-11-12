package backup

import (
	"maps"
	"testing"
)

const incr = "-L -c -e -I 'apool/src@a' 'apool/src@b'"

// incrementalOn scripts an incremental sync between two endpoints reached
// through the given remote prefixes ("" for local).
func incrementalOn(t *testing.T, srcPrefix, tgtPrefix string, opts map[string]string) *fx {
	f := newFx(t, "backup", opts)
	f.propsOn(srcPrefix, "apool/src", "", ". type filesystem").
		propsOn(tgtPrefix, "bpool/tgt", "", ". type filesystem").
		listOn(srcPrefix, "apool/src", "name,guid", "", "@b 2", "@a 1", ". 9").
		listOn(tgtPrefix, "bpool/tgt", "name,guid", "", "@a 1", ". 8")
	return f
}

func TestTopologies(t *testing.T) {
	tests := []struct {
		name     string
		src, tgt string
		sp, tp   string
		opts     map[string]string
		want     string
		warning  string
	}{
		{"local", "apool/src", "bpool/tgt", "", "", nil,
			"+ { zfs send -P " + incr + "|zfs recv -v -s 'bpool/tgt' ; }", ""},
		{"pull", "me@s:apool/src", "me@t:bpool/tgt", "ssh -n me@s ", "ssh -n me@t ", nil,
			`+ ssh -n me@t "ssh -n me@s zfs send -P ` + incr + ` | zfs recv -v -s 'bpool/tgt'"`, ""},
		{"push", "me@s:apool/src", "me@t:bpool/tgt", "ssh -n me@s ", "ssh -n me@t ", map[string]string{"SYNC_DIRECTION": "push"},
			`+ ssh -n me@s "zfs send -P ` + incr + ` | ssh me@t zfs recv -v -s 'bpool/tgt'"`, ""},
		{"proxy", "me@s:apool/src", "me@t:bpool/tgt", "ssh -n me@s ", "ssh -n me@t ", map[string]string{"SYNC_DIRECTION": "0"},
			"+ { ssh -n me@s zfs send -P " + incr + "|ssh me@t zfs recv -v -s 'bpool/tgt' ; }",
			"warning: syncing remote endpoints through localhost; consider --push or --pull\n"},
		{"hairpin", "me@h:apool/src", "me@h:bpool/tgt", "ssh -n me@h ", "ssh -n me@h ", nil,
			`+ ssh -n me@h "{ zfs send -P ` + incr + `|zfs recv -v -s 'bpool/tgt' ; }"`, ""},
		{"local to remote", "apool/src", "me@t:bpool/tgt", "", "ssh -n me@t ", map[string]string{"RECV_PIPE": "pv -ptr |"},
			"+ { zfs send -P " + incr + "|pv -ptr 2>>/dev/tty | ssh me@t zfs recv -v -s 'bpool/tgt' ; }", ""},
		{"remote to local", "me@s:apool/src", "bpool/tgt", "ssh -n me@s ", "", map[string]string{"REMOTE_SEND": "ssh -n -c fast"},
			"+ { ssh -n -c fast me@s zfs send -P " + incr + "|zfs recv -v -s 'bpool/tgt' ; }", ""},
		{"recv pipe ignored for two remotes", "me@s:apool/src", "me@t:bpool/tgt", "ssh -n me@s ", "ssh -n me@t ", map[string]string{"RECV_PIPE": "pv"},
			`+ ssh -n me@t "ssh -n me@s zfs send -P ` + incr + ` | zfs recv -v -s 'bpool/tgt'"`, ""},
	}
	for _, tt := range tests {
		opts := map[string]string{"DRYRUN": "1"}
		maps.Copy(opts, tt.opts)
		f := incrementalOn(t, tt.sp, tt.tp, opts)
		if err := f.backup(tt.src, tt.tgt); err != nil {
			t.Fatalf("%s: %v\n%s", tt.name, err, f.tr.Err.String())
		}
		f.noUnmatched()
		eqLines(t, tt.name+" stdout", f.tr.Out.String(), "would sync 1 datasets", tt.want)
		eqLines(t, tt.name+" stderr", f.tr.Err.String()+"", trimNL(tt.warning)...)
	}
}

func trimNL(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s[:len(s)-1]}
}

func TestRealPipelineExecution(t *testing.T) {
	f := incrementalOn(t, "ssh -n me@s ", "ssh -n me@t ", nil)
	f.on(`ssh -n me@t "ssh -n me@s zfs send -P `+incr+` | zfs recv -v -s 'bpool/tgt'"`, received("incremental", 4096, 2))
	if err := f.backup("me@s:apool/src", "me@t:bpool/tgt"); err != nil {
		t.Fatal(err)
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(), "syncing 1 datasets", "dataset up-to-date", "4K sent, 2 streams received in 1 seconds")
}
