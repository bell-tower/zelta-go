package match

import (
	"testing"

	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

func TestTreeCarriesDatasetOrigin(t *testing.T) {
	tree, err := buildTree("tank/src", []zfs.ListRow{{
		Name:  "tank/src",
		Props: map[string]string{"guid": "1", "origin": "tank/base@seed", "type": "filesystem"},
	}}, ParseFilter(nil, nil), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := tree.get("").Origin; got != "tank/base@seed" {
		t.Fatalf("origin=%q", got)
	}
}
