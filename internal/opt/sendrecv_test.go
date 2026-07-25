package opt

import (
	"testing"
)

func TestDefault(t *testing.T) {
	d := Default()
	if d.SendDefault != "-L -c -e" {
		t.Fatalf("SendDefault=%q", d.SendDefault)
	}
	if d.RecvTop != "-o readonly=on" {
		t.Fatalf("RecvTop=%q", d.RecvTop)
	}
	if d.RecvFS != "-u -x mountpoint -o canmount=noauto" {
		t.Fatalf("RecvFS=%q", d.RecvFS)
	}
	if d.RecvPartial != "-s" || !d.Resume {
		t.Fatalf("partial=%q resume=%v", d.RecvPartial, d.Resume)
	}
}

func TestResolveEnv(t *testing.T) {
	t.Setenv("ZELTA_SEND_DEFAULT", "--raw")
	t.Setenv("ZELTA_RECV_TOP", "-o readonly=off")
	t.Setenv("ZELTA_RESUME", "0")
	r := Resolve()
	if r.SendDefault != "--raw" {
		t.Fatalf("SendDefault=%q", r.SendDefault)
	}
	if r.RecvTop != "-o readonly=off" {
		t.Fatalf("RecvTop=%q", r.RecvTop)
	}
	if r.Resume {
		t.Fatal("Resume should be false")
	}
	if r.SendFlags() != "--raw" {
		t.Fatalf("SendFlags=%q", r.SendFlags())
	}
}

func TestResolveReceiveProperties(t *testing.T) {
	t.Setenv("ZELTA_RECV_PROPS_ADD", "compression=lz4,quota=10G")
	t.Setenv("ZELTA_RECV_PROPS_DEL", "mountpoint,canmount")
	r := Resolve()
	if want := "compression=lz4"; len(r.RecvPropsAdd) != 2 || r.RecvPropsAdd[0] != want || r.RecvPropsAdd[1] != "quota=10G" {
		t.Fatalf("RecvPropsAdd=%v", r.RecvPropsAdd)
	}
	if want := "mountpoint"; len(r.RecvPropsDel) != 2 || r.RecvPropsDel[0] != want || r.RecvPropsDel[1] != "canmount" {
		t.Fatalf("RecvPropsDel=%v", r.RecvPropsDel)
	}
}

func TestSendOverride(t *testing.T) {
	t.Setenv("ZELTA_SEND_OVERRIDE", "-w")
	r := Resolve()
	if r.SendFlags() != "-w" {
		t.Fatalf("SendFlags=%q", r.SendFlags())
	}
}

func TestLookupBool(t *testing.T) {
	t.Setenv("ZELTA_FOO", "yes")
	if !LookupBool("FOO", false) {
		t.Fatal("yes → true")
	}
	t.Setenv("ZELTA_FOO", "no")
	if LookupBool("FOO", true) {
		t.Fatal("no → false")
	}
}
