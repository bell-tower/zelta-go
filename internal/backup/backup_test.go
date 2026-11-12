package backup

import (
	"testing"
)

const (
	fullRecvTop = "-o readonly=on -u -x mountpoint -o canmount=noauto -s"
	fullRecvFS  = "-u -x mountpoint -o canmount=noauto -s"
)

func TestFullBackupLocal(t *testing.T) {
	f := newFx(t, "backup", nil)
	f.props("apool/src", ". type filesystem", ". written 0").
		missing("bpool/tgt").
		list("apool/src", "@b 2", "@a 1", ". 9").
		on("{ zfs send -P -L -c -e 'apool/src@a'|zfs recv -v "+fullRecvTop+" 'bpool/tgt' ; }", received("full", 2048, 1)).
		on("{ zfs send -P -L -c -e -I 'apool/src@a' 'apool/src@b'|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(), "syncing 1 datasets", "3K sent, 2 streams received in 1 seconds")
	eqLines(t, "stderr", f.tr.Err.String())
}

func TestIncrementalUpToDateAndDryRun(t *testing.T) {
	f := newFx(t, "backup", nil)
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").
		list("apool/src", "@a 1", ". 9").list("bpool/tgt", "@a 1", ". 8")
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatal(err)
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(), "dataset up-to-date")
	eqSlice(t, "mutations", f.mutations())
}
