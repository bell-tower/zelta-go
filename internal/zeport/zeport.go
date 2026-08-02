package zeport

import (
	"strconv"
	"strings"
	"time"
)

// MaxAge is the staleness threshold (oracle hard-codes 24h).
const MaxAge = 86400 * time.Second

// Status is the oracle report bucket for one endpoint.
type Status string

const (
	StatusNone Status = "none" // no classifiable datasets
	StatusAll  Status = "all"  // every classifiable dataset is stale
	StatusSome Status = "some" // mix of stale and fresh
	StatusOK   Status = "ok"   // all classifiable datasets are fresh
)

// Row is one zfs list line: name, snapshots_changed, usedbysnapshots.
type Row struct {
	Name            string
	Changed         string // unix seconds, or "-"
	UsedBySnapshots string
}

// Endpoint meta for template expansion ({endpoint}, {host}, {dataset}).
type Endpoint struct {
	ID      string // raw user endpoint string
	Host    string
	Dataset string
	Leaf    string // last path component of Dataset
}

// Result is the classified outcome for one endpoint.
type Result struct {
	Endpoint Endpoint
	Status   Status
	Old      []string // relative names of stale datasets (discovery order)
	OKCount  int
	OldCount int
}

// Classify buckets list rows under root against tooOld (unix seconds).
// hasSnaps is the oracle has_snapshots probe (depth-1 snapshot list); may be nil
// (treated as "no snapshots").
func Classify(ep Endpoint, tooOld int64, rows []Row, hasSnaps func(ds string) bool) Result {
	r := Result{Endpoint: ep}
	seen := map[string]bool{}
	root := strings.TrimSuffix(ep.Dataset, "/")
	leaf := ep.Leaf
	if leaf == "" {
		leaf = leafOf(root)
	}

	for _, row := range rows {
		ds := row.Name
		if ds == "" || seen[ds] {
			continue
		}
		seen[ds] = true

		rel := relName(root, ds, leaf)
		if rel == leaf && row.Changed == "-" {
			continue
		}
		if row.Changed == "-" {
			continue
		}

		changed, err := strconv.ParseInt(row.Changed, 10, 64)
		if err != nil {
			continue
		}
		if changed < tooOld {
			if row.UsedBySnapshots == "0" && (hasSnaps == nil || !hasSnaps(ds)) {
				r.OKCount++
				continue
			}
			r.Old = append(r.Old, rel)
			r.OldCount++
			continue
		}
		r.OKCount++
	}
	r.Status = StatusOf(r.OKCount, r.OldCount)
	return r
}

// StatusOf maps counts to the oracle status string.
func StatusOf(ok, old int) Status {
	switch {
	case ok == 0 && old == 0:
		return StatusNone
	case ok == 0:
		return StatusAll
	case old > 0:
		return StatusSome
	default:
		return StatusOK
	}
}

// DefaultMessage returns the built-in template for status when env is unset.
func DefaultMessage(status Status) string {
	switch status {
	case StatusNone:
		return "{endpoint} no snapshots found"
	case StatusAll:
		return "{endpoint} all snapshots are out of date"
	case StatusSome:
		return "{endpoint} some snapshots are out of date: {dslist}"
	default:
		return "{endpoint} snapshots are up to date"
	}
}

// Lookup is REPORT_MESSAGE_* / REPORT_COMMAND_* resolution (env or defaults).
type Lookup func(key string) string

// Message builds the notice line for r using REPORT_MESSAGE_<STATUS> or
// REPORT_MESSAGE_DEFAULT, falling back to DefaultMessage.
func Message(r Result, get Lookup) string {
	if get == nil {
		get = func(string) string { return "" }
	}
	key := "REPORT_MESSAGE_" + strings.ToUpper(string(r.Status))
	msg := get(key)
	if msg == "" {
		msg = get("REPORT_MESSAGE_DEFAULT")
	}
	if msg == "" {
		msg = DefaultMessage(r.Status)
	}
	return Expand(msg, "", r)
}

// Command returns the expanded REPORT_COMMAND_* hook, or "" if unset.
func Command(r Result, message string, get Lookup) string {
	if get == nil {
		return ""
	}
	key := "REPORT_COMMAND_" + strings.ToUpper(string(r.Status))
	cmd := get(key)
	if cmd == "" {
		cmd = get("REPORT_COMMAND_DEFAULT")
	}
	if cmd == "" {
		return ""
	}
	return Expand(cmd, message, r)
}

// Expand substitutes oracle report tokens in template.
// Tokens: {message} {endpoint} {host} {dataset} {dslist} {oldcount} {okcount} {status}
func Expand(template, message string, r Result) string {
	dslist := strings.Join(r.Old, " ")
	out := template
	out = replaceAll(out, "{message}", message)
	out = replaceAll(out, "{endpoint}", r.Endpoint.ID)
	out = replaceAll(out, "{host}", r.Endpoint.Host)
	out = replaceAll(out, "{dataset}", r.Endpoint.Dataset)
	out = replaceAll(out, "{dslist}", dslist)
	out = replaceAll(out, "{oldcount}", strconv.Itoa(r.OldCount))
	out = replaceAll(out, "{okcount}", strconv.Itoa(r.OKCount))
	out = replaceAll(out, "{status}", string(r.Status))
	return out
}

// ParseListLines turns tab-separated zfs list output into Rows.
func ParseListLines(lines []string) []Row {
	var rows []Row
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		row := Row{}
		if len(parts) > 0 {
			row.Name = parts[0]
		}
		if len(parts) > 1 {
			row.Changed = parts[1]
		}
		if len(parts) > 2 {
			row.UsedBySnapshots = parts[2]
		}
		rows = append(rows, row)
	}
	return rows
}

// ListArgv is the oracle get_snapshot_ages command.
func ListArgv(dataset string) []string {
	return []string{
		"zfs", "list",
		"-t", "filesystem,volume",
		"-Hpr",
		"-o", "name,snapshots_changed,usedbysnapshots",
		"-S", "snapshots_changed",
		dataset,
	}
}

// SnapListArgv is the oracle has_snapshots probe (depth-1).
func SnapListArgv(dataset string) []string {
	return []string{
		"zfs", "list",
		"-Hro", "name",
		"-t", "snapshot",
		"-d", "1",
		dataset,
	}
}

// LeafOf returns the last path component of a dataset name.
func LeafOf(dataset string) string { return leafOf(dataset) }

func leafOf(dataset string) string {
	dataset = strings.TrimSuffix(dataset, "/")
	if i := strings.LastIndex(dataset, "/"); i >= 0 {
		return dataset[i+1:]
	}
	return dataset
}

func relName(root, full, leaf string) string {
	root = strings.TrimSuffix(root, "/")
	full = strings.TrimSuffix(full, "/")
	if full == root {
		return leaf
	}
	prefix := root + "/"
	if strings.HasPrefix(full, prefix) {
		return strings.TrimPrefix(full, prefix)
	}
	return full
}

// replaceAll is literal token replace (no $ interpretation).
func replaceAll(str, token, value string) string {
	if token == "" {
		return str
	}
	var b strings.Builder
	for {
		i := strings.Index(str, token)
		if i < 0 {
			b.WriteString(str)
			return b.String()
		}
		b.WriteString(str[:i])
		b.WriteString(value)
		str = str[i+len(token):]
	}
}
