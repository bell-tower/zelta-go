// Package zfs is the Executor port for local and remote ZFS commands.
//
// Production:
//
//	exec := &zfs.Real{
//	    SSH: zfs.SSHConfig{IdentityFile: key, Port: "22"},
//	}
//
// Tests use zfs.Fake with canned list output. OpenSSH binary only — no
// golang.org/x/crypto/ssh client.
package zfs
