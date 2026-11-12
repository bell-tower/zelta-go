package zfs

import (
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/logging"
)

func TestCommand(t *testing.T) {
	rt := app.NewTest("backup", map[string]string{"REMOTE_SEND": "ssh -n -c fast"})
	local := Command(rt.Runtime, rt.Endpoint("pool/ds"), RemoteDefault, command.Word("list"), command.Name("pool/ds"))
	if got := local.String(); got != "zfs list 'pool/ds'" {
		t.Errorf("local = %q", got)
	}
	remote := Command(rt.Runtime, rt.Endpoint("me@host:pool/ds"), RemoteSend, command.Word("send"), command.Name("pool/ds@s"))
	if got := remote.String(); got != "ssh -n -c fast me@host zfs send 'pool/ds@s'" {
		t.Errorf("remote = %q", got)
	}
	recv := Command(rt.Runtime, rt.Endpoint("host:pool/ds"), RemoteRecv, command.Word("recv"))
	if got := recv.String(); got != "ssh host zfs recv" {
		t.Errorf("recv = %q", got)
	}
	if Local(command.Word("version")).String() != "zfs version" {
		t.Errorf("Local")
	}
}

func TestClassifyAndFeedback(t *testing.T) {
	tests := map[string]logging.Level{
		"": logging.Notice,
		"ssh: connect to host x port 22: No route to host":                     logging.Error,
		"ssh: Could not resolve hostname nope":                                 logging.Error,
		"sh: zfs: command not found":                                           logging.Error,
		"Connection refused":                                                   logging.Error,
		"Permission denied (publickey).":                                       logging.Error,
		"Host key verification failed.":                                        logging.Error,
		"Warning: Permanently added 'x' (ED25519) to the list of known hosts.": logging.Debug,
		"cannot open 'pool/x': dataset does not exist":                         logging.Info,
	}
	for line, want := range tests {
		if got := Classify(line); got != want {
			t.Errorf("Classify(%q) = %d want %d", line, got, want)
		}
	}
	rt := app.NewTest("match", map[string]string{"LOG_LEVEL": "4"})
	Feedback(rt.Log, "")
	Feedback(rt.Log, "Connection refused")
	Feedback(rt.Log, "odd output")
	if got := rt.Err.String(); got != "error: Connection refused\nodd output\n" {
		t.Errorf("feedback = %q", got)
	}
}

func TestHelpers(t *testing.T) {
	if f := Fields("a\t\tb"); len(f) != 3 || f[1] != "" {
		t.Errorf("Fields = %#v", f)
	}
	if !DoesNotExist("cannot open 'x': dataset does not exist") || DoesNotExist("ok") {
		t.Errorf("DoesNotExist")
	}
	tests := []struct {
		root, name, suffix string
		ok                 bool
	}{
		{"pool/a", "pool/a", "", true},
		{"pool/a", "pool/a/b", "/b", true},
		{"pool/a", "pool/a@s", "@s", true},
		{"pool/a", "pool/a#b", "#b", true},
		{"pool/a", "pool/ab", "", false},
		{"pool/a", "pool", "", false},
	}
	for _, tt := range tests {
		s, ok := Child(tt.root, tt.name)
		if s != tt.suffix || ok != tt.ok {
			t.Errorf("Child(%q,%q) = %q,%v", tt.root, tt.name, s, ok)
		}
	}
	_ = strings.TrimSpace
}
