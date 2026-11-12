// Package failover implements "zelta lock", "zelta unlock", "zelta propsync",
// and "zelta failover", which compose an active-passive promotion of a
// dataset tree: lock the active source, run a final backup, replay local
// properties onto the standby, and unlock it.
package failover

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/shellwords"
	"github.com/bell-tower/zelta-go/internal/zfs"
)

// Backup runs "zelta backup" in-process; the CLI sets it to backup.Run.
var Backup func(ctx context.Context, rt *app.Runtime, args []string) error

// Exit statuses.
const (
	// codeMount reports a non-critical mount or unmount failure.
	codeMount = 1
	// codeBackup reports a failed final backup after the source was locked.
	codeBackup = 2
	// codeReadonly reports that readonly could not be set or cleared.
	codeReadonly = 255
)

// runner executes the commands of one invocation and tracks the highest
// non-fatal error status.
type runner struct {
	ctx  context.Context
	rt   *app.Runtime
	code int
}

func newRunner(ctx context.Context, rt *app.Runtime) *runner { return &runner{ctx: ctx, rt: rt} }

// result converts the tracked status into the command's error.
func (r *runner) result(err error) error {
	if err != nil {
		return err
	}
	if r.code != 0 {
		return &app.ExitError{Code: r.code}
	}
	return nil
}

func (r *runner) dry() bool { return r.rt.DryRun() }

// zfs builds a zfs command on ep's host. Remote commands are passed to the
// remote shell as one quoted string, as Zelta has always displayed them.
func (r *runner) zfs(ep endpoint.Endpoint, args ...command.Arg) command.Cmd {
	local := zfs.Local(args...)
	if rem := r.rt.Remote(ep, zfs.RemoteDefault); rem != nil {
		return command.Cmd{Remote: rem, Script: local.String()}
	}
	return local
}

// exec shows a command in dry-run mode, or runs it, passing its output
// through like system(3) would. It reports success.
func (r *runner) exec(c command.Cmd) bool {
	res := r.run(c)
	r.forward(res.Lines)
	return res.OK()
}

// run shows a command in dry-run mode, where it succeeds without running, or
// runs it and returns its result without passing its output through.
func (r *runner) run(c command.Cmd) execx.Result {
	if r.dry() {
		r.rt.Log.Noticef("+ %s", c.String())
		return execx.Result{}
	}
	r.rt.Log.Infof("+ %s", c.String())
	return r.rt.Exec.Run(r.ctx, c)
}

func (r *runner) forward(lines []execx.Line) {
	for _, l := range lines {
		w := r.rt.Stdout
		if l.Stream == execx.Stderr {
			w = r.rt.Stderr
		}
		fmt.Fprintln(w, app.SanitizeLine(l.Text))
	}
}

// must runs a command that may not fail; failure ends the command with code.
func (r *runner) must(c command.Cmd, code int, msg string) error {
	if r.exec(c) {
		return nil
	}
	return app.Exit(code, "%s: %s", msg, c.String())
}

// try runs a command whose failure is reported but not fatal.
func (r *runner) try(c command.Cmd, msg string) {
	if !r.exec(c) {
		r.mountFailed(c, msg)
	}
}

// mountFailed reports a failed mount or unmount, which is not fatal.
func (r *runner) mountFailed(c command.Cmd, msg string) {
	r.rt.Log.Errorf("%s: %s", msg, c.String())
	if codeMount > r.code {
		r.code = codeMount
	}
}

// mountTree runs "zfs mount -R" on ep's tree. OpenZFS 2.2 has no -R and
// rejects it; the per-dataset mounts that follow do the same work, so unlike
// Zelta 1.2 that is not reported as a failure.
func (r *runner) mountTree(ep endpoint.Endpoint) {
	c := r.zfs(ep, command.Word("mount"), command.Word("-R"), command.Name(ep.DS))
	res := r.run(c)
	if res.OK() {
		r.forward(res.Lines)
		return
	}
	if slices.ContainsFunc(res.Stderr(), func(s string) bool { return strings.Contains(s, "invalid option 'R'") }) {
		r.rt.Log.Infof("zfs mount -R is not supported; mounting each dataset")
		return
	}
	r.forward(res.Lines)
	r.mountFailed(c, "failed recursive mount during unlock")
}

// rows runs a listing and returns its tab-separated stdout rows; other
// output is passed through.
func (r *runner) rows(c command.Cmd) [][]string {
	res := r.rt.Run(r.ctx, c)
	var rows [][]string
	for _, l := range res.Lines {
		text := app.SanitizeLine(l.Text)
		if l.Stream == execx.Stderr {
			fmt.Fprintln(r.rt.Stderr, text)
			continue
		}
		if text != "" {
			rows = append(rows, zfs.Fields(text))
		}
	}
	return rows
}

// script renders a dry-run shell pipeline for ep: the text as-is locally, or
// wrapped in double quotes for the remote shell, as Zelta has always shown.
func (r *runner) script(ep endpoint.Endpoint, text string) string {
	rem := r.rt.Remote(ep, zfs.RemoteDefault)
	if rem == nil {
		return text
	}
	return shellwords.Join(append(append([]string{}, rem.Prefix...), rem.Host)) + ` "` + text + `"`
}

func listArgs(props, ds string, sort ...string) []command.Arg {
	args := append(command.Words("list", "-Hpr", "-o", props, "-t", "filesystem"), command.Words(sort...)...)
	return append(args, command.Name(ds))
}

func (r *runner) unmountFlags() []command.Arg {
	if r.rt.Opts.Bool("FORCE_UNMOUNT") {
		return command.Words("-f")
	}
	return nil
}

// lock makes a dataset tree read-only, sets canmount=noauto everywhere, and
// unmounts mounted filesystems unless NO_UNMOUNT is set.
func (r *runner) lock(ep endpoint.Endpoint) error {
	names := listArgs("name", ep.DS, "-Screatetxg")
	mounts := listArgs("name,mounted", ep.DS, "-Screatetxg")
	readonly := r.zfs(ep, command.Word("set"), command.Word("readonly=on"), command.Name(ep.DS))
	unmount := !r.rt.Opts.Bool("NO_UNMOUNT")
	if r.dry() {
		r.rt.Log.Noticef("+ %s", readonly.String())
		r.rt.Log.Noticef("+ %s", r.script(ep, zfs.Local(names...).String()+" | xargs -n1 zfs set canmount=noauto"))
		if unmount {
			umount := zfs.Local(append(command.Words("unmount"), r.unmountFlags()...)...).String()
			r.rt.Log.Noticef("+ %s", r.script(ep, zfs.Local(mounts...).String()+
				` | while read ds mounted; do [ "$mounted" = yes ] && `+umount+` "$ds"; done`))
		}
		return nil
	}
	if err := r.must(readonly, codeReadonly, "failed to set readonly; dataset is not safely locked"); err != nil {
		return err
	}
	for _, row := range r.rows(r.zfs(ep, names...)) {
		if err := r.must(r.zfs(ep, command.Word("set"), command.Word("canmount=noauto"), command.Name(row[0])), 1, "command failed"); err != nil {
			return err
		}
	}
	if !unmount {
		return nil
	}
	for _, row := range r.rows(r.zfs(ep, mounts...)) {
		if len(row) > 1 && row[1] == "yes" {
			args := append(append(command.Words("unmount"), r.unmountFlags()...), command.Name(row[0]))
			r.try(r.zfs(ep, args...), "failed to unmount locked dataset")
		}
	}
	return nil
}

// unlock reverses lock: it inherits readonly, sets canmount=on, and mounts
// the tree.
func (r *runner) unlock(ep endpoint.Endpoint) error {
	names := listArgs("name", ep.DS, "-s", "createtxg")
	mounts := listArgs("name,canmount,mounted", ep.DS, "-s", "createtxg")
	inherit := r.zfs(ep, command.Word("inherit"), command.Word("readonly"), command.Name(ep.DS))
	if err := r.must(inherit, codeReadonly, "failed to clear readonly; dataset is not safely unlocked"); err != nil {
		return err
	}
	if r.dry() {
		r.rt.Log.Noticef("+ %s", r.script(ep, zfs.Local(names...).String()+" | xargs -n1 zfs set canmount=on"))
	} else {
		for _, row := range r.rows(r.zfs(ep, names...)) {
			if err := r.must(r.zfs(ep, command.Word("set"), command.Word("canmount=on"), command.Name(row[0])), 1, "command failed"); err != nil {
				return err
			}
		}
	}
	r.mountTree(ep)
	if r.dry() {
		r.rt.Log.Noticef("+ %s", r.script(ep, zfs.Local(mounts...).String()+
			` | while read ds canmount mounted; do [ "$canmount" != off ] && [ "$mounted" != yes ] && zfs mount "$ds"; done`))
		return nil
	}
	for _, row := range r.rows(r.zfs(ep, mounts...)) {
		if len(row) > 2 && row[1] != "off" && row[2] != "yes" {
			r.try(r.zfs(ep, command.Word("mount"), command.Name(row[0])), "failed to mount unlocked dataset")
		}
	}
	return nil
}

// props holds local properties by dataset suffix and property name.
type props map[string]map[string]string

func (p props) set(suffix, prop, val string) {
	if p[suffix] == nil {
		p[suffix] = map[string]string{}
	}
	p[suffix][prop] = val
}

func (p props) has(suffix, prop string) bool {
	_, ok := p[suffix][prop]
	return ok
}

// each calls fn for every property, parents before children and properties
// in name order, so replays are deterministic.
func (p props) each(fn func(suffix, prop, val string) error) error {
	suffixes := make([]string, 0, len(p))
	for s := range p {
		suffixes = append(suffixes, s)
	}
	sort.Strings(suffixes)
	for _, s := range suffixes {
		names := make([]string, 0, len(p[s]))
		for n := range p[s] {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if err := fn(s, n, p[s][n]); err != nil {
				return err
			}
		}
	}
	return nil
}

// relSuffix returns ds relative to root.
func relSuffix(root, ds string) (string, error) {
	if ds == root {
		return "", nil
	}
	if strings.HasPrefix(ds, root+"/") {
		return ds[len(root):], nil
	}
	return "", app.Fail("dataset is outside expected root: %s", ds)
}

// loadProps reads the locally set properties of a dataset tree, ignoring
// volsize, canmount, and the top dataset's readonly, which failover manages.
func (r *runner) loadProps(ep endpoint.Endpoint) (props, error) {
	p := props{}
	c := r.zfs(ep, append(command.Words("get", "-Hpr", "-s", "local", "-t", "filesystem,volume", "-o", "name,property,value", "all"), command.Name(ep.DS))...)
	for _, row := range r.rows(c) {
		if len(row) < 3 {
			continue
		}
		ds, prop, val := row[0], row[1], strings.Join(row[2:], "\t")
		if prop == "volsize" || prop == "canmount" {
			continue
		}
		suffix, err := relSuffix(ep.DS, ds)
		if err != nil {
			return nil, err
		}
		if prop == "readonly" && suffix == "" {
			continue
		}
		p.set(suffix, prop, val)
	}
	return p, nil
}

// syncProps sets the source's local properties on the target and inherits
// local properties that only the target has.
func (r *runner) syncProps(tgt endpoint.Endpoint, src, dst props) error {
	err := src.each(func(suffix, prop, val string) error {
		c := r.zfs(tgt, command.Word("set"), command.LitName(prop+"=", val), command.Name(tgt.DS+suffix))
		return r.must(c, 1, "command failed")
	})
	if err != nil {
		return err
	}
	return dst.each(func(suffix, prop, _ string) error {
		if src.has(suffix, prop) {
			return nil
		}
		return r.must(r.zfs(tgt, command.Word("inherit"), command.Word(prop), command.Name(tgt.DS+suffix)), 1, "command failed")
	})
}

// finalBackup replicates the locked source to the target in-process with a
// fresh snapshot.
func (r *runner) finalBackup(src, tgt endpoint.Endpoint) error {
	level := r.rt.Opts.Get("LOG_LEVEL")
	display := fmt.Sprintf("zelta ipc-run backup --snapshot --log-mode=text --log-level=%s %s %s",
		level, shellwords.SingleQuote(src.ID), shellwords.SingleQuote(tgt.ID))
	if r.dry() {
		r.rt.Log.Noticef("+ %s", display)
		return nil
	}
	r.rt.Log.Infof("+ %s", display)
	failed := app.Exit(codeBackup, "final backup failed after source lock: %s", display)
	if Backup == nil {
		return failed
	}
	opts := r.rt.Opts.Clone()
	opts.Set("VERB", "backup", config.FromCLI)
	opts.Set("SNAP_MODE", "ALWAYS", config.FromCLI)
	opts.Set("LOG_MODE", "text", config.FromCLI)
	brt, err := app.Build(r.rt, opts)
	if err != nil {
		return failed
	}
	if app.Finish(brt, Backup(r.ctx, brt, []string{src.ID, tgt.ID})) != 0 {
		return failed
	}
	return nil
}

func requireOne(rt *app.Runtime, args []string) error {
	if len(args) < 1 {
		return app.Fail("zelta %s requires at least one dataset", rt.Opts.Get("VERB"))
	}
	return nil
}

func requireTwo(rt *app.Runtime, args []string) error {
	if len(args) != 2 {
		return app.Fail("zelta %s requires SOURCE and TARGET", rt.Opts.Get("VERB"))
	}
	return nil
}

// Lock implements "zelta lock ENDPOINT...".
func Lock(ctx context.Context, rt *app.Runtime, args []string) error {
	if err := requireOne(rt, args); err != nil {
		return err
	}
	r := newRunner(ctx, rt)
	for _, a := range args {
		if err := r.lock(rt.Endpoint(a)); err != nil {
			return err
		}
	}
	return r.result(nil)
}

// Unlock implements "zelta unlock ENDPOINT...".
func Unlock(ctx context.Context, rt *app.Runtime, args []string) error {
	if err := requireOne(rt, args); err != nil {
		return err
	}
	r := newRunner(ctx, rt)
	for _, a := range args {
		if err := r.unlock(rt.Endpoint(a)); err != nil {
			return err
		}
	}
	return r.result(nil)
}

// Propsync implements "zelta propsync SOURCE TARGET".
func Propsync(ctx context.Context, rt *app.Runtime, args []string) error {
	if err := requireTwo(rt, args); err != nil {
		return err
	}
	r := newRunner(ctx, rt)
	src, tgt := rt.Endpoint(args[0]), rt.Endpoint(args[1])
	sp, err := r.loadProps(src)
	if err != nil {
		return err
	}
	tp, err := r.loadProps(tgt)
	if err != nil {
		return err
	}
	return r.result(r.syncProps(tgt, sp, tp))
}

// Failover implements "zelta failover SOURCE TARGET": lock the source, run a
// final backup, replay local properties, and unlock the target.
func Failover(ctx context.Context, rt *app.Runtime, args []string) error {
	if err := requireTwo(rt, args); err != nil {
		return err
	}
	r := newRunner(ctx, rt)
	src, tgt := rt.Endpoint(args[0]), rt.Endpoint(args[1])
	sp, tp := props{}, props{}
	if !r.dry() {
		var err error
		if sp, err = r.loadProps(src); err != nil {
			return err
		}
		if tp, err = r.loadProps(tgt); err != nil {
			return err
		}
	}
	steps := []func() error{
		func() error { return r.lock(src) },
		func() error { return r.finalBackup(src, tgt) },
		func() error { return r.syncProps(tgt, sp, tp) },
		func() error { return r.unlock(tgt) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return r.result(nil)
}
