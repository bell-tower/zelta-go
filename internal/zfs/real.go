package zfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
	"git.belltower.it/djbell/zelta-go/internal/endpoint"
)

func osPipe() (io.ReadCloser, io.WriteCloser, error) {
	return os.Pipe()
}

// Real invokes local or remote zfs (ssh for remote endpoints).
type Real struct {
	ZFS string // default "zfs"
	SSH string // default "ssh"
}

func (r *Real) bin() string {
	if r.ZFS != "" {
		return r.ZFS
	}
	return "zfs"
}

func (r *Real) sshBin() string {
	if r.SSH != "" {
		return r.SSH
	}
	return "ssh"
}

func (r *Real) List(ctx context.Context, epStr, dataset string, props []string, depth int) ([]string, error) {
	if dataset == "" {
		return nil, fmt.Errorf("zfs list: empty dataset")
	}
	argv, err := cmdbuild.ListArgv(props, depth, dataset)
	if err != nil {
		return nil, err
	}
	argv = r.rewriteBin(argv)
	out, err := r.output(ctx, epStr, argv)
	if err != nil {
		// Oracle match ignores missing datasets (full backup to new target).
		if isMissingDataset(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("zfs list %s: %w", dataset, err)
	}
	return splitNonEmpty(string(out)), nil
}

func isMissingDataset(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		msg += string(ee.Stderr)
	}
	return strings.Contains(msg, "dataset does not exist")
}

func (r *Real) Snapshot(ctx context.Context, epStr, datasetSnap string, recursive bool) error {
	if datasetSnap == "" {
		return fmt.Errorf("zfs snapshot: empty name")
	}
	var argv []string
	if recursive {
		// cmds.tsv SNAP includes -r
		var err error
		argv, err = cmdbuild.SnapArgv(datasetSnap)
		if err != nil {
			return err
		}
		argv = r.rewriteBin(argv)
	} else {
		argv = []string{r.bin(), "snapshot", datasetSnap}
	}
	if _, err := r.output(ctx, epStr, argv); err != nil {
		return fmt.Errorf("zfs snapshot %s: %w", datasetSnap, err)
	}
	return nil
}

func (r *Real) Create(ctx context.Context, epStr, dataset string) error {
	if dataset == "" {
		return fmt.Errorf("zfs create: empty dataset")
	}
	argv, err := cmdbuild.CreateArgv(dataset)
	if err != nil {
		return err
	}
	argv = r.rewriteBin(argv)
	// CombinedOutput: zfs prints the readonly bug on stderr; may still create one level.
	out, err := r.combined(ctx, epStr, argv)
	msg := string(out)
	if err != nil {
		msg = msg + err.Error()
	}
	if strings.Contains(msg, "permission denied") {
		return fmt.Errorf("permission denied creating target '%s'", dataset)
	}
	if strings.Contains(msg, "no such pool") {
		return fmt.Errorf("no such pool in target path: '%s'", dataset)
	}
	if strings.Contains(msg, "Read-only file system") {
		return ErrReadOnlyCreate
	}
	if err != nil {
		if isDatasetExists(err) || strings.Contains(msg, "dataset already exists") {
			return nil
		}
		return fmt.Errorf("zfs create %s: %w\n%s", dataset, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *Real) combined(ctx context.Context, epStr string, argv []string) ([]byte, error) {
	ep, err := endpoint.Parse(epStr)
	if err != nil {
		return nil, err
	}
	cmd, err := r.command(ctx, ep, argv)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

func (r *Real) Exists(ctx context.Context, epStr, dataset string) (bool, error) {
	if dataset == "" {
		return false, fmt.Errorf("zfs exists: empty dataset")
	}
	argv, err := cmdbuild.CheckArgv(dataset)
	if err != nil {
		return false, err
	}
	argv = r.rewriteBin(argv)
	out, err := r.output(ctx, epStr, argv)
	if err != nil {
		if isMissingDataset(err) {
			return false, nil
		}
		return false, fmt.Errorf("zfs list %s: %w", dataset, err)
	}
	for _, line := range splitNonEmpty(string(out)) {
		if line == dataset {
			return true, nil
		}
	}
	return false, nil
}

func (r *Real) Bookmark(ctx context.Context, epStr, sourceSnap, bookmark string) error {
	argv, err := cmdbuild.BookmarkArgv(sourceSnap, bookmark)
	if err != nil {
		return err
	}
	argv = r.rewriteBin(argv)
	if _, err := r.output(ctx, epStr, argv); err != nil {
		return fmt.Errorf("zfs bookmark %s: %w", bookmark, err)
	}
	return nil
}

func (r *Real) Clone(ctx context.Context, epStr, sourceSnap, dataset string) error {
	argv, err := cmdbuild.CloneArgv(sourceSnap, dataset)
	if err != nil {
		return err
	}
	if _, err := r.output(ctx, epStr, r.rewriteBin(argv)); err != nil {
		return fmt.Errorf("zfs clone %s: %w", dataset, err)
	}
	return nil
}

func isDatasetExists(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		msg += string(ee.Stderr)
	}
	return strings.Contains(msg, "dataset already exists")
}

func (r *Real) RunPipe(ctx context.Context, leftEp string, leftArgv []string, rightEp string, rightArgv []string) error {
	return r.RunPipeDirection(ctx, leftEp, leftArgv, rightEp, rightArgv, "PULL")
}

func (r *Real) RunPipeDirection(ctx context.Context, leftEp string, leftArgv []string, rightEp string, rightArgv []string, direction string) error {
	if len(leftArgv) == 0 || len(rightArgv) == 0 {
		return fmt.Errorf("zfs pipe: empty argv")
	}
	lep, err := endpoint.Parse(leftEp)
	if err != nil {
		return fmt.Errorf("zfs pipe left: %w", err)
	}
	rep, err := endpoint.Parse(rightEp)
	if err != nil {
		return fmt.Errorf("zfs pipe right: %w", err)
	}
	lt, lok := sshTarget(lep)
	rt, rok := sshTarget(rep)

	// Same remote host: one ssh shell with pipe (oracle hairpin).
	if lok && rok && lt == rt {
		remote := "{ " + shellJoin(leftArgv) + " | " + shellJoin(rightArgv) + " ; }"
		cmd := r.sshCmd(ctx, lt, remote, true)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("zfs pipe on %s: %w\n%s", lt, err, out)
		}
		return nil
	}

	// Dual-remote with direction: run the whole pipe on one host.
	if lok && rok {
		switch strings.ToUpper(direction) {
		case "PULL":
			// On target: ssh -n src 'send' | recv
			inner := "ssh -n " + shellSingleQuote(lt) + " " + shellSingleQuote(shellJoin(leftArgv)) +
				" | " + shellJoin(rightArgv)
			cmd := r.sshCmd(ctx, rt, inner, true)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("zfs pull pipe on %s: %w\n%s", rt, err, out)
			}
			return nil
		case "PUSH":
			// On source: send | ssh tgt 'recv'
			inner := shellJoin(leftArgv) +
				" | ssh " + shellSingleQuote(rt) + " " + shellSingleQuote(shellJoin(rightArgv))
			cmd := r.sshCmd(ctx, lt, inner, true)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("zfs push pipe on %s: %w\n%s", lt, err, out)
			}
			return nil
		}
		// Proxy (controller-side): fall through to ssh|ssh below.
	}

	pctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// cmds.tsv roles: SEND → ssh -n; RECV → ssh (stdin open for stream).
	leftCmd, err := r.commandOpts(pctx, lep, leftArgv, cmdbuild.StdinNull(cmdbuild.RoleSend))
	if err != nil {
		return err
	}
	rightCmd, err := r.commandOpts(pctx, rep, rightArgv, cmdbuild.StdinNull(cmdbuild.RoleRecv))
	if err != nil {
		return err
	}
	pr, pw, err := osPipe()
	if err != nil {
		return err
	}
	leftCmd.Stdout = pw
	rightCmd.Stdin = pr
	if err := leftCmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return fmt.Errorf("zfs pipe start send: %w", err)
	}
	if err := rightCmd.Start(); err != nil {
		pw.Close()
		cancel()
		_ = leftCmd.Wait()
		pr.Close()
		return fmt.Errorf("zfs pipe start recv: %w", err)
	}
	pw.Close() // parent drops write end; send owns the only writer

	type sideErr struct {
		side string
		err  error
	}
	errc := make(chan sideErr, 2)
	go func() { errc <- sideErr{"send", leftCmd.Wait()} }()
	go func() { errc <- sideErr{"recv", rightCmd.Wait()} }()
	var leftErr, rightErr error
	for i := 0; i < 2; i++ {
		se := <-errc
		if se.err != nil {
			cancel() // kill peer if stuck on a full/broken pipe
		}
		switch se.side {
		case "send":
			leftErr = se.err
		default:
			rightErr = se.err
		}
	}
	pr.Close()
	if leftErr != nil && rightErr != nil {
		return fmt.Errorf("zfs send: %w; zfs recv: %v", leftErr, rightErr)
	}
	if leftErr != nil {
		return fmt.Errorf("zfs send: %w", leftErr)
	}
	if rightErr != nil {
		return fmt.Errorf("zfs recv: %w", rightErr)
	}
	return nil
}

func (r *Real) output(ctx context.Context, epStr string, argv []string) ([]byte, error) {
	ep, err := endpoint.Parse(epStr)
	if err != nil {
		return nil, err
	}
	cmd, err := r.command(ctx, ep, argv)
	if err != nil {
		return nil, err
	}
	out, err := cmd.Output()
	if err != nil {
		// Wrap so isMissingDataset can see stderr text in Error() too.
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return out, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return out, err
	}
	return out, nil
}

func (r *Real) command(ctx context.Context, ep endpoint.Endpoint, argv []string) (*exec.Cmd, error) {
	// List/snapshot use DEFAULT role → ssh -n.
	return r.commandOpts(ctx, ep, argv, cmdbuild.StdinNull(cmdbuild.RoleDefault))
}

// commandOpts builds local or ssh argv. stdinNull → ssh -n (DEFAULT/SEND); false keeps stdin (RECV).
func (r *Real) commandOpts(ctx context.Context, ep endpoint.Endpoint, argv []string, stdinNull bool) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty argv")
	}
	if target, ok := sshTarget(ep); ok {
		return r.sshCmd(ctx, target, shellJoin(argv), stdinNull), nil
	}
	return exec.CommandContext(ctx, argv[0], argv[1:]...), nil
}

func (r *Real) sshCmd(ctx context.Context, target, remote string, stdinNull bool) *exec.Cmd {
	args := make([]string, 0, 8)
	if stdinNull {
		args = append(args, "-n")
	}
	args = append(args,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=30",
		target,
		remote,
	)
	return exec.CommandContext(ctx, r.sshBin(), args...)
}

// rewriteBin swaps leading "zfs" for Real.ZFS when overridden.
func (r *Real) rewriteBin(argv []string) []string {
	if len(argv) == 0 {
		return argv
	}
	bin := r.bin()
	if argv[0] == "zfs" && bin != "zfs" {
		out := append([]string(nil), argv...)
		out[0] = bin
		return out
	}
	return argv
}

// sshTarget returns user@host (or host) when ep needs ssh.
// localhost / empty host → local (Awk clears remote for bare localhost).
func sshTarget(ep endpoint.Endpoint) (string, bool) {
	if !ep.Remote || ep.Host == "" || ep.Host == "localhost" {
		return "", false
	}
	if ep.User != "" {
		return ep.User + "@" + ep.Host, true
	}
	return ep.Host, true
}

// shellJoin quotes args for a single remote shell command string.
func shellJoin(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = shellSingleQuote(a)
	}
	return strings.Join(parts, " ")
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
