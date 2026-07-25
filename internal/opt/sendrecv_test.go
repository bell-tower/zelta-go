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
