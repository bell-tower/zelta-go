package config

import (
	"slices"
	"time"
)

// Default option values, as set by the original zelta controller script.
var defaults = []struct{ key, val string }{
	{"LOG_LEVEL", "2"},
	{"SNAP_MODE", "IF_NEEDED"},
	{"SYNC_DIRECTION", "PULL"},
	{"LIST_WRITTEN", "1"},
	{"CREATE_PARENT", "1"},
	{"RESUME", "1"},
	{"SEND_DEFAULT", "-L -c -e"},
	{"SEND_DECRYPTED", "-L -c"},
	{"SEND_RAW", "--raw"},
	{"SEND_NEW", "-p"},
	{"SEND_INTR", "1"},
	{"SEND_REPLICATE", "--raw -s -R"},
	{"RECV_TOP", "-o readonly=on"},
	{"RECV_FS", "-u -x mountpoint -o canmount=noauto"},
	{"RECV_PARTIAL", "-s"},
	{"CLONE_DEFAULT", "-po readonly=off"},
	{"REMOTE_COMMAND", "ssh"},
	{"REMOTE_MULTIPLEX", "1"},
	{"PARALLEL", "1"},
	{"CACHE", "1"},
	{"CACHE_TIMEOUT", "1day"},
}

// SnapTimeFormat is the layout of default snapshot names.
const SnapTimeFormat = "zelta_2006-01-02_15.04.05"

func knownDefault(key string) bool {
	return slices.ContainsFunc(defaults, func(d struct{ key, val string }) bool { return d.key == key })
}

// applyVerbDefaults sets values the controller forced or defaulted per verb
// before reading the command line.
func applyVerbDefaults(o *Options, verb string) {
	switch verb {
	case "clone", "rotate", "sync":
		if !o.Has("SEND_INTR") || o.Source("SEND_INTR") == FromDefault {
			o.Override("SEND_INTR", "0", FromDefault)
		}
	case "revert":
		o.Set("SEND_INTR", "0", FromEnv)
		o.Set("SNAP_MODE", "0", FromEnv)
	}
}

// applyDefaults fills unset options with built-in defaults, including values
// derived from other options.
func applyDefaults(o *Options, now time.Time) {
	for _, d := range defaults {
		if !o.Has(d.key) {
			o.Set(d.key, d.val, FromDefault)
		}
	}
	if !o.Has("SNAP_NAME") {
		o.Set("SNAP_NAME", now.UTC().Format(SnapTimeFormat), FromDefault)
	}
	if !o.Has("REMOTE_DEFAULT") {
		o.Set("REMOTE_DEFAULT", o.Get("REMOTE_COMMAND")+" -n", FromDefault)
	}
	if !o.Has("REMOTE_SEND") {
		o.Set("REMOTE_SEND", o.Get("REMOTE_DEFAULT"), FromDefault)
	}
	if !o.Has("REMOTE_RECV") {
		o.Set("REMOTE_RECV", o.Get("REMOTE_COMMAND"), FromDefault)
	}
}
