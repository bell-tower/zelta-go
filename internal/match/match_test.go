package match

import (
	"context"
	"testing"

	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

func TestCompareFake(t *testing.T) {
	fake := &zfs.Fake{Lists: map[string]string{
		"tank/src": "tank/src\tg1\ntank/src@a\tg2\n",
		"tank/tgt": "tank/tgt\tg1\ntank/tgt@a\tg2\n",
	}}
	res, err := Compare(context.Background(), fake, Request{
		Source: "tank/src",
		Target: "tank/tgt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SrcRows) != 2 || len(res.TgtRows) != 2 {
		t.Fatalf("rows src=%d tgt=%d", len(res.SrcRows), len(res.TgtRows))
	}
}
