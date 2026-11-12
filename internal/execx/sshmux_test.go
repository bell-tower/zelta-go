package execx

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bell-tower/zelta-go/internal/command"
)

type startLog struct {
	mu    sync.Mutex
	calls [][]string
	err   error
}

func (s *startLog) start(_ context.Context, argv []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, argv)
	return s.err
}

func remoteCmd(prefix, host string, words ...string) command.Cmd {
	return command.New(command.Words(words...)...).On(command.NewRemote(prefix, host))
}

func TestSSHMuxRewritesPlainSSH(t *testing.T) {
	fake := NewFake().OnPrefix("", Out("ok"))
	starts := &startLog{}
	m := &SSHMux{Inner: fake, Start: starts.start}
	ctx := context.Background()

	m.Run(ctx, remoteCmd("ssh -n", "me@h", "zfs", "list"))
	m.Run(ctx, remoteCmd("ssh", "me@h", "zfs", "recv"))               // same connection, different session flags
	m.Run(ctx, remoteCmd("/usr/bin/ssh -p 22", "me@h", "zfs", "get")) // different connection options
	m.Pipe(ctx, command.Pipeline{remoteCmd("ssh -n", "other", "zfs", "send"), command.New(command.Word("cat"))}, nil)

	if len(starts.calls) != 3 {
		t.Fatalf("masters started = %d: %v", len(starts.calls), starts.calls)
	}
	master := strings.Join(starts.calls[0], " ")
	if !strings.HasPrefix(master, "ssh -o ControlPath=") || !strings.HasSuffix(master, "-o ControlMaster=yes -o ControlPersist=30 -N -f me@h") {
		t.Errorf("master argv = %q", master)
	}
	path := strings.TrimPrefix(starts.calls[0][2], "ControlPath=")
	for i, want := range []string{
		"ssh -o ControlPath=" + path + " -o ControlMaster=no -n me@h zfs list",
		"ssh -o ControlPath=" + path + " -o ControlMaster=no me@h zfs recv",
	} {
		if fake.Calls[i] != want {
			t.Errorf("call %d = %q want %q", i, fake.Calls[i], want)
		}
	}
	if !strings.Contains(fake.Calls[2], "/usr/bin/ssh -o ControlPath=") || strings.Contains(fake.Calls[2], path) {
		t.Errorf("distinct connection options need a distinct master: %q", fake.Calls[2])
	}
	if !strings.HasPrefix(fake.Calls[3], "{ ssh -o ControlPath=") || !strings.HasSuffix(fake.Calls[3], "|cat ; }") {
		t.Errorf("pipeline = %q", fake.Calls[3])
	}

	dir := strings.TrimSuffix(path, "/"+path[strings.LastIndex(path, "/")+1:])
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("socket dir: %v", err)
	}
	starts.calls = nil
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if len(starts.calls) != 3 || !strings.HasSuffix(strings.Join(starts.calls[0], " "), "-O exit me@h") && !strings.HasSuffix(strings.Join(starts.calls[0], " "), "-O exit other") {
		t.Errorf("close calls = %v", starts.calls)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("socket dir not removed")
	}
	if m.Close() != nil {
		t.Errorf("second close")
	}
}

func TestSSHMuxLeavesOtherCommandsAlone(t *testing.T) {
	fake := NewFake().OnPrefix("", Out())
	starts := &startLog{}
	m := &SSHMux{Inner: fake, Start: starts.start}
	ctx := context.Background()
	cmds := []command.Cmd{
		command.New(command.Words("zfs", "list")...),
		remoteCmd("rsh", "h", "zfs", "list"),
		remoteCmd("ssh -o ControlPath=/mine", "h", "zfs", "list"),
		remoteCmd("ssh -oControlMaster=auto", "h", "zfs", "list"),
		remoteCmd("ssh -S /sock", "h", "zfs", "list"),
		remoteCmd("ssh -S/sock", "h", "zfs", "list"),
		remoteCmd("ssh -o controlpersist=1m", "h", "zfs", "list"),
	}
	for _, c := range cmds {
		m.Run(ctx, c)
	}
	for i, c := range cmds {
		if fake.Calls[i] != c.String() {
			t.Errorf("command %d rewritten: %q", i, fake.Calls[i])
		}
	}
	if len(starts.calls) != 0 {
		t.Errorf("unexpected masters: %v", starts.calls)
	}
	m.Close()
}

func TestSSHMuxFallsBackWhenMasterFails(t *testing.T) {
	fake := NewFake().OnPrefix("", Out())
	starts := &startLog{err: errors.New("auth failed")}
	m := &SSHMux{Inner: fake, Start: starts.start}
	c := remoteCmd("ssh -n", "h", "zfs", "list")
	m.Run(context.Background(), c)
	m.Run(context.Background(), c)
	if fake.Calls[0] != c.String() || fake.Calls[1] != c.String() || len(starts.calls) != 1 {
		t.Errorf("calls = %v starts = %d", fake.Calls, len(starts.calls))
	}
	starts.calls = nil
	m.Close()
	if len(starts.calls) != 0 {
		t.Errorf("a failed master must not be stopped: %v", starts.calls)
	}
}

func TestSSHMuxStartsOneMasterConcurrently(t *testing.T) {
	fake := NewFake().OnPrefix("", Out())
	starts := &startLog{}
	m := &SSHMux{Inner: fake, Start: starts.start}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			m.Run(context.Background(), remoteCmd("ssh -n", "h", "zfs", "list"))
		})
	}
	wg.Wait()
	if len(starts.calls) != 1 {
		t.Errorf("masters started = %d", len(starts.calls))
	}
	m.Close()
}

func TestSSHMuxDefaultStart(t *testing.T) {
	m := &SSHMux{}
	if err := m.start(context.Background(), []string{"true"}); err != nil {
		t.Errorf("start true: %v", err)
	}
	if err := m.start(context.Background(), []string{"false"}); err == nil {
		t.Errorf("start false succeeded")
	}
	if !controlled([]string{"ssh", "-o", "ControlPath=x"}) || controlled([]string{"ssh", "-o", "Port=2"}) || controlled([]string{"ssh", "-o"}) {
		t.Errorf("controlled")
	}
	if got := strings.Join(connection([]string{"ssh", "-n", "-T", "-p", "2"}), " "); got != "ssh -p 2" {
		t.Errorf("connection = %q", got)
	}
}

// TestSSHMuxSessionCap holds sessions open and checks that commands beyond
// MaxSessions connect directly instead of being refused by sshd.
func TestSSHMuxSessionCap(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	shared, direct := 0, 0
	fake := NewFake().OnFunc(func(string) bool { return true }, func(s string) Result {
		mu.Lock()
		if strings.Contains(s, "ControlPath=") {
			shared++
		} else {
			direct++
		}
		mu.Unlock()
		<-release
		return Out()
	})
	starts := &startLog{}
	m := &SSHMux{Inner: fake, Start: starts.start, MaxSessions: 2}
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			m.Pipe(context.Background(), command.Pipeline{remoteCmd("ssh", "h", "zfs", "recv")}, nil)
		})
	}
	for {
		mu.Lock()
		n := shared + direct
		mu.Unlock()
		if n == 5 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if shared != 2 || direct != 3 {
		t.Errorf("shared=%d direct=%d, want 2 and 3", shared, direct)
	}
	// Released sessions are reusable.
	m.Run(context.Background(), remoteCmd("ssh", "h", "true"))
	if !strings.Contains(fake.Calls[len(fake.Calls)-1], "ControlPath=") {
		t.Errorf("session not released: %q", fake.Calls[len(fake.Calls)-1])
	}
	m.Close()
}
