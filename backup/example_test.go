package backup_test

import (
	"context"
	"fmt"
	"time"

	"git.belltower.it/djbell/zelta-go/backup"
	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func ExampleRun_commands() {
	ctx := context.Background()
	f := &zfs.Fake{
		Lists: map[string]string{
			"pool/src": "pool/src\t11111\t0\t2024-01-01 00:00:00\t-\tfilesystem\t-\t-\t-\npool/src@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096\t-\t-\t-\t-",
			"pool/tgt": "pool/tgt\t33333\t0\t2024-01-01 00:00:00\t-\tfilesystem\t-\t-\t-\npool/tgt@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096\t-\t-\t-\t-",
		},
	}
	lines, err := backup.Commands(ctx, f, backup.Request{
		Source:   endpoint.MustParse("pool/src"),
		Target:   endpoint.MustParse("pool/tgt"),
		SnapMode: backup.SnapNever,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(len(lines))
	plan, err := backup.Prepare(ctx, f, backup.Request{
		Source:   endpoint.MustParse("pool/src"),
		Target:   endpoint.MustParse("pool/tgt"),
		SnapMode: backup.SnapNever,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(plan.Skip == 1)
	// Output:
	// 0
	// true
}

func ExampleRun_fromMemory() {
	// Integrator-shaped: build endpoints and dials without env or internal/.
	src := endpoint.Endpoint{Dataset: "tank/data"}
	tgt := endpoint.Endpoint{User: "root", Host: "backup", Dataset: "tank/data", Remote: true}
	flags := backup.DefaultSendRecv()
	flags.Bookmarks = true
	_ = backup.Request{
		Source:        src,
		Target:        tgt,
		SnapMode:      backup.SnapIfNeeded,
		SnapTime:      time.Hour,
		SnapSize:      1 << 20,
		SyncDirection: backup.DirectionPull,
		Flags:         &flags,
		OnLine:        func(line string) { /* progress */ },
	}
	_ = &zfs.Real{
		SSH: zfs.SSHConfig{
			IdentityFile: "/path/to/key",
			Options:      []string{"BatchMode=yes"},
		},
	}
}

func ExampleRun_fromStrings() {
	// Import edge: same types, filled via public Parse helpers (CLI/JSON path).
	src, _ := endpoint.Parse("tank/data")
	tgt, _ := endpoint.Parse("root@backup:tank/data")
	mode, _ := backup.ParseSnapMode("IF_NEEDED")
	dir, _ := backup.ParseSyncDirection("pull")
	st, _ := backup.ParseSnapTime("1h")
	ss, _ := backup.ParseSnapSize("1048576")
	_ = backup.Request{
		Source:        src,
		Target:        tgt,
		SnapMode:      mode,
		SyncDirection: dir,
		SnapTime:      st,
		SnapSize:      ss,
	}
}
