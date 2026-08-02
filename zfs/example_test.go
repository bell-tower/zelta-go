package zfs_test

import (
	"fmt"

	"github.com/bell-tower/zelta-go/zfs"
)

func ExampleSSHConfig_Argv() {
	cfg := zfs.SSHConfig{
		IdentityFile: "/var/db/sylve/ssh/target-1_id",
		Port:         22,
		Options:      []string{"StrictHostKeyChecking=accept-new"},
	}
	argv, err := cfg.Argv("root@backup.local", "zfs list -H tank/data", zfs.RoleDefault)
	if err != nil {
		panic(err)
	}
	// First tokens: ssh -n … -i key -p 22 …
	fmt.Printf("%s %s %s\n", argv[0], argv[1], argv[len(argv)-2])
	// Output:
	// ssh -n root@backup.local
}

func ExampleCommandRemote() {
	// Awk-style custom transport (mbuffer on the send path).
	r := zfs.CommandRemote{
		Command: "ssh -p 2202",
		Send:    "mbuffer -s 128k -m 1G | ssh -p 2202",
	}
	line, err := r.Shell("host", "zfs send tank@snap", zfs.RoleSend)
	if err != nil {
		panic(err)
	}
	fmt.Println(line != "")
	// Output:
	// true
}

func ExampleReal() {
	// Library caller (Sylve): structured SSH on Real.
	_ = &zfs.Real{
		SSH: zfs.SSHConfig{IdentityFile: "/path/to/key", Port: 22},
	}
	// Or raw REMOTE_* parity / non-ssh tools:
	_ = &zfs.Real{
		Remote: zfs.CommandRemote{Command: "ssh -p 2202"},
	}
}
