package zelta_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/bell-tower/zelta-go/zelta"
)

func Example() {
	ctx := context.Background()
	c, err := zelta.New(zelta.WithLogger(slog.Default()))
	if err != nil {
		log.Fatal(err)
	}
	res, err := c.Backup(ctx, "tank/home", "backup@vault:pool/backups/home", &zelta.BackupOptions{
		Snapshot: zelta.SnapshotPolicy{Mode: zelta.SnapshotIfNeeded, MinInterval: time.Hour},
		Parallel: 4,
	})
	var zerr *zelta.Error
	if errors.As(err, &zerr) && res != nil {
		// Some datasets failed; the others were synced.
		for _, d := range res.Datasets {
			if !d.UpToDate {
				fmt.Printf("%s: %s\n", d.Target, d.Notice)
			}
		}
	} else if err != nil {
		log.Fatal(err)
	}
}

func ExampleClient_Match() {
	c, _ := zelta.New()
	cmp, err := c.Match(context.Background(), "tank/home", "vault:pool/backups/home", nil)
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range cmp.Datasets {
		fmt.Printf("%-20s %-10s %s\n", "home"+d.Suffix, d.State, d.Match)
	}
	fmt.Println(cmp.Summary)
}

func ExampleClient_PlanPrune() {
	c, _ := zelta.New()
	plan, err := c.PlanPrune(context.Background(), "tank/home", &zelta.PruneOptions{
		Guard: "vault:pool/backups/home", // keep what the replica still needs
		Grid:  "24x1 hour, 30x1 day, 12x1 month",
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range plan.Candidates() {
		fmt.Println("would destroy", c)
	}
}

func ExampleClient_Prune() {
	// A planner re-runs PlanPrune as the user edits the policy, shows the
	// result, and destroys only what the user approved.
	c, _ := zelta.New()
	ctx := context.Background()
	opts := &zelta.PruneOptions{Guard: "vault:pool/backups/home", Keep: 30}
	seen, err := c.PlanPrune(ctx, "tank/home", opts)
	if err != nil {
		log.Fatal(err)
	}
	opts.Approved = seen
	plan, err := c.Prune(ctx, "tank/home", opts)
	if err != nil {
		log.Fatal(err)
	}
	for _, s := range plan.Skipped {
		fmt.Println("no longer a candidate, kept:", s)
	}
}

func ExampleWithEnvironment() {
	// Behave like the zelta command on this host: read ZELTA_* variables
	// and zelta.env, then override one option.
	c, err := zelta.New(
		zelta.WithEnvironment(),
		zelta.WithSettings(map[string]string{"RECV_TOP": "-o readonly=on -o compression=zstd"}),
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = c
}

func ExampleParseEndpoint() {
	ep, _ := zelta.ParseEndpoint("backup@vault:pool/backups/home@nightly")
	fmt.Println(ep.User, ep.Host, ep.Dataset, ep.Snapshot)
	ep.Snapshot = ""
	fmt.Println(ep)
	// Output:
	// backup vault pool/backups/home nightly
	// backup@vault:pool/backups/home
}
