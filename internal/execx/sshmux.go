package execx

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bell-tower/zelta-go/internal/command"
)

// SSHMux runs commands through Inner, routing every command sent to the
// same host through plain ssh over one shared connection (OpenSSH
// connection multiplexing). A control master is started per host on first
// use and closed by Close. Only the executed argv changes; rendered
// commands, and therefore dry-run and debug output, are unaffected.
//
// Remote commands that do not use ssh, or that configure their own
// ControlPath or ControlMaster, are left alone. If a master cannot be
// started, commands for that host connect directly as before. At most
// MaxSessions commands share a master at once (sshd refuses sessions beyond
// its own MaxSessions, 10 by default); further commands connect directly.
type SSHMux struct {
	Inner Executor
	// Start runs a command that starts or stops a control master; nil
	// executes it with no standard streams attached.
	Start func(ctx context.Context, argv []string) error
	// MaxSessions caps concurrent sessions per shared connection; 0 means 8.
	MaxSessions int

	mu      sync.Mutex
	dir     string
	masters map[string]*sshMaster
}

type sshMaster struct {
	once   sync.Once
	ok     bool
	path   string
	host   string
	base   []string // connection options, for "-O exit"
	active int      // sessions in use, guarded by SSHMux.mu
}

// controlPersist bounds how long an orphaned master outlives Zelta.
const controlPersist = "30"

// Run implements Executor.
func (m *SSHMux) Run(ctx context.Context, c command.Cmd) Result {
	c, release := m.rewrite(ctx, c)
	defer release()
	return m.Inner.Run(ctx, c)
}

// Pipe implements Executor.
func (m *SSHMux) Pipe(ctx context.Context, p command.Pipeline, onLine func(Line)) Result {
	stages := make(command.Pipeline, len(p))
	for i, c := range p {
		var release func()
		stages[i], release = m.rewrite(ctx, c)
		defer release()
	}
	return m.Inner.Pipe(ctx, stages, onLine)
}

// rewrite routes c through a control master when it is a plain ssh command
// and the master has a free session. The returned func releases the session.
func (m *SSHMux) rewrite(ctx context.Context, c command.Cmd) (command.Cmd, func()) {
	noop := func() {}
	r := c.Remote
	if r == nil || len(r.Prefix) == 0 || filepath.Base(r.Prefix[0]) != "ssh" || controlled(r.Prefix) {
		return c, noop
	}
	master := m.master(ctx, r)
	if !master.ok || !m.acquire(master) {
		return c, noop
	}
	prefix := append([]string{r.Prefix[0], "-o", "ControlPath=" + master.path, "-o", "ControlMaster=no"}, r.Prefix[1:]...)
	c.Remote = &command.Remote{Prefix: prefix, Host: r.Host}
	return c, func() { m.release(master) }
}

func (m *SSHMux) acquire(mst *sshMaster) bool {
	limit := m.MaxSessions
	if limit <= 0 {
		limit = 8
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if mst.active >= limit {
		return false
	}
	mst.active++
	return true
}

func (m *SSHMux) release(mst *sshMaster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mst.active--
}

// controlled reports whether the ssh options already manage multiplexing.
func controlled(prefix []string) bool {
	for i, w := range prefix {
		opt := w
		if (w == "-o" || w == "-S") && i+1 < len(prefix) {
			if w == "-S" {
				return true
			}
			opt = prefix[i+1]
		} else if strings.HasPrefix(w, "-o") {
			opt = w[2:]
		} else if strings.HasPrefix(w, "-S") && w != "-S" {
			return true
		}
		key := strings.ToLower(strings.SplitN(opt, "=", 2)[0])
		if key == "controlpath" || key == "controlmaster" || key == "controlpersist" {
			return true
		}
	}
	return false
}

// connection returns the ssh options that identify a connection, dropping
// per-session flags such as -n and -T.
func connection(prefix []string) []string {
	var out []string
	for _, w := range prefix {
		switch w {
		case "-n", "-T", "-t", "-tt":
			continue
		}
		out = append(out, w)
	}
	return out
}

func (m *SSHMux) master(ctx context.Context, r *command.Remote) *sshMaster {
	conn := connection(r.Prefix)
	key := strings.Join(append(conn, r.Host), "\x00")
	m.mu.Lock()
	if m.masters == nil {
		m.masters = map[string]*sshMaster{}
	}
	mst := m.masters[key]
	if mst == nil {
		mst = &sshMaster{}
		m.masters[key] = mst
	}
	m.mu.Unlock()
	mst.once.Do(func() {
		dir, err := m.socketDir()
		if err != nil {
			return
		}
		h := fnv.New64a()
		h.Write([]byte(key))
		mst.path = filepath.Join(dir, fmt.Sprintf("%016x", h.Sum64()))
		mst.host = r.Host
		mst.base = append(append([]string{}, conn...), "-o", "ControlPath="+mst.path)
		argv := append(append([]string{}, mst.base...), "-o", "ControlMaster=yes", "-o", "ControlPersist="+controlPersist, "-N", "-f", r.Host)
		mst.ok = m.start(ctx, argv) == nil
	})
	return mst
}

// socketDir creates the directory for control sockets. Socket paths are
// limited to about 100 bytes, so a long TMPDIR falls back to /tmp.
func (m *SSHMux) socketDir() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dir != "" {
		return m.dir, nil
	}
	parent := os.TempDir()
	if len(parent) > 60 {
		parent = "/tmp"
	}
	dir, err := os.MkdirTemp(parent, "zelta-ssh-")
	if err != nil {
		return "", err
	}
	m.dir = dir
	return dir, nil
}

func (m *SSHMux) start(ctx context.Context, argv []string) error {
	if m.Start != nil {
		return m.Start(ctx, argv)
	}
	// No pipes: a backgrounded master would hold them open.
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run()
}

// Close stops the control masters and removes their sockets.
func (m *SSHMux) Close() error {
	m.mu.Lock()
	masters, dir := m.masters, m.dir
	m.masters, m.dir = nil, ""
	m.mu.Unlock()
	for _, mst := range masters {
		if mst.ok {
			argv := append(append([]string{}, mst.base...), "-O", "exit", mst.host)
			_ = m.start(context.Background(), argv)
		}
	}
	if dir != "" {
		return os.RemoveAll(dir)
	}
	return nil
}
