package zfs

import (
	"context"
	"fmt"
	"strings"
)

// Fake returns canned list dumps and records mutating calls.
type Fake struct {
	// Lists maps key → full zfs list -Hpr style body (newline-separated).
	Lists map[string]string
	// Props maps key → zfs get -Hpr name,property,value body (newline-separated).
	// When unset for a dataset that exists in Lists/Existing, GetProps returns
	// empty lines (no optional features) so list-only tests keep working.
	Props map[string]string
	// Snapshots records dataset@snap names passed to Snapshot.
	Snapshots []string
	// Creates records datasets passed to Create.
	Creates []string
	// Existing marks datasets Exists reports true for (plus any Lists keys).
	Existing map[string]bool
	// Pipes records left/right argv pairs from RunPipe.
	Pipes          []PipeCall
	Bookmarks      []BookmarkCall
	Clones         []CloneCall
	Renames        []RenameCall
	ListErrors     map[string]error
	PropErrors     map[string]error
	BookmarkErrors map[string]error
	PipeErrors     map[string]error
}

type BookmarkCall struct{ Endpoint, SourceSnap, Bookmark string }
type CloneCall struct{ Endpoint, SourceSnap, Dataset string }
type RenameCall struct{ Endpoint, OldDataset, NewDataset string }

// PipeCall is one RunPipe invocation.
type PipeCall struct {
	LeftEp, RightEp string
	Left, Right     []string
	Direction       string
}

func (f *Fake) List(_ context.Context, endpoint, dataset string, _ []string, _ int) ([]string, error) {
	if err := f.ListErrors[dataset]; err != nil {
		return nil, err
	}
	if f.Lists == nil {
		return nil, fmt.Errorf("zfs fake: no lists configured")
	}
	for _, key := range []string{endpoint, dataset, endpoint + ":" + dataset} {
		if body, ok := f.Lists[key]; ok {
			return splitNonEmpty(body), nil
		}
	}
	return nil, fmt.Errorf("zfs fake: no list for endpoint=%q dataset=%q", endpoint, dataset)
}

func (f *Fake) GetProps(_ context.Context, endpoint, dataset string, _ string, _ int) ([]string, error) {
	if err := f.PropErrors[dataset]; err != nil {
		return nil, err
	}
	for _, key := range []string{endpoint, dataset, endpoint + ":" + dataset} {
		if f.Props != nil {
			if body, ok := f.Props[key]; ok {
				return splitNonEmpty(body), nil
			}
		}
	}
	// Explicit Existing without Props: present, no optional features.
	if f.Existing != nil && (f.Existing[dataset] || f.Existing[endpoint]) {
		return nil, nil
	}
	// List-only fixtures: non-empty list dump ⇒ exists; empty body ⇒ missing target.
	for _, key := range []string{endpoint, dataset, endpoint + ":" + dataset} {
		if f.Lists != nil {
			if body, ok := f.Lists[key]; ok {
				if strings.TrimSpace(body) == "" {
					return nil, fmt.Errorf("cannot open '%s': dataset does not exist", dataset)
				}
				return nil, nil
			}
		}
	}
	return nil, fmt.Errorf("cannot open '%s': dataset does not exist", dataset)
}

func (f *Fake) hasDataset(dataset string) bool {
	if dataset == "" {
		return false
	}
	if f.Existing != nil && f.Existing[dataset] {
		return true
	}
	if f.Props != nil {
		if _, ok := f.Props[dataset]; ok {
			return true
		}
	}
	if f.Lists != nil {
		if body, ok := f.Lists[dataset]; ok && strings.TrimSpace(body) != "" {
			return true
		}
	}
	return false
}

func (f *Fake) Snapshot(_ context.Context, _, datasetSnap string, _ bool) error {
	f.Snapshots = append(f.Snapshots, datasetSnap)
	return nil
}

func (f *Fake) Create(_ context.Context, _, dataset string) error {
	f.Creates = append(f.Creates, dataset)
	if f.Existing == nil {
		f.Existing = make(map[string]bool)
	}
	f.Existing[dataset] = true
	return nil
}

func (f *Fake) Exists(_ context.Context, _, dataset string) (bool, error) {
	return f.hasDataset(dataset), nil
}

func (f *Fake) Bookmark(_ context.Context, endpoint, sourceSnap, bookmark string) error {
	if err := f.BookmarkErrors[bookmark]; err != nil {
		return err
	}
	f.Bookmarks = append(f.Bookmarks, BookmarkCall{endpoint, sourceSnap, bookmark})
	return nil
}

func (f *Fake) Clone(_ context.Context, endpoint, sourceSnap, dataset string) error {
	f.Clones = append(f.Clones, CloneCall{endpoint, sourceSnap, dataset})
	return nil
}

func (f *Fake) Rename(_ context.Context, endpoint, oldDataset, newDataset string) error {
	f.Renames = append(f.Renames, RenameCall{endpoint, oldDataset, newDataset})
	return nil
}

func (f *Fake) RunPipe(ctx context.Context, leftEp string, leftArgv []string, rightEp string, rightArgv []string) error {
	return f.RunPipeDirection(ctx, leftEp, leftArgv, rightEp, rightArgv, "")
}

func (f *Fake) RunPipeDirection(_ context.Context, leftEp string, leftArgv []string, rightEp string, rightArgv []string, direction string) error {
	f.Pipes = append(f.Pipes, PipeCall{
		LeftEp:    leftEp,
		RightEp:   rightEp,
		Left:      append([]string(nil), leftArgv...),
		Right:     append([]string(nil), rightArgv...),
		Direction: direction,
	})
	if len(leftArgv) > 0 {
		if err := f.PipeErrors[leftArgv[len(leftArgv)-1]]; err != nil {
			return err
		}
	}
	return nil
}

func splitNonEmpty(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
