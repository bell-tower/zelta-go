package match

import (
	"sort"
	"strconv"
	"strings"
)

type pairStatus int

const (
	pairUnknown pairStatus = iota
	pairExists
	pairSrcOnly
	pairTgtOnly
)

// Pair is one source/target dataset comparison row.
type Pair struct {
	DSSuffix              string   `json:"ds_suffix,omitempty"`
	Match                 string   `json:"match,omitempty"`
	MatchIVSet            string   `json:"match_ivset,omitempty"`
	NumMatches            int      `json:"num_matches,omitempty"`
	SrcName               string   `json:"src_name,omitempty"`
	TgtName               string   `json:"tgt_name,omitempty"`
	SrcOrigin             string   `json:"src_origin,omitempty"`
	TgtOrigin             string   `json:"tgt_origin,omitempty"`
	SrcFirst              string   `json:"src_first,omitempty"`
	TgtFirst              string   `json:"tgt_first,omitempty"`
	SrcLast               string   `json:"src_last,omitempty"`
	TgtLast               string   `json:"tgt_last,omitempty"`
	SrcNext               string   `json:"src_next,omitempty"`
	TgtNext               string   `json:"tgt_next,omitempty"`
	SrcWritten            string   `json:"src_written,omitempty"`
	SrcEncryption         string   `json:"src_encryption,omitempty"`
	TgtEncryption         string   `json:"tgt_encryption,omitempty"`
	TgtWritten            string   `json:"tgt_written,omitempty"`
	SrcSnapshotsChanged   string   `json:"src_snapshots_changed,omitempty"`
	TgtSnapshotsChanged   string   `json:"tgt_snapshots_changed,omitempty"`
	TgtReceiveResumeToken string   `json:"tgt_receive_resume_token,omitempty"`
	SrcType               string   `json:"src_type,omitempty"` // filesystem | volume
	TgtType               string   `json:"tgt_type,omitempty"`
	SrcSnaps              int      `json:"src_snaps,omitempty"`
	SrcSavepoints         []string `json:"src_savepoints,omitempty"`
	TgtSnaps              int      `json:"tgt_snaps,omitempty"`
	XferNum               int      `json:"xfer_num,omitempty"`
	XferSize              int64    `json:"xfer_size,omitempty"`
	NumBlocked            int      `json:"num_blocked,omitempty"`
	Info                  string   `json:"info,omitempty"`
	status                pairStatus
}

func pairTrees(src, tgt *tree) []*Pair {
	seen := make(map[string]*Pair)
	var list []*Pair

	add := func(suf string) *Pair {
		if p, ok := seen[suf]; ok {
			return p
		}
		p := &Pair{DSSuffix: suf}
		seen[suf] = p
		list = append(list, p)
		return p
	}

	for _, suf := range src.order {
		sds := src.get(suf)
		if sds == nil || sds.Name == "" {
			continue
		}
		p := add(suf)
		fillEnds(p, sds, tgt.get(suf))
		tds := tgt.get(suf)
		if tds != nil && tds.Name != "" {
			p.status = pairExists
		} else {
			p.status = pairSrcOnly
		}
		compareSnaps(p, sds, tgt)
	}

	for _, suf := range tgt.order {
		tds := tgt.get(suf)
		if tds == nil || tds.Name == "" {
			continue
		}
		p := seen[suf]
		if p == nil {
			p = add(suf)
			fillEnds(p, src.get(suf), tds)
			p.status = pairTgtOnly
		}
		reviewTarget(p, tds, src)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].DSSuffix < list[j].DSSuffix
	})
	setInfo(list)
	return list
}

func fillEnds(p *Pair, sds, tds *Dataset) {
	if sds != nil {
		p.SrcName = sds.Name
		p.SrcOrigin = sds.Origin
		p.SrcWritten = sds.Written
		p.SrcEncryption = sds.Encryption
		p.SrcSnapshotsChanged = sds.SnapshotsChanged
		p.SrcType = sds.Type
		p.SrcSnaps = len(sds.Snaps)
		for _, sp := range sds.Snaps {
			if sp.Type == objSnapshot {
				p.SrcSavepoints = append(p.SrcSavepoints, sp.Savepoint)
			}
		}
		if n := len(sds.Snaps); n > 0 {
			p.SrcLast = sds.Snaps[0].Savepoint
			p.SrcFirst = sds.Snaps[n-1].Savepoint
		}
	}
	if tds != nil {
		p.TgtName = tds.Name
		p.TgtOrigin = tds.Origin
		p.TgtWritten = tds.Written
		p.TgtEncryption = tds.Encryption
		p.TgtSnapshotsChanged = tds.SnapshotsChanged
		if tds.ReceiveResumeToken != "-" {
			p.TgtReceiveResumeToken = tds.ReceiveResumeToken
		}
		p.TgtType = tds.Type
		p.TgtSnaps = len(tds.Snaps)
		if n := len(tds.Snaps); n > 0 {
			p.TgtLast = tds.Snaps[0].Savepoint
			p.TgtFirst = tds.Snaps[n-1].Savepoint
		}
	}
}

func compareSnaps(p *Pair, sds *Dataset, tgt *tree) {
	for _, sp := range sds.Snaps {
		if sp.Type != objSnapshot && sp.Type != objBookmark {
			continue
		}
		tgSnap, ok := tgt.snapByGUID(p.DSSuffix, sp.GUID)
		if ok && tgSnap.Type == objSnapshot {
			if p.NumMatches == 0 {
				p.Match = sp.Savepoint
				if sp.IVSetGUID != "" && sp.IVSetGUID != "-" && sp.IVSetGUID == tgSnap.IVSetGUID {
					p.MatchIVSet = sp.IVSetGUID
				}
			}
			p.NumMatches++
			continue
		}
		if p.Match == "" {
			p.SrcNext = sp.Savepoint
			p.XferNum++
			p.XferSize += parseBytes(sp.Written)
		}
	}
}

func reviewTarget(p *Pair, tds *Dataset, src *tree) {
	matchFound := false
	for _, sp := range tds.Snaps {
		if _, ok := src.snapByGUID(p.DSSuffix, sp.GUID); ok {
			matchFound = true
		}
		if !matchFound && sp.Type == objSnapshot {
			p.NumBlocked++
			p.TgtNext = sp.Savepoint
		}
	}
}

func setInfo(pairs []*Pair) {
	for _, p := range pairs {
		switch p.status {
		case pairTgtOnly:
			p.Info = "no source (target only)"
			continue
		case pairSrcOnly:
			p.Info = "syncable (full)"
			continue
		}
		if p.TgtReceiveResumeToken != "" && p.TgtReceiveResumeToken != "-" {
			p.Info = "syncable (resume)"
			continue
		}

		var reason string
		if p.TgtSnaps == 0 && p.SrcSnaps > 0 {
			reason = "no target snapshots"
		} else if p.Match != p.TgtLast {
			reason = "target diverged"
		} else if isTruthyWritten(p.TgtWritten) {
			reason = "target is written"
		}
		if reason != "" {
			p.Info = "blocked sync: " + reason
			continue
		}
		if p.Match == p.SrcLast {
			p.Info = "up-to-date"
		} else if p.Match == p.TgtLast {
			p.Info = "syncable (incremental)"
		}
	}
}

func isTruthyWritten(w string) bool {
	if w == "" || w == "-" {
		return false
	}
	n, err := strconv.ParseInt(w, 10, 64)
	if err == nil {
		return n != 0
	}
	return w != "0"
}

// parseBytes accepts plain integers or zfs-style human sizes (K/M/G/T/P, optional iB).
func parseBytes(s string) int64 {
	if s == "" || s == "-" {
		return 0
	}
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	mult := int64(1)
	end := len(s)
	for end > 0 {
		c := s[end-1]
		if c >= '0' && c <= '9' || c == '.' {
			break
		}
		end--
	}
	if end == 0 || end == len(s) {
		return 0
	}
	unit := strings.ToUpper(s[end:])
	unit = strings.TrimSuffix(unit, "B")
	unit = strings.TrimSuffix(unit, "I")
	switch unit {
	case "K":
		mult = 1024
	case "M":
		mult = 1024 * 1024
	case "G":
		mult = 1024 * 1024 * 1024
	case "T":
		mult = 1024 * 1024 * 1024 * 1024
	case "P":
		mult = 1024 * 1024 * 1024 * 1024 * 1024
	case "":
		// bare number already handled
	default:
		return 0
	}
	num := s[:end]
	if i, err := strconv.ParseInt(num, 10, 64); err == nil {
		return i * mult
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	return int64(f * float64(mult))
}
