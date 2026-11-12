//go:build integration

package integration

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startSSHD runs a throwaway sshd on a free loopback port with its own host
// and client keys, and returns an ssh command that reaches it as host
// "zeltatest", plus the sshd log path. The daemon is stopped at cleanup.
func startSSHD(t *testing.T) (sshCmd, logFile string) {
	t.Helper()
	sshd := "/usr/sbin/sshd"
	if _, err := os.Stat(sshd); err != nil {
		t.Skip("sshd not installed")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0o700)
	for _, k := range []string{"host_key", "client_key"} {
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", filepath.Join(dir, k)).CombinedOutput(); err != nil {
			t.Fatalf("ssh-keygen: %v %s", err, out)
		}
	}
	pub, _ := os.ReadFile(filepath.Join(dir, "client_key.pub"))
	os.WriteFile(filepath.Join(dir, "authorized_keys"), pub, 0o600)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	logFile = filepath.Join(dir, "sshd.log")
	cfg := filepath.Join(dir, "sshd_config")
	os.WriteFile(cfg, fmt.Appendf(nil, "ListenAddress 127.0.0.1\nPort %d\nHostKey %s/host_key\nPidFile %s/sshd.pid\n"+
		"AuthorizedKeysFile %s/authorized_keys\nPermitRootLogin prohibit-password\nPasswordAuthentication no\n"+
		"KbdInteractiveAuthentication no\nUsePAM no\nStrictModes no\nMaxStartups 100\n", port, dir, dir, dir), 0o600)
	daemon := exec.Command(sshd, "-D", "-f", cfg, "-E", logFile)
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { daemon.Process.Kill(); daemon.Wait() })
	for i := 0; ; i++ {
		if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			c.Close()
			break
		}
		if i > 100 {
			t.Fatal("sshd did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	clientCfg := filepath.Join(dir, "ssh_config")
	os.WriteFile(clientCfg, fmt.Appendf(nil, "Host zeltatest\n  HostName 127.0.0.1\n  Port %d\n  User root\n  IdentityFile %s/client_key\n"+
		"  IdentitiesOnly yes\n  StrictHostKeyChecking no\n  UserKnownHostsFile %s/known_hosts\n  BatchMode yes\n  LogLevel ERROR\n", port, dir, dir), 0o600)
	return "ssh -F " + clientCfg, logFile
}

func countLog(file, s string) int {
	data, _ := os.ReadFile(file)
	return strings.Count(string(data), s)
}

// TestRealSSHConnectionReuse runs backups through a real OpenSSH server
// (opt-in: ZELTA_TEST_SSHD=1 starts a temporary loopback sshd).
func TestRealSSHConnectionReuse(t *testing.T) {
	if os.Getenv("ZELTA_TEST_SSHD") != "1" {
		t.Skip("set ZELTA_TEST_SSHD=1 to start a temporary loopback sshd")
	}
	ssh, log := startSSHD(t)
	src, tgt := tree(t, "realssh")
	sh(t, "zfs create -u "+tgt)
	for _, ds := range []string{"a", "b", "c", "with space", "a/x"} {
		sh(t, "zfs create -u '"+src+"/"+ds+"'")
	}
	sh(t, "zfs snapshot -r "+src+"@one")
	env := []string{"ZELTA_REMOTE_COMMAND=" + ssh}

	before := countLog(log, "Accepted publickey")
	expectOK(t, zelta(t, env, "backup", "--parallel", "4", src, "zeltatest:"+tgt+"/copy"))
	if n := countLog(log, "Accepted publickey") - before; n != 1 {
		t.Errorf("parallel backup opened %d ssh connections, want 1", n)
	}
	if got := snapshots(t, tgt+"/copy/with space"); len(got) != 1 {
		t.Errorf("replica snapshots = %v", got)
	}
	r := zelta(t, env, "match", src, "zeltatest:"+tgt+"/copy")
	if !strings.Contains(r.Stdout, "6 up-to-date") {
		t.Errorf("match:\n%s%s", r.Stdout, r.Stderr)
	}

	// The shared connection is closed when zelta exits.
	deadline := time.Now().Add(5 * time.Second)
	for countLog(log, "Disconnected from user") < countLog(log, "Accepted publickey") {
		if time.Now().After(deadline) {
			t.Fatalf("control connection still open after exit:\n%s", readFile(log))
		}
		time.Sleep(50 * time.Millisecond)
	}

	sh(t, "zfs snapshot -r "+src+"@two")
	before = countLog(log, "Accepted publickey")
	expectOK(t, zelta(t, append(env, "ZELTA_REMOTE_MULTIPLEX=0"), "backup", src, "zeltatest:"+tgt+"/copy"))
	if n := countLog(log, "Accepted publickey") - before; n < 6 {
		t.Errorf("without multiplexing each command connects; got %d connections", n)
	}
}

func readFile(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}
