package zfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/cmdbuild"
)

func osPipe() (io.ReadCloser, io.WriteCloser, error) {
	return os.Pipe()
}

// Real invokes local or remote zfs.
//
// Remote transport:
//   - If Remote != nil, use it (SSHConfig, CommandRemote, or custom).
//   - Else use SSH (structured OpenSSH). Zero SSH is plain ssh with BatchMode.
type Real struct {
	ZFS string // default "zfs"
	// SSH is used when Remote is nil.
	SSH SSHConfig
	// Remote overrides SSH when non-nil.
	Remote Remote

	// StderrLog, when non-nil, receives a copy of stderr from zfs pipe
	// commands. Useful for progress logging (see backup.Request.OnLine).
	StderrLog io.Writer
}

// SetStderrLog implements the optional progress-tee hook used by backup.OnLine.
func (r *Real) SetStderrLog(w io.Writer) {
	r.StderrLog = w
}

func (r *Real) bin() string {
	if r.ZFS != "" {
		return r.ZFS
	}
	return "zfs"
}

func (r *Real) remote() Remote {
	if r.Remote != nil {
		return r.Remote
	}
	return r.SSH
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
		// OpenZFS may create the clone then fail mount when non-root:
		// "filesystem successfully created, but it may only be mounted by root"
		if strings.Contains(err.Error(), "successfully created") {
			return nil
		}
		return fmt.Errorf("zfs clone %s: %w", dataset, err)
	}
	return nil
}

func (r *Real) Rename(ctx context.Context, epStr, oldDataset, newDataset string) error {
	argv, err := cmdbuild.RenameArgv(oldDataset, newDataset)
	if err != nil {
		return err
	}
	if _, err := r.output(ctx, epStr, r.rewriteBin(argv)); err != nil {
		return fmt.Errorf("zfs rename %s: %w", oldDataset, err)
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
		cmd, err := r.remoteCmd(ctx, lt, remote, RoleDefault)
		if err != nil {
			return err
		}
		if r.StderrLog != nil {
			out, err := r.runWithStderrLog(ctx, cmd)
			if err != nil {
				return fmt.Errorf("zfs pipe on %s: %w\n%s", lt, err, out)
			}
		} else {
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("zfs pipe on %s: %w\n%s", lt, err, out)
			}
		}
		return nil
	}

	// Dual-remote with direction: run the whole pipe on one host.
	if lok && rok {
		switch strings.ToUpper(direction) {
		case "PULL":
			// On target: ssh -n src 'send' | recv
			innerSend, err := r.remoteShell(lt, shellJoin(leftArgv), RoleSend)
			if err != nil {
				return err
			}
			inner := innerSend + " | " + shellJoin(rightArgv)
			cmd, err := r.remoteCmd(ctx, rt, inner, RoleDefault)
			if err != nil {
				return err
			}
			if r.StderrLog != nil {
				out, err := r.runWithStderrLog(ctx, cmd)
				if err != nil {
					return fmt.Errorf("zfs pull pipe on %s: %w\n%s", rt, err, out)
				}
			} else {
				if out, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("zfs pull pipe on %s: %w\n%s", rt, err, out)
				}
			}
			return nil
		case "PUSH":
			// On source: send | ssh tgt 'recv'
			innerRecv, err := r.remoteShell(rt, shellJoin(rightArgv), RoleRecv)
			if err != nil {
				return err
			}
			inner := shellJoin(leftArgv) + " | " + innerRecv
			cmd, err := r.remoteCmd(ctx, lt, inner, RoleDefault)
			if err != nil {
				return err
			}
			if r.StderrLog != nil {
				out, err := r.runWithStderrLog(ctx, cmd)
				if err != nil {
					return fmt.Errorf("zfs push pipe on %s: %w\n%s", lt, err, out)
				}
			} else {
				if out, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("zfs push pipe on %s: %w\n%s", lt, err, out)
				}
			}
			return nil
		}
		// Proxy (controller-side): fall through to remote|remote below.
	}

	pctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// cmds.tsv roles: SEND → -n; RECV → stdin open for stream.
	leftCmd, err := r.commandOpts(pctx, lep, leftArgv, RoleSend)
	if err != nil {
		return err
	}
	rightCmd, err := r.commandOpts(pctx, rep, rightArgv, RoleRecv)
	if err != nil {
		return err
	}
	pr, pw, err := osPipe()
	if err != nil {
		return err
	}
	leftCmd.Stdout = pw
	rightCmd.Stdin = pr

	var leftStderr, rightStderr bytes.Buffer
	if r.StderrLog != nil {
		leftCmd.Stderr = io.MultiWriter(&leftStderr, r.StderrLog)
		rightCmd.Stderr = io.MultiWriter(&rightStderr, r.StderrLog)
	} else {
		leftCmd.Stderr = &leftStderr
		rightCmd.Stderr = &rightStderr
	}

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
		return fmt.Errorf("zfs send: %w; zfs recv: %v\nsend stderr: %s\nrecv stderr: %s",
			leftErr, rightErr, strings.TrimSpace(leftStderr.String()), strings.TrimSpace(rightStderr.String()))
	}
	if leftErr != nil {
		return fmt.Errorf("zfs send: %w\n%s", leftErr, strings.TrimSpace(leftStderr.String()))
	}
	if rightErr != nil {
		return fmt.Errorf("zfs recv: %w\n%s", rightErr, strings.TrimSpace(rightStderr.String()))
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
	return r.commandOpts(ctx, ep, argv, RoleDefault)
}

// commandOpts builds local or remote-wrapped argv.
func (r *Real) commandOpts(ctx context.Context, ep endpoint.Endpoint, argv []string, role Role) (*exec.Cmd, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("empty argv")
	}
	if target, ok := sshTarget(ep); ok {
		return r.remoteCmd(ctx, target, shellJoin(argv), role)
	}
	return exec.CommandContext(ctx, argv[0], argv[1:]...), nil
}

func (r *Real) remoteCmd(ctx context.Context, target, remoteCmd string, role Role) (*exec.Cmd, error) {
	argv, err := r.remote().Argv(target, remoteCmd, role)
	if err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, fmt.Errorf("zfs remote: empty argv")
	}
	return exec.CommandContext(ctx, argv[0], argv[1:]...), nil
}

func (r *Real) remoteShell(target, remoteCmd string, role Role) (string, error) {
	return r.remote().Shell(target, remoteCmd, role)
}

// runWithStderrLog runs cmd with separate stdout/stderr pipes, writes stderr
// to r.StderrLog, and returns stdout + any exit error.
func (r *Real) runWithStderrLog(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, _ = io.Copy(&outBuf, stdout)
	}()
	go func() {
		if r.StderrLog != nil {
			_, _ = io.Copy(io.MultiWriter(&errBuf, r.StderrLog), stderr)
		} else {
			_, _ = io.Copy(&errBuf, stderr)
		}
	}()
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			return outBuf.Bytes(), fmt.Errorf("%w: %s", err, strings.TrimSpace(errBuf.String()))
		}
		return outBuf.Bytes(), nil
	case <-ctx.Done():
		_ = cmd.Cancel()
		return nil, ctx.Err()
	}
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
