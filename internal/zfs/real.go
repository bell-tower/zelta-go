package zfs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Real invokes local zfs (remote SSH later).
type Real struct {
	ZFS string // default "zfs"
}

func (r *Real) bin() string {
	if r.ZFS != "" {
		return r.ZFS
	}
	return "zfs"
}

func (r *Real) List(ctx context.Context, _, dataset string, props []string) ([]string, error) {
	if dataset == "" {
		return nil, fmt.Errorf("zfs list: empty dataset")
	}
	args := []string{"list", "-Hpr"}
	if len(props) > 0 {
		args = append(args, "-o", strings.Join(props, ","))
	}
	args = append(args, dataset)
	cmd := exec.CommandContext(ctx, r.bin(), args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("zfs list %s: %w", dataset, err)
	}
	return splitNonEmpty(string(out)), nil
}
