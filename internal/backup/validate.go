package backup

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/logging"
	"github.com/bell-tower/zelta-go/internal/units"
	"github.com/bell-tower/zelta-go/internal/zfs"
)

// validateDatasets checks the operands and loads both endpoints.
func (r *run) validateDatasets() error {
	n := len(r.args)
	srcOnly := r.verb == "revert"
	cloners := r.verb == "rotate" || r.verb == "clone"
	switch {
	case n == 0:
		return app.Usage("no endpoints given")
	case n > 2 && !(r.verb == "clone" && n == 4):
		return app.Usage("too many operands: %s", r.args[2])
	case n == 1 && !srcOnly:
		if cloners {
			r.rt.Log.Errorf("did you mean 'zelta revert '%s' ?", r.src.ID)
		}
		return app.Usage("missing target endpoint argument")
	case n != 1 && srcOnly:
		r.rt.Log.Errorf("did you mean 'zelta rotate' %s %s ?", r.src.ID, r.tgt.ID)
		return app.Usage("'%s' does not take a target argument", r.verb)
	}
	if err := r.validateSourceDataset(); err != nil {
		return err
	}
	if !srcOnly {
		if err := r.validateTargetDataset(); err != nil {
			return err
		}
	}
	if pipe := r.opts.Get("RECV_PIPE"); pipe != "" && !(r.src.IsRemote() && r.tgt.IsRemote()) {
		r.receivePipe = strings.TrimRight(pipe, "| ")
	}
	return nil
}

func (r *run) validateSourceDataset() error {
	if r.verb == "clone" && r.tgt.ID != "" && r.tree.sourcePool != r.tree.targetPool {
		return fail("cannot clone: target pool doesn't match source")
	}
	ok, err := r.loadProperties(src)
	if err != nil {
		return err
	}
	if !ok {
		return fail("source dataset '%s' does not exist", r.src.ID)
	}
	return nil
}

func (r *run) validateTargetDataset() error {
	if r.memory != nil {
		r.recallTarget()
		return nil
	}
	ok, err := r.loadProperties(tgt)
	if err != nil {
		return err
	}
	if ok {
		r.tree.targetExists = true
		if r.verb == "clone" {
			return fail("cannot clone: target dataset '%s' already exists", r.tgt.ID)
		}
	} else if r.verb == "rotate" {
		return fail("cannot 'rotate': target dataset '%s' does not exist", r.tgt.ID)
	}
	return r.validateTargetParent()
}

// plannedProperties are the properties the planner reads. Asking for these
// instead of "all" is several times faster on large trees.
const plannedProperties = "type,written,encryption,origin,receive_resume_token,snapshots_changed"

// propsCommand lists local and inherent properties of a tree: the planned
// properties, or every property when props is "all".
func (r *run) propsCommand(s side, props string) command.Cmd {
	ep := r.endpoint(s)
	args := command.Words("get", "-Hpr", "-s", "local,none", "-t", "filesystem,volume", "-o", "name,property,value")
	if r.depth > 0 {
		args = append(args, command.Word("-d"+strconv.Itoa(r.depth-1)))
	}
	args = append(args, command.Word(props), command.Name(ep.DS))
	return zfs.Command(r.rt, ep, zfs.RemoteDefault, args...)
}

// unknownProperty reports whether zfs rejected a property name, as releases
// before OpenZFS 2.2 do for snapshots_changed.
func unknownProperty(res execx.Result) bool {
	for _, l := range res.Lines {
		if strings.HasPrefix(l.Text, "bad property list") {
			return true
		}
	}
	return false
}

// loadProperties reads a tree's properties and reports whether the top
// dataset exists. Connection failures are fatal.
func (r *run) loadProperties(s side) (bool, error) {
	ep := r.endpoint(s)
	r.rt.Log.Infof("checking properties for %s", ep.ID)
	res := r.rt.Run(r.ctx, r.propsCommand(s, plannedProperties))
	if unknownProperty(res) {
		res = r.rt.Run(r.ctx, r.propsCommand(s, "all"))
	}
	for _, l := range res.Lines {
		line := app.SanitizeLine(l.Text)
		f := zfs.Fields(line)
		if len(f) == 3 && strings.HasPrefix(f[0], ep.DS) {
			suffix := f[0][len(ep.DS):]
			val := f[2]
			if val == "-" && strings.Contains(plannedProperties, f[1]) {
				// Named properties print "-" when unset; "all" omits them.
				r.dataset(s, suffix).exists = true
				continue
			}
			if val == "off" {
				val = "0"
			}
			d := r.dataset(s, suffix)
			d.props[f[1]] = val
			d.exists = true
			if s == src {
				r.sourceProperty(f[1], val)
			}
			continue
		}
		if zfs.DoesNotExist(line) {
			return false, nil
		}
		if zfs.Feedback(r.rt.Log, line) == logging.Error {
			return false, &app.ExitError{Code: 1}
		}
	}
	return r.ds[s][""] != nil && r.ds[s][""].exists, nil
}

// sourceProperty tracks properties that drive the snapshot decision.
func (r *run) sourceProperty(key, val string) {
	switch key {
	case "encryption":
		if val != "0" {
			r.tree.sourceEncrypted = true
		}
	case "written":
		if n := units.Atoi(val); n != 0 {
			r.sum.sourceWritten += int64(n)
			r.tree.snapshotNeeded = snapWritten
		}
	case "snapshots_changed":
		if val == "-" || val == "" {
			r.tree.snapshotsChangedMissing = true
		} else if n := int64(units.Atof(val)); r.tree.snapshotsChanged == 0 || n < r.tree.snapshotsChanged {
			r.tree.snapshotsChanged = n
		}
	}
}

// datasetExists checks for one dataset with "zfs list".
func (r *run) datasetExists(s side, name string) bool {
	ep := r.endpoint(s)
	r.rt.Log.Infof("checking for existence of %s dataset: %s", s, name)
	res := r.rt.Run(r.ctx, zfs.Command(r.rt, ep, zfs.RemoteDefault, command.Word("list"), command.Word("-Ho"), command.Word("name"), command.Name(name)))
	found := false
	for _, l := range res.Lines {
		if app.SanitizeLine(l.Text) != name {
			return false
		}
		found = true
	}
	return found
}

func parentOf(ds string) string {
	if i := strings.LastIndexByte(ds, '/'); i >= 0 {
		return ds[:i]
	}
	return ds
}

var propertyInfoRe = regexp.MustCompile(`^[[:space:]].*=`)

// validateTargetParent ensures the target's parent exists, creating it with
// "zfs create -p" when CREATE_PARENT is set. "zfs create -p" below a
// read-only dataset only creates one level per attempt, so it is retried.
func (r *run) validateTargetParent() error {
	if r.tree.targetExists {
		return nil
	}
	parent := parentOf(r.tgt.DS)
	if !r.opts.Bool("CREATE_PARENT") {
		if !r.datasetExists(tgt, parent) {
			return fail("target has no parent dataset: '%s'", parent)
		}
		return nil
	}
	cmd := zfs.Command(r.rt, r.tgt, zfs.RemoteDefault, command.Word("create"), command.Word("-vupo"), command.Word("canmount=noauto"), command.Name(parent))
	r.rt.Log.Infof("validating target parent dataset: '%s'", parent)
	attempts := strings.Count(parent, "/")
	if attempts > 0 && r.dryRun() {
		if !r.datasetExists(tgt, parent) {
			r.rt.Log.Noticef("+ %s", cmd.String())
		}
		return nil
	}
	for ; attempts > 0; attempts-- {
		readOnly := false
		res := r.rt.Run(r.ctx, cmd)
		for _, l := range res.Lines {
			line := app.SanitizeLine(l.Text)
			switch {
			case line == "":
			case strings.Contains(line, "Read-only file system"):
				r.rt.Log.Debugf("`zfs create` read-only bug detected")
				readOnly = true
			case strings.Contains(line, "create ancestors"):
				r.rt.Log.Debugf("attempted to create ancestor(s)")
			case strings.HasPrefix(line, "create "+parent):
			case propertyInfoRe.MatchString(line):
				r.rt.Log.Debugf("property info: '%s'", line)
			case strings.Contains(line, "permission denied"):
				return fail("permission denied creating target '%s'", parent)
			case strings.Contains(line, "no such pool"):
				return fail("no such pool in target path: '%s'", parent)
			default:
				r.rt.Log.Warnf("unexpected `zfs create` output: '%s'", line)
			}
		}
		if !readOnly {
			break
		}
		r.rt.Log.Infof("incomplete `zfs create`; retrying")
	}
	return nil
}
