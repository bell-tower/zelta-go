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
	DSSuffix   string
	Match      string
	MatchIVSet string
	NumMatches int
	SrcName    string
	TgtName    string
	SrcFirst   string
	TgtFirst   string
	SrcLast    string
	TgtLast    string
	SrcNext    string
	TgtNext    string
	SrcWritten string
	TgtWritten string
	SrcType    string // filesystem | volume
	TgtType    string
	SrcSnaps   int
	TgtSnaps   int
	XferNum    int
	XferSize   int64
	NumBlocked int
	Info       string
	status     pairStatus
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
		p.SrcWritten = sds.Written
		p.SrcType = sds.Type
		p.SrcSnaps = len(sds.Snaps)
		if n := len(sds.Snaps); n > 0 {
			p.SrcLast = sds.Snaps[0].Savepoint
			p.SrcFirst = sds.Snaps[n-1].Savepoint
		}
	}
	if tds != nil {
		p.TgtName = tds.Name
		p.TgtWritten = tds.Written
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

func summaryOf(pairs []*Pair) string {
	var up, sync, blocked int
	for _, p := range pairs {
		switch {
		case p.Info == "up-to-date":
			up++
		case strings.HasPrefix(p.Info, "syncable"):
			sync++
		case strings.HasPrefix(p.Info, "blocked") || p.Info == "no source (target only)":
			blocked++
		}
	}
	var parts []string
	if up > 0 {
		parts = append(parts, strconv.Itoa(up)+" up-to-date")
	}
	if sync > 0 {
		parts = append(parts, strconv.Itoa(sync)+" syncable")
	}
	if blocked > 0 {
		parts = append(parts, strconv.Itoa(blocked)+" blocked")
	}
	return strings.Join(parts, ", ")
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
