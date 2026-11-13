package cli

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/policy"
)

// policyEditing reports whether "zelta policy" was asked to change jobs
// instead of running them.
func policyEditing(opts *config.Options) bool {
	return opts.Get("POLICY_SAVE") != "" || opts.Get("POLICY_RM") != "" || opts.Get("POLICY_IMPORT") != ""
}

// storePath returns the policy store to change: CONFIG when it is a
// store, or a new store beside the default zelta.conf.
func storePath(rt *app.Runtime, loader *config.Loader) (string, error) {
	cfg := rt.Opts.Get("CONFIG")
	if policy.IsStore(cfg) {
		return cfg, nil
	}
	if rt.Opts.Source("CONFIG") > config.FromDefault {
		return "", app.Fail("cannot change '%s': jobs can only be saved in a %s policy", cfg, filepath.Ext(policy.StoreName))
	}
	path := filepath.Join(loader.Paths().Etc, policy.StoreName)
	if jobs, err := policy.Import(cfg); err == nil && len(jobs) > 0 {
		rt.Log.Warnf("%s will be used instead of %s; to keep its jobs, run: zelta policy --import %s", path, cfg, cfg)
	}
	return path, nil
}

// policyEdit runs "zelta policy --save, --rm, or --import".
func (a *App) policyEdit(rt *app.Runtime, loader *config.Loader) error {
	path, err := storePath(rt, loader)
	if err != nil {
		return err
	}
	store, err := policy.LoadStore(path)
	if err != nil {
		return app.Fail("%v", err)
	}
	switch {
	case rt.Opts.Get("POLICY_SAVE") != "":
		err = a.policySave(rt, store, rt.Opts.Get("POLICY_SAVE"))
	case rt.Opts.Get("POLICY_RM") != "":
		err = a.policyRemove(rt, store, rt.Opts.Get("POLICY_RM"))
	default:
		err = policyImport(rt, store, rt.Opts.Get("POLICY_IMPORT"))
	}
	if err != nil {
		return err
	}
	if rt.DryRun() {
		return nil
	}
	if err := store.Save(path); err != nil {
		return app.Fail("cannot save policy '%s': %v", path, err)
	}
	return nil
}

func (a *App) policySave(rt *app.Runtime, store *policy.Store, pattern string) error {
	var found []replayEntry
	for _, e := range a.replayList() {
		if strings.Contains(e.label(), pattern) {
			found = append(found, e)
		}
	}
	if len(found) == 0 {
		return app.Fail("no backup matches '%s'", pattern)
	}
	e, ok := a.pickReplay(found)
	if !ok {
		return &app.ExitError{Code: 1}
	}
	job := policy.StoreJob{Source: e.Source, Target: e.Target, Options: map[string]string{}}
	for k, v := range e.Options {
		if policy.JobOption(k) {
			job.Options[k] = v
		}
	}
	switch e.Args[0] {
	case "sync":
		if _, ok := job.Options["SEND_INTR"]; !ok {
			job.Options["SEND_INTR"] = "0"
		}
	case "backup":
		if e.Options["VERB"] != "" && e.Options["VERB"] != "backup" {
			return app.Fail("cannot save '%s': policy jobs run 'zelta backup'", e.Options["VERB"])
		}
	default:
		return app.Fail("cannot save '%s': policy jobs run 'zelta backup'", e.Args[0])
	}
	if len(job.Options) == 0 {
		job.Options = nil
	}
	var old map[string]string
	if i := slices.IndexFunc(store.Jobs, func(j policy.StoreJob) bool { return j.Label() == job.Label() }); i >= 0 {
		job.Group, old = store.Jobs[i].Group, store.Jobs[i].Options
		rt.Log.Noticef("updated: %s", job.Label())
	} else {
		rt.Log.Noticef("saved: %s", job.Label())
	}
	store.Put(job)
	logChanges(rt, old, job.Options)
	return nil
}

// logChanges prints the options that differ between two jobs.
func logChanges(rt *app.Runtime, old, cur map[string]string) {
	keys := slices.Collect(maps.Keys(old))
	for k := range cur {
		if _, ok := old[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		ov, inOld := old[k]
		nv, inNew := cur[k]
		switch {
		case inOld && !inNew:
			rt.Log.Noticef("  - %s=%s", k, ov)
		case !inOld && inNew:
			rt.Log.Noticef("  + %s=%s", k, nv)
		case ov != nv:
			rt.Log.Noticef("  - %s=%s", k, ov)
			rt.Log.Noticef("  + %s=%s", k, nv)
		}
	}
}

func (a *App) policyRemove(rt *app.Runtime, store *policy.Store, pattern string) error {
	found := store.Find(pattern)
	if len(found) == 0 {
		return app.Fail("no job matches '%s'", pattern)
	}
	labels := make([]string, len(found))
	for i, j := range found {
		labels[i] = j.Label()
	}
	i, ok := a.choose(labels)
	if !ok {
		return &app.ExitError{Code: 1}
	}
	store.Remove(labels[i])
	rt.Log.Noticef("removed: %s", labels[i])
	return nil
}

func policyImport(rt *app.Runtime, store *policy.Store, file string) error {
	jobs, err := policy.Import(file)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if store.Put(j) != nil {
			rt.Log.Noticef("updated: %s", j.Label())
		} else {
			rt.Log.Noticef("imported: %s", j.Label())
		}
	}
	rt.Log.Noticef("%d jobs imported from %s", len(jobs), file)
	return nil
}
