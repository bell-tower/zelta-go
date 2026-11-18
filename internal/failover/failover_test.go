package failover

import (
	"context"
	"errors"
	"strings"
	"testing"

	"zelta.space/internal/app"
	"zelta.space/internal/execx"
)

var ctx = context.Background()

func lines(s string) string { return strings.Join(strings.Fields(s), " ") }

func checkOut(t *testing.T, tr *app.TestRuntime, want ...string) {
	t.Helper()
	got := strings.Split(strings.TrimSuffix(tr.Out.String(), "\n"), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("stdout:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func code(err error) int { return app.Code(err) }

func TestLockDryRun(t *testing.T) {
	tr := app.NewTest("lock", map[string]string{"DRYRUN": "1"})
	if err := Lock(ctx, tr.Runtime, []string{"pool/svc"}); err != nil {
		t.Fatal(err)
	}
	checkOut(t, tr,
		"+ zfs set readonly=on 'pool/svc'",
		"+ zfs list -Hpr -o name -t filesystem -Screatetxg 'pool/svc' | xargs -n1 zfs set canmount=noauto",
		`+ zfs list -Hpr -o name,mounted -t filesystem -Screatetxg 'pool/svc' | while read ds mounted; do [ "$mounted" = yes ] && zfs unmount "$ds"; done`)
	if len(tr.Fake.Calls) != 0 {
		t.Errorf("dry run executed %v", tr.Fake.Calls)
	}

	tr = app.NewTest("lock", map[string]string{"DRYRUN": "1", "FORCE_UNMOUNT": "1"})
	Lock(ctx, tr.Runtime, []string{"user@h:pool/svc"})
	checkOut(t, tr,
		`+ ssh -n user@h "zfs set readonly=on 'pool/svc'"`,
		`+ ssh -n user@h "zfs list -Hpr -o name -t filesystem -Screatetxg 'pool/svc' | xargs -n1 zfs set canmount=noauto"`,
		`+ ssh -n user@h "zfs list -Hpr -o name,mounted -t filesystem -Screatetxg 'pool/svc' | while read ds mounted; do [ "$mounted" = yes ] && zfs unmount -f "$ds"; done"`)

	tr = app.NewTest("lock", map[string]string{"DRYRUN": "1", "NO_UNMOUNT": "1"})
	Lock(ctx, tr.Runtime, []string{"pool/svc"})
	checkOut(t, tr,
		"+ zfs set readonly=on 'pool/svc'",
		"+ zfs list -Hpr -o name -t filesystem -Screatetxg 'pool/svc' | xargs -n1 zfs set canmount=noauto")
}

func TestUnlockDryRun(t *testing.T) {
	tr := app.NewTest("unlock", map[string]string{"DRYRUN": "1"})
	if err := Unlock(ctx, tr.Runtime, []string{"pool/svc", "h:pool/x"}); err != nil {
		t.Fatal(err)
	}
	checkOut(t, tr,
		"+ zfs inherit readonly 'pool/svc'",
		"+ zfs list -Hpr -o name -t filesystem -s createtxg 'pool/svc' | xargs -n1 zfs set canmount=on",
		"+ zfs mount -R 'pool/svc'",
		`+ zfs list -Hpr -o name,canmount,mounted -t filesystem -s name 'pool/svc' | while read ds canmount mounted; do [ "$canmount" != off ] && [ "$mounted" != yes ] && zfs mount "$ds"; done`,
		`+ ssh -n h "zfs inherit readonly 'pool/x'"`,
		`+ ssh -n h "zfs list -Hpr -o name -t filesystem -s createtxg 'pool/x' | xargs -n1 zfs set canmount=on"`,
		`+ ssh -n h "zfs mount -R 'pool/x'"`,
		`+ ssh -n h "zfs list -Hpr -o name,canmount,mounted -t filesystem -s name 'pool/x' | while read ds canmount mounted; do [ "$canmount" != off ] && [ "$mounted" != yes ] && zfs mount "$ds"; done"`)
}

func TestFailoverDryRun(t *testing.T) {
	tr := app.NewTest("failover", map[string]string{"DRYRUN": "1"})
	if err := Failover(ctx, tr.Runtime, []string{"a:pool/svc", "b:pool/svc"}); err != nil {
		t.Fatal(err)
	}
	checkOut(t, tr,
		`+ ssh -n a "zfs set readonly=on 'pool/svc'"`,
		`+ ssh -n a "zfs list -Hpr -o name -t filesystem -Screatetxg 'pool/svc' | xargs -n1 zfs set canmount=noauto"`,
		`+ ssh -n a "zfs list -Hpr -o name,mounted -t filesystem -Screatetxg 'pool/svc' | while read ds mounted; do [ "$mounted" = yes ] && zfs unmount "$ds"; done"`,
		"+ zelta ipc-run backup --snapshot --log-mode=text --log-level=2 'a:pool/svc' 'b:pool/svc'",
		`+ ssh -n b "zfs inherit readonly 'pool/svc'"`,
		`+ ssh -n b "zfs list -Hpr -o name -t filesystem -s createtxg 'pool/svc' | xargs -n1 zfs set canmount=on"`,
		`+ ssh -n b "zfs mount -R 'pool/svc'"`,
		`+ ssh -n b "zfs list -Hpr -o name,canmount,mounted -t filesystem -s name 'pool/svc' | while read ds canmount mounted; do [ "$canmount" != off ] && [ "$mounted" != yes ] && zfs mount "$ds"; done"`)
	if len(tr.Fake.Calls) != 0 {
		t.Errorf("dry run executed %v", tr.Fake.Calls)
	}
}

func TestOperandErrors(t *testing.T) {
	for _, tc := range []struct {
		verb string
		fn   func(context.Context, *app.Runtime, []string) error
		args []string
		msg  string
	}{
		{"lock", Lock, nil, "zelta lock requires at least one dataset"},
		{"unlock", Unlock, nil, "zelta unlock requires at least one dataset"},
		{"failover", Failover, []string{"x"}, "zelta failover requires SOURCE and TARGET"},
		{"propsync", Propsync, []string{"a", "b", "c"}, "zelta propsync requires SOURCE and TARGET"},
	} {
		tr := app.NewTest(tc.verb, nil)
		err := tc.fn(ctx, tr.Runtime, tc.args)
		if code(err) != 1 || err.Error() != tc.msg {
			t.Errorf("%s: %v", tc.verb, err)
		}
	}
}

const (
	lockNames  = "zfs list -Hpr -o name -t filesystem -Screatetxg 'pool/svc'"
	lockMounts = "zfs list -Hpr -o name,mounted -t filesystem -Screatetxg 'pool/svc'"
)

func TestLockRun(t *testing.T) {
	tr := app.NewTest("lock", map[string]string{"LOG_LEVEL": "3", "FORCE_UNMOUNT": "1"})
	tr.Fake.On("zfs set readonly=on 'pool/svc'", execx.Out())
	tr.Fake.On(lockNames, execx.Out("pool/svc/child", "pool/svc").WithStderr("note from zfs"))
	tr.Fake.On("zfs set canmount=noauto 'pool/svc/child'", execx.Out())
	tr.Fake.On("zfs set canmount=noauto 'pool/svc'", execx.Out("set output"))
	tr.Fake.On(lockMounts, execx.Out("pool/svc/child\tyes", "pool/svc\tno", "pool/svc/busy\tyes"))
	tr.Fake.On("zfs unmount -f 'pool/svc/child'", execx.Out())
	tr.Fake.On("zfs unmount -f 'pool/svc/busy'", execx.Fail(1, "cannot unmount: busy"))
	err := Lock(ctx, tr.Runtime, []string{"pool/svc"})
	if code(err) != 1 {
		t.Fatalf("err = %v", err)
	}
	wantCalls := "zfs set readonly=on 'pool/svc'|" + lockNames + "|zfs set canmount=noauto 'pool/svc/child'|zfs set canmount=noauto 'pool/svc'|" +
		lockMounts + "|zfs unmount -f 'pool/svc/child'|zfs unmount -f 'pool/svc/busy'"
	if got := strings.Join(tr.Fake.Calls, "|"); got != wantCalls {
		t.Errorf("calls:\n%s\nwant\n%s", got, wantCalls)
	}
	if !strings.Contains(tr.Err.String(), "error: failed to unmount locked dataset: zfs unmount -f 'pool/svc/busy'") ||
		!strings.Contains(tr.Err.String(), "+ zfs set readonly=on 'pool/svc'") {
		t.Errorf("stderr = %q", tr.Err.String())
	}
	if !strings.Contains(tr.RawErr.String(), "note from zfs") || !strings.Contains(tr.RawErr.String(), "cannot unmount: busy") || tr.RawOut.String() != "set output\n" {
		t.Errorf("forwarded output: out=%q err=%q", tr.RawOut.String(), tr.RawErr.String())
	}

	tr = app.NewTest("lock", map[string]string{"NO_UNMOUNT": "1"})
	tr.Fake.On("zfs set readonly=on 'pool/svc'", execx.Out())
	tr.Fake.On(lockNames, execx.Out())
	if err := Lock(ctx, tr.Runtime, []string{"pool/svc"}); err != nil || len(tr.Fake.Calls) != 2 {
		t.Errorf("no-unmount: %v %v", err, tr.Fake.Calls)
	}
}

func TestLockFailures(t *testing.T) {
	tr := app.NewTest("lock", nil)
	tr.Fake.On("ssh -n h \"zfs set readonly=on 'pool/svc'\"", execx.Fail(1))
	err := Lock(ctx, tr.Runtime, []string{"h:pool/svc"})
	if code(err) != 255 || err.Error() != `failed to set readonly; dataset is not safely locked: ssh -n h "zfs set readonly=on 'pool/svc'"` {
		t.Errorf("readonly failure: %v", err)
	}

	tr = app.NewTest("lock", nil)
	tr.Fake.On("zfs set readonly=on 'pool/svc'", execx.Out())
	tr.Fake.On(lockNames, execx.Out("pool/svc"))
	tr.Fake.On("zfs set canmount=noauto 'pool/svc'", execx.Fail(1))
	err = Lock(ctx, tr.Runtime, []string{"pool/svc"})
	if code(err) != 1 || err.Error() != "command failed: zfs set canmount=noauto 'pool/svc'" {
		t.Errorf("canmount failure: %v", err)
	}
}

const (
	unlockNames  = "zfs list -Hpr -o name -t filesystem -s createtxg 'pool/svc'"
	unlockMounts = "zfs list -Hpr -o name,canmount,mounted -t filesystem -s name 'pool/svc'"
)

func TestUnlockRun(t *testing.T) {
	// unlock runs "zelta unlock pool/svc" with the given "zfs mount -R" result.
	unlock := func(mountR execx.Result) (*app.TestRuntime, error) {
		tr := app.NewTest("unlock", nil)
		tr.Fake.On("zfs inherit readonly 'pool/svc'", execx.Out())
		tr.Fake.On(unlockNames, execx.Out("pool/svc", "pool/svc/a"))
		tr.Fake.On("zfs set canmount=on 'pool/svc'", execx.Out())
		tr.Fake.On("zfs set canmount=on 'pool/svc/a'", execx.Out())
		tr.Fake.On("zfs mount -R 'pool/svc'", mountR)
		tr.Fake.On(unlockMounts, execx.Out("pool/svc\ton\tno", "pool/svc/a\toff\tno", "pool/svc/b\tnoauto\tyes", "pool/svc/c\tnoauto\tno", "short\trow"))
		tr.Fake.On("zfs mount 'pool/svc'", execx.Out())
		tr.Fake.On("zfs mount 'pool/svc/c'", execx.Out())
		return tr, Unlock(ctx, tr.Runtime, []string{"pool/svc"})
	}

	// OpenZFS 2.2 has no -R; mounting each dataset is not an error.
	tr, err := unlock(execx.Fail(2, "invalid option 'R'", "usage:"))
	if err != nil {
		t.Fatalf("without mount -R: %v", err)
	}
	if got := len(tr.Fake.CallsMatching("zfs mount '")); got != 2 {
		t.Errorf("mount calls = %v", tr.Fake.Calls)
	}
	if tr.Err.String() != "" || tr.RawErr.String() != "" {
		t.Errorf("without mount -R: stderr = %q, zfs output = %q", tr.Err.String(), tr.RawErr.String())
	}

	tr, err = unlock(execx.Fail(1, "cannot mount 'pool/svc': mountpoint busy"))
	if code(err) != 1 {
		t.Fatalf("err = %v", err)
	}
	if got := len(tr.Fake.CallsMatching("zfs mount '")); got != 2 {
		t.Errorf("mount calls = %v", tr.Fake.Calls)
	}
	if !strings.Contains(tr.Err.String(), "error: failed recursive mount during unlock: zfs mount -R 'pool/svc'") ||
		!strings.Contains(tr.RawErr.String(), "mountpoint busy") {
		t.Errorf("stderr = %q, zfs output = %q", tr.Err.String(), tr.RawErr.String())
	}

	tr = app.NewTest("unlock", nil)
	tr.Fake.On("zfs inherit readonly 'pool/svc'", execx.Fail(1))
	if err := Unlock(ctx, tr.Runtime, []string{"pool/svc"}); code(err) != 255 || !strings.HasPrefix(err.Error(), "failed to clear readonly; dataset is not safely unlocked: ") {
		t.Errorf("inherit failure: %v", err)
	}

	tr = app.NewTest("unlock", nil)
	tr.Fake.On("zfs inherit readonly 'pool/svc'", execx.Out())
	tr.Fake.On(unlockNames, execx.Out("pool/svc"))
	tr.Fake.On("zfs set canmount=on 'pool/svc'", execx.Fail(1))
	if err := Unlock(ctx, tr.Runtime, []string{"pool/svc"}); code(err) != 1 || !strings.HasPrefix(err.Error(), "command failed: ") {
		t.Errorf("canmount failure: %v", err)
	}
}

const (
	srcProps = "zfs get -Hpr -s local -t filesystem,volume -o name,property,value all 'src/svc'"
	tgtProps = "ssh -n b \"zfs get -Hpr -s local -t filesystem,volume -o name,property,value all 'tgt/svc'\""
)

func propsFakes(tr *app.TestRuntime) {
	tr.Fake.On(srcProps, execx.Out(
		"src/svc\tcompression\tlz4",
		"src/svc\treadonly\ton",
		"src/svc\tcanmount\tnoauto",
		"src/svc/vol\tvolsize\t1G",
		"src/svc/a\tcom.x:note\tsay \"hi\" $HOME",
		"src/svc/a\treadonly\ton",
		"broken line",
	))
	tr.Fake.On(tgtProps, execx.Out(
		"tgt/svc\tcompression\toff",
		"tgt/svc\tatime\toff",
		"tgt/svc/a\tquota\t1G",
	))
}

func TestPropsync(t *testing.T) {
	tr := app.NewTest("propsync", map[string]string{"LOG_LEVEL": "3"})
	propsFakes(tr)
	tr.Fake.OnPrefix("ssh -n b \"zfs set", execx.Out())
	tr.Fake.OnPrefix("ssh -n b \"zfs inherit", execx.Out())
	if err := Propsync(ctx, tr.Runtime, []string{"src/svc", "b:tgt/svc"}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		srcProps, tgtProps,
		`ssh -n b "zfs set compression='lz4' 'tgt/svc'"`,
		`ssh -n b "zfs set com.x:note='say \"hi\" \$HOME' 'tgt/svc/a'"`,
		`ssh -n b "zfs set readonly='on' 'tgt/svc/a'"`,
		`ssh -n b "zfs inherit atime 'tgt/svc'"`,
		`ssh -n b "zfs inherit quota 'tgt/svc/a'"`,
	}
	if got := strings.Join(tr.Fake.Calls, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}

	tr = app.NewTest("propsync", map[string]string{"DRYRUN": "1"})
	propsFakes(tr)
	if err := Propsync(ctx, tr.Runtime, []string{"src/svc", "b:tgt/svc"}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(tr.Out.String(), "+ ssh -n b"); n != 5 || len(tr.Fake.Calls) != 2 {
		t.Errorf("dry-run propsync out=%q calls=%v", tr.Out.String(), tr.Fake.Calls)
	}

	tr = app.NewTest("propsync", nil)
	tr.Fake.On(srcProps, execx.Out("other/x\tatime\toff"))
	if err := Propsync(ctx, tr.Runtime, []string{"src/svc", "b:tgt/svc"}); code(err) != 1 || err.Error() != "dataset is outside expected root: other/x" {
		t.Errorf("outside root: %v", err)
	}

	tr = app.NewTest("propsync", nil)
	tr.Fake.On(srcProps, execx.Out())
	tr.Fake.On(tgtProps, execx.Out("tgt/svc/z\tatime\toff", "elsewhere\tatime\toff"))
	if err := Propsync(ctx, tr.Runtime, []string{"src/svc", "b:tgt/svc"}); code(err) != 1 {
		t.Errorf("target outside root: %v", err)
	}

	tr = app.NewTest("propsync", nil)
	propsFakes(tr)
	tr.Fake.OnPrefix("ssh -n b \"zfs set", execx.Fail(1))
	if err := Propsync(ctx, tr.Runtime, []string{"src/svc", "b:tgt/svc"}); code(err) != 1 || !strings.HasPrefix(err.Error(), "command failed: ssh -n b \"zfs set compression") {
		t.Errorf("set failure: %v", err)
	}

	tr = app.NewTest("propsync", nil)
	propsFakes(tr)
	tr.Fake.OnPrefix("ssh -n b \"zfs set", execx.Out())
	tr.Fake.OnPrefix("ssh -n b \"zfs inherit", execx.Fail(1))
	if err := Propsync(ctx, tr.Runtime, []string{"src/svc", "b:tgt/svc"}); code(err) != 1 || !strings.Contains(err.Error(), "zfs inherit atime") {
		t.Errorf("inherit failure: %v", err)
	}
}

// failoverFakes scripts a full local failover from src/svc to tgt/svc.
func failoverFakes(tr *app.TestRuntime) {
	tr.Fake.On("zfs get -Hpr -s local -t filesystem,volume -o name,property,value all 'src/svc'", execx.Out("src/svc\tatime\toff"))
	tr.Fake.On("zfs get -Hpr -s local -t filesystem,volume -o name,property,value all 'tgt/svc'", execx.Out())
	tr.Fake.OnPrefix("zfs set", execx.Out())
	tr.Fake.OnPrefix("zfs inherit", execx.Out())
	tr.Fake.OnPrefix("zfs list", execx.Out())
	tr.Fake.OnPrefix("zfs mount", execx.Out())
}

func TestFailoverRun(t *testing.T) {
	defer func(b func(context.Context, *app.Runtime, []string) error) { Backup = b }(Backup)
	var gotArgs []string
	var gotRT *app.Runtime
	Backup = func(_ context.Context, rt *app.Runtime, args []string) error {
		gotArgs, gotRT = args, rt
		rt.Log.Noticef("backup ran")
		return nil
	}
	tr := app.NewTest("failover", map[string]string{"LOG_LEVEL": "3", "LOG_PREFIX": "[x] "})
	failoverFakes(tr)
	if err := Failover(ctx, tr.Runtime, []string{"src/svc", "tgt/svc"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(gotArgs, " ") != "src/svc tgt/svc" {
		t.Errorf("backup args = %v", gotArgs)
	}
	o := gotRT.Opts
	if o.Get("SNAP_MODE") != "ALWAYS" || o.Get("VERB") != "backup" || o.Get("LOG_MODE") != "text" || o.Get("LOG_LEVEL") != "3" {
		t.Errorf("backup options: %v", o.Environ())
	}
	if tr.Opts.Get("SNAP_MODE") == "ALWAYS" {
		t.Errorf("failover options must not be modified")
	}
	if tr.RawOut.String() != "backup ran\n" {
		t.Errorf("backup output = %q", tr.RawOut.String())
	}
	calls := strings.Join(tr.Fake.Calls, "\n")
	for _, want := range []string{"zfs set readonly=on 'src/svc'", "zfs set atime='off' 'tgt/svc'", "zfs inherit readonly 'tgt/svc'", "zfs mount -R 'tgt/svc'"} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing call %q in\n%s", want, calls)
		}
	}
	if !strings.Contains(tr.Err.String(), "+ zelta ipc-run backup --snapshot --log-mode=text --log-level=3 'src/svc' 'tgt/svc'") {
		t.Errorf("stderr = %q", tr.Err.String())
	}

	Backup = func(context.Context, *app.Runtime, []string) error { return app.Fail("boom") }
	tr = app.NewTest("failover", nil)
	failoverFakes(tr)
	err := Failover(ctx, tr.Runtime, []string{"src/svc", "tgt/svc"})
	if code(err) != 2 || !strings.HasPrefix(err.Error(), "final backup failed after source lock: zelta ipc-run backup") {
		t.Errorf("backup failure: %v", err)
	}
	if !strings.Contains(tr.RawErr.String(), "error: boom") {
		t.Errorf("backup error not logged: %q", tr.RawErr.String())
	}
	if len(tr.Fake.CallsMatching("inherit readonly")) != 0 {
		t.Errorf("target unlocked after failed backup")
	}

	Backup = nil
	tr = app.NewTest("failover", nil)
	failoverFakes(tr)
	if err := Failover(ctx, tr.Runtime, []string{"src/svc", "tgt/svc"}); code(err) != 2 {
		t.Errorf("nil backup: %v", err)
	}
}

func TestFailoverEarlyFailures(t *testing.T) {
	tr := app.NewTest("failover", nil)
	tr.Fake.On("zfs get -Hpr -s local -t filesystem,volume -o name,property,value all 'src/svc'", execx.Out("bad/root\tatime\toff"))
	if err := Failover(ctx, tr.Runtime, []string{"src/svc", "tgt/svc"}); code(err) != 1 {
		t.Errorf("source props: %v", err)
	}
	tr = app.NewTest("failover", nil)
	tr.Fake.On("zfs get -Hpr -s local -t filesystem,volume -o name,property,value all 'src/svc'", execx.Out())
	tr.Fake.On("zfs get -Hpr -s local -t filesystem,volume -o name,property,value all 'tgt/svc'", execx.Out("bad/root\tatime\toff"))
	if err := Failover(ctx, tr.Runtime, []string{"src/svc", "tgt/svc"}); code(err) != 1 {
		t.Errorf("target props: %v", err)
	}
	tr = app.NewTest("failover", nil)
	tr.Fake.Once("zfs set readonly=on 'src/svc'", execx.Fail(1))
	failoverFakes(tr)
	err := Failover(ctx, tr.Runtime, []string{"src/svc", "tgt/svc"})
	var ee *app.ExitError
	if !errors.As(err, &ee) || ee.Code != 255 {
		t.Errorf("lock failure: %v", err)
	}
	_ = lines
}
