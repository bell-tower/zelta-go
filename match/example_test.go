package match_test

import (
	"context"
	"fmt"

	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func ExampleCompare() {
	ctx := context.Background()
	f := &zfs.Fake{
		Lists: map[string]string{
			"pool/src": "pool/src\t11111\t0\t2024-01-01 00:00:00\t-\npool/src@a\t22222\t0\t2024-01-01 01:00:00\t0",
			"pool/tgt": "pool/tgt\t33333\t0\t2024-01-01 00:00:00\t-\npool/tgt@a\t22222\t0\t2024-01-01 01:00:00\t0",
		},
	}
	res, err := match.Compare(ctx, f, match.Request{
		Source: "pool/src",
		Target: "pool/tgt",
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(res != nil && len(res.Pairs) >= 1)
	// Output:
	// true
}
