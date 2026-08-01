package match

import "git.belltower.it/djbell/zelta-go/zfs"

// SnapListOpts controls optional columns on the expensive snapshot/bookmark list.
// Dataset-only fields (encryption, type, origin, snapshots_changed, resume token)
// must never appear here — they come from DatasetContext (zfs get all).
type SnapListOpts struct {
	Written bool // written,creation,used on snaps (xfer sizing)
	IVSet   bool // request ivsetguid when features allow (snap property)
}

// SnapListProps builds zfs list -o columns for snapshot/bookmark compare only.
// Keep this list small: thousands of snaps × wide props exhausts memory.
func SnapListProps(feat zfs.Features, opts SnapListOpts) []string {
	props := []string{"name", "guid"}
	if opts.Written {
		props = append(props, "written", "creation", "used")
	}
	if opts.IVSet && feat.IVSetGUID {
		props = append(props, "ivsetguid")
	}
	return props
}

// colsNeedIVSet reports whether match output columns require ivsetguid listing.
func colsNeedIVSet(cols []string) bool {
	for _, c := range cols {
		switch c {
		case "match_ivset", "matchivset", "ivset", "ivsetguid":
			return true
		}
	}
	return false
}

// ApplyDatasetContext merges filesystem/volume get props into match pairs.
// tgt may be Exists=false with root encryption filled from a parent (missing target).
func ApplyDatasetContext(pairs []*Pair, src, tgt *zfs.DatasetContext) {
	for _, p := range pairs {
		if src != nil {
			applySideProps(p, src, true)
		}
		if tgt != nil {
			applySideProps(p, tgt, false)
		}
	}
	// Resume token / written can change info classification.
	setInfo(pairs)
}

func applySideProps(p *Pair, dc *zfs.DatasetContext, source bool) {
	if dc == nil {
		return
	}
	m := dc.BySuffix[p.DSSuffix]
	if m == nil && !source {
		// Missing target dataset (or parent-only context): inherit encryption.
		if v := dc.AncestorProp(p.DSSuffix, "encryption"); v != "" {
			p.TgtEncryption = v
		}
		// Also try root "" when context was filled from a parent of a missing target.
		if p.TgtEncryption == "" {
			if v := dc.Prop("", "encryption"); v != "" {
				p.TgtEncryption = v
			}
		}
		return
	}
	if m == nil {
		return
	}
	if source {
		if v := m["written"]; v != "" {
			p.SrcWritten = v
		}
		if v := m["encryption"]; v != "" {
			p.SrcEncryption = v
		}
		if v := m["type"]; v != "" {
			p.SrcType = v
		}
		if v := m["origin"]; v != "" && v != "-" {
			p.SrcOrigin = v
		}
		if v := m["snapshots_changed"]; v != "" {
			p.SrcSnapshotsChanged = v
		}
		return
	}
	if v := m["written"]; v != "" {
		p.TgtWritten = v
	}
	if v := m["encryption"]; v != "" {
		p.TgtEncryption = v
	}
	if v := m["type"]; v != "" {
		p.TgtType = v
	}
	if v := m["origin"]; v != "" && v != "-" {
		p.TgtOrigin = v
	}
	if v := m["snapshots_changed"]; v != "" {
		p.TgtSnapshotsChanged = v
	}
	if v := m["receive_resume_token"]; v != "" && v != "-" {
		p.TgtReceiveResumeToken = v
	}
}
