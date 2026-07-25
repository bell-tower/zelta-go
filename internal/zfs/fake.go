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
	// Snapshots records dataset@snap names passed to Snapshot.
	Snapshots []string
	// Creates records datasets passed to Create.
	Creates []string
	// Existing marks datasets Exists reports true for (plus any Lists keys).
	Existing map[string]bool
	// Pipes records left/right argv pairs from RunPipe.
	Pipes []PipeCall
}

// PipeCall is one RunPipe invocation.
type PipeCall struct {
	LeftEp, RightEp string
	Left, Right     []string
	Direction       string
}

func (f *Fake) List(_ context.Context, endpoint, dataset string, _ []string, _ int) ([]string, error) {
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
	if f.Existing != nil && f.Existing[dataset] {
		return true, nil
	}
	if f.Lists != nil {
		if _, ok := f.Lists[dataset]; ok {
			return true, nil
		}
	}
	return false, nil
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
