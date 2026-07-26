package match

import (
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

type objType int

const (
	objUnknown objType = iota
	objDataset
	objSnapshot
	objBookmark
)

// Snap is one snapshot or bookmark under a dataset.
type Snap struct {
	Savepoint string // includes @ or #
	GUID      string
	Written   string
	Type      objType
	Name      string
}

// Dataset holds one dataset node and its savepoints (newest-first).
type Dataset struct {
	Suffix             string
	Name               string
	GUID               string
	Origin             string
	Written            string
	SnapshotsChanged   string
	ReceiveResumeToken string
	Type               string // filesystem | volume (from zfs list type; empty if not listed)
	Snaps              []Snap
}

// tree is datasets keyed by ds_suffix, plus discovery order.
type tree struct {
	root    string
	bySuf   map[string]*Dataset
	order   []string
	guidIdx map[string]map[string]int // suffix → guid → snap index
}

func buildTree(root string, rows []zfs.ListRow, filt *Filter, sourceSide, preserveSourceSnapshots bool) (*tree, error) {
	t := &tree{
		root:    root,
		bySuf:   make(map[string]*Dataset),
		guidIdx: make(map[string]map[string]int),
	}
	for _, row := range rows {
		if err := t.addRow(row, filt, sourceSide, preserveSourceSnapshots); err != nil {
			return nil, err
		}
	}
	return t, nil
}

func (t *tree) addRow(row zfs.ListRow, filt *Filter, sourceSide, preserveSourceSnapshots bool) error {
	name := row.Name
	base, savepoint, typ := splitName(name)
	suf, err := endpoint.DSSuffix(t.root, base)
	if err != nil {
		return err
	}

	// DS include/exclude apply to dataset rows only (oracle process_row).
	// Source snaps still load under an excluded DS (exact path) so tgt-only
	// pairs can show src_last; pairs only emit when Dataset.Name is set.
	if typ == objDataset {
		if !filt.keepDataset(name, suf) {
			return nil
		}
	} else if typ == objSnapshot && sourceSide {
		if !preserveSourceSnapshots && !filt.keepSourceSnap(savepoint, base, suf) {
			return nil
		}
	}

	guid := row.Props["guid"]
	written := row.Props["written"]

	ds := t.bySuf[suf]
	if ds == nil {
		ds = &Dataset{Suffix: suf}
		t.bySuf[suf] = ds
		t.order = append(t.order, suf)
		t.guidIdx[suf] = make(map[string]int)
	}

	if typ == objDataset {
		ds.Name = name
		ds.GUID = guid
		ds.Origin = row.Props["origin"]
		ds.Written = written
		ds.SnapshotsChanged = row.Props["snapshots_changed"]
		ds.ReceiveResumeToken = row.Props["receive_resume_token"]
		ds.Type = row.Props["type"]
		return nil
	}

	sp := Snap{
		Savepoint: savepoint,
		GUID:      guid,
		Written:   written,
		Type:      typ,
		Name:      name,
	}
	idx := len(ds.Snaps)
	ds.Snaps = append(ds.Snaps, sp)
	// Snapshots win over bookmarks for GUID lookup (oracle: prefer snapshot).
	if prev, ok := t.guidIdx[suf][guid]; !ok || typ == objSnapshot {
		_ = prev
		t.guidIdx[suf][guid] = idx
	}
	return nil
}

func splitName(name string) (base, savepoint string, typ objType) {
	if i := strings.IndexAny(name, "@#"); i >= 0 {
		base = name[:i]
		savepoint = name[i:]
		switch savepoint[0] {
		case '@':
			typ = objSnapshot
		case '#':
			typ = objBookmark
		default:
			typ = objUnknown
		}
		return
	}
	return name, "", objDataset
}

func (t *tree) get(suf string) *Dataset {
	return t.bySuf[suf]
}

func (t *tree) snapByGUID(suf, guid string) (Snap, bool) {
	m := t.guidIdx[suf]
	if m == nil {
		return Snap{}, false
	}
	idx, ok := m[guid]
	if !ok {
		return Snap{}, false
	}
	ds := t.bySuf[suf]
	if ds == nil || idx < 0 || idx >= len(ds.Snaps) {
		return Snap{}, false
	}
	return ds.Snaps[idx], true
}
