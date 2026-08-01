package backup_test

import (
	"context"
	"fmt"
	"git.belltower.it/djbell/zelta-go/endpoint"

	"git.belltower.it/djbell/zelta-go/backup"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func ExampleRun_dryRun() {
	ctx := context.Background()
	f := &zfs.Fake{
		Lists: map[string]string{
			"pool/src": "pool/src\t11111\t0\t2024-01-01 00:00:00\t-\tfilesystem\t-\t-\t-\npool/src@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096\t-\t-\t-\t-",
			"pool/tgt": "pool/tgt\t33333\t0\t2024-01-01 00:00:00\t-\tfilesystem\t-\t-\t-\npool/tgt@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096\t-\t-\t-\t-",
		},
	}
	res, err := backup.Run(ctx, f, backup.Request{
		Source: endpoint.MustParse("pool/src"),
		Target: endpoint.MustParse("pool/tgt"),
		DryRun: true,
		JSON:   true,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(res.ErrCode == backup.ErrCodeUpToDate || res.ErrCode == backup.ErrCodeNone)
	fmt.Println(res.JSONReport != nil)
	// Output:
	// true
	// true
}

func ExampleRun_withSSH() {
	// Production-shaped call (does not run here — needs real endpoints).
	_ = backup.Request{
		Source: endpoint.MustParse("tank/data"),
		Target: endpoint.MustParse("backup/data"),
		OnLine: func(line string) { /* progress */ },
	}
	_ = &zfs.Real{
		SSH: zfs.SSHConfig{
			IdentityFile: "/path/to/key",
			Options:      []string{"BatchMode=yes"},
		},
	}
}
