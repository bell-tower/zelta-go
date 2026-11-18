package main

import (
	"strings"

	"zelta.space/zelta"
)

// field is one option the planner tunes.
type field struct {
	label, flag, key, hint string
	val                    string
	presets                []string
	cycleOnly              bool
}

const (
	fNum = iota
	fTime
	fGrid
	fSize
	fGuard
	fDepth
	fExclude
	fInclude
)

func newFields(guard string) []field {
	fs := []field{
		fNum:     {label: "keep newest", flag: "--prune-num", key: "PRUNE_NUM", hint: "N snapshots older than the match", presets: []string{"", "0", "7", "14", "30", "60", "90", "365"}},
		fTime:    {label: "keep newer than", flag: "--prune-time", key: "PRUNE_TIME", hint: "e.g. 7d, 2w, 3mo, 1y", presets: []string{"", "1d", "7d", "14d", "30d", "90d", "1y"}},
		fGrid:    {label: "GFS grid", flag: "--prune-grid", key: "PRUNE_GRID", hint: "e.g. 30x1 day, 52x1 week, 1 year", presets: []string{"", "24x1 hour, 30x1 day", "7x1 day, 4x1 week, 12x1 month", "30x1 day, 52x1 week, 1 year", "1 day"}},
		fSize:    {label: "reclaim at least", flag: "--prune-size", key: "PRUNE_SIZE", hint: "oldest first until this is freed", presets: []string{"", "10M", "100M", "1G", "10G", "100G"}},
		fGuard:   {label: "guard", flag: "--prune-guard", key: "PRUNE_GUARD", presets: []string{"latest", "unsynced", "none"}, val: "latest", cycleOnly: true},
		fDepth:   {label: "depth", flag: "--depth", key: "DEPTH", hint: "1 is the endpoint only", presets: []string{"", "1", "2", "3"}},
		fExclude: {label: "exclude", flag: "--exclude", key: "EXCLUDE", hint: "dataset or @snapshot patterns, comma-separated"},
		fInclude: {label: "include", flag: "--include", key: "INCLUDE", hint: "dataset or @snapshot patterns, comma-separated"},
	}
	if guard == "" {
		fs[fGuard].presets, fs[fGuard].val = []string{"none"}, "none"
		fs[fGuard].hint = "no guard endpoint given"
	}
	return fs
}

// retentionDefault reports whether no retention option is set, so Zelta
// keeps the newest 30 snapshots and those from the last 30 days.
func retentionDefault(fs []field) bool {
	for _, f := range fs[:fGuard] {
		if strings.TrimSpace(f.val) != "" {
			return false
		}
	}
	return true
}

// options passes the fields to Zelta as typed, so Zelta parses and
// validates them exactly as zelta prune would.
func options(fs []field, guard string) *zelta.PruneOptions {
	s := map[string]string{}
	for _, f := range fs {
		if v := strings.TrimSpace(f.val); v != "" && f.key != "PRUNE_GUARD" {
			s[f.key] = v
		}
	}
	o := &zelta.PruneOptions{Guard: guard, Settings: s}
	if g := fs[fGuard].val; guard == "" || g != "latest" {
		o.GuardMode = zelta.GuardMode(g)
	}
	return o
}

// command is the zprune command line that destroys what the plan shows,
// after its own preview and confirmation.
func command(fs []field, endpoint, guard string) string {
	args := []string{"zprune"}
	if guard != "" {
		args = append(args, "--match-endpoint="+shellWord(guard))
	}
	for _, f := range fs {
		v := strings.TrimSpace(f.val)
		switch {
		case v == "":
		case f.key == "PRUNE_GUARD":
			if guard == "" || v == "none" {
				args = append(args, "--no-prune-guard")
			} else if v != "latest" {
				args = append(args, f.flag+"="+v)
			}
		case f.key == "EXCLUDE" || f.key == "INCLUDE":
			for p := range strings.SplitSeq(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					args = append(args, f.flag+"="+shellWord(p))
				}
			}
		default:
			args = append(args, f.flag+"="+shellWord(v))
		}
	}
	return strings.Join(append(args, shellWord(endpoint)), " ")
}

func shellWord(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+-_./:=,") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
