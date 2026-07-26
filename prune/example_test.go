package prune_test

import (
	"context"
	"fmt"

	"git.belltower.it/djbell/zelta-go/prune"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func ExampleRun() {
	ctx := context.Background()
	f := &zfs.Fake{
		Lists: map[string]string{
			"pool/src": "pool/src\t11111\t0\t2024-01-01 00:00:00\t-\t-\t-",
		},
	}
	res, err := prune.Run(ctx, f, prune.Request{
		Source:     "pool/src",
		PruneGuard: prune.GuardLatest,
	})
	if err != nil {
		panic(err)
	}
	// Read-only: Candidates() lists snaps; destroy stays in CLI zprune / the app.
	fmt.Println(len(res.Candidates()))
	// Output:
	// 0
}
