package zfs

import (
	"context"
	"fmt"
	"strings"
)

// Fake returns canned list dumps keyed by endpoint raw string or dataset.
type Fake struct {
	// Lists maps key → full zfs list -Hpr style body (newline-separated).
	Lists map[string]string
}

func (f *Fake) List(_ context.Context, endpoint, dataset string, _ []string) ([]string, error) {
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

func splitNonEmpty(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
