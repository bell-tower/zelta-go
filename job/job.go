package job

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/bell-tower/zelta-go/backup"
	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/match"
	"github.com/bell-tower/zelta-go/zfs"
)

// Current document schema version.
const Version = 1

// Op names the action package an Item configures or reports.
type Op string

const (
	OpBackup Op = "backup"
	OpMatch  Op = "match"
)

// Document is a versioned list of Zelta work items (intent and/or outcomes).
type Document struct {
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}

// Item is one op-associated Request plus optional Result after a run.
// Exactly one of Backup/Match is set to match Op.
type Item struct {
	Op Op `json:"op"`

	Backup *backup.Request `json:"-"`
	Match  *match.Request  `json:"-"`

	// Results are export shapes (subset of action Results; no raw list rows).
	BackupResult *BackupResult `json:"-"`
	MatchResult  *MatchResult  `json:"-"`
}

// BackupResult is the native export of a completed backup.Run.
type BackupResult struct {
	ErrCode   backup.ErrCode `json:"err_code,omitempty"`
	Warnings  []string       `json:"warnings,omitempty"`
	Errors    []string       `json:"errors,omitempty"`
	Stats     zfs.PipeStats  `json:"stats,omitempty"`
	StartTime time.Time      `json:"start_time,omitempty"`
	EndTime   time.Time      `json:"end_time,omitempty"`
	Full  int `json:"full,omitempty"`
	Incr  int `json:"incr,omitempty"`
	Skip  int `json:"skip,omitempty"`
	Block int `json:"block,omitempty"`
}

// MatchResult is the native export of a completed match.Compare.
type MatchResult struct {
	Source      endpoint.Endpoint `json:"source,omitempty"`
	Target      endpoint.Endpoint `json:"target,omitempty"`
	Pairs       []*match.Pair     `json:"pairs,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
	SrcListTime float64           `json:"src_list_time,omitempty"`
	TgtListTime float64           `json:"tgt_list_time,omitempty"`
}

// Decode reads a JSON Document from r.
func Decode(r io.Reader) (*Document, error) {
	var doc Document
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, err
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Encode writes doc as JSON to w.
func Encode(w io.Writer, doc *Document) error {
	if doc == nil {
		return fmt.Errorf("job: nil document")
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

// Marshal returns the JSON encoding of doc.
func Marshal(doc *Document) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("job: nil document")
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// Unmarshal parses JSON into a Document.
func Unmarshal(data []byte) (*Document, error) {
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Validate checks version and per-item op/payload consistency.
func (d *Document) Validate() error {
	if d == nil {
		return fmt.Errorf("job: nil document")
	}
	if d.Version == 0 {
		return fmt.Errorf("job: version required (want %d)", Version)
	}
	if d.Version != Version {
		return fmt.Errorf("job: unsupported version %d (want %d)", d.Version, Version)
	}
	for i := range d.Items {
		if err := d.Items[i].validate(); err != nil {
			return fmt.Errorf("job: items[%d]: %w", i, err)
		}
	}
	return nil
}

func (it *Item) validate() error {
	switch it.Op {
	case OpBackup:
		if it.Backup == nil && it.BackupResult == nil {
			return fmt.Errorf("backup item needs request and/or result")
		}
		if it.Match != nil || it.MatchResult != nil {
			return fmt.Errorf("backup item must not carry match payload")
		}
	case OpMatch:
		if it.Match == nil && it.MatchResult == nil {
			return fmt.Errorf("match item needs request and/or result")
		}
		if it.Backup != nil || it.BackupResult != nil {
			return fmt.Errorf("match item must not carry backup payload")
		}
	case "":
		return fmt.Errorf("op required")
	default:
		return fmt.Errorf("unknown op %q", it.Op)
	}
	return nil
}

// FromBackup builds an Item from a backup request and optional run result.
func FromBackup(req backup.Request, res *backup.Result) Item {
	it := Item{Op: OpBackup, Backup: cloneBackupRequest(req)}
	if res != nil {
		it.BackupResult = backupResultFrom(res)
	}
	return it
}

// FromMatch builds an Item from a match request and optional compare result.
func FromMatch(req match.Request, res *match.Result) Item {
	it := Item{Op: OpMatch, Match: cloneMatchRequest(req)}
	if res != nil {
		it.MatchResult = matchResultFrom(res)
	}
	return it
}

func backupResultFrom(res *backup.Result) *BackupResult {
	out := &BackupResult{
		ErrCode:   res.ErrCode,
		Warnings:  append([]string(nil), res.Warnings...),
		Errors:    append([]string(nil), res.Errors...),
		Stats:     res.Stats,
		StartTime: res.StartTime,
		EndTime:   res.EndTime,
	}
	if res.Plan != nil {
		out.Full = res.Plan.Full
		out.Incr = res.Plan.Incr
		out.Skip = res.Plan.Skip
		out.Block = res.Plan.Block
	}
	return out
}

func matchResultFrom(res *match.Result) *MatchResult {
	pairs := make([]*match.Pair, len(res.Pairs))
	for i, p := range res.Pairs {
		if p == nil {
			continue
		}
		cp := *p
		pairs[i] = &cp
	}
	return &MatchResult{
		Source:      res.Source,
		Target:      res.Target,
		Pairs:       pairs,
		Warnings:    append([]string(nil), res.Warnings...),
		SrcListTime: res.SrcListTime,
		TgtListTime: res.TgtListTime,
	}
}

func cloneBackupRequest(req backup.Request) *backup.Request {
	cp := req
	cp.OnLine = nil
	if req.Include != nil {
		cp.Include = append([]string(nil), req.Include...)
	}
	if req.Exclude != nil {
		cp.Exclude = append([]string(nil), req.Exclude...)
	}
	if req.CreateParent != nil {
		v := *req.CreateParent
		cp.CreateParent = &v
	}
	if req.Flags != nil {
		f := *req.Flags
		if req.Flags.RecvPropsAdd != nil {
			f.RecvPropsAdd = append([]string(nil), req.Flags.RecvPropsAdd...)
		}
		if req.Flags.RecvPropsDel != nil {
			f.RecvPropsDel = append([]string(nil), req.Flags.RecvPropsDel...)
		}
		cp.Flags = &f
	}
	return &cp
}

func cloneMatchRequest(req match.Request) *match.Request {
	cp := req
	cp.SrcContext = nil
	cp.TgtContext = nil
	if req.Props != nil {
		cp.Props = append([]string(nil), req.Props...)
	}
	if req.Cols != nil {
		cp.Cols = append([]string(nil), req.Cols...)
	}
	if req.Include != nil {
		cp.Include = append([]string(nil), req.Include...)
	}
	if req.Exclude != nil {
		cp.Exclude = append([]string(nil), req.Exclude...)
	}
	return &cp
}
