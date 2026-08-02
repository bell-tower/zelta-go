package job

import (
	"encoding/json"
	"fmt"

	"github.com/bell-tower/zelta-go/backup"
	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/match"
)

// wire shapes keep action packages free of duration/string import quirks.

type documentJSON struct {
	Version int        `json:"version"`
	Items   []itemJSON `json:"items"`
}

type itemJSON struct {
	Op           Op                 `json:"op"`
	Request      json.RawMessage    `json:"request,omitempty"`
	Result       json.RawMessage    `json:"result,omitempty"`
	BackupReq    *backupRequestJSON `json:"-"`
	MatchReq     *matchRequestJSON  `json:"-"`
	BackupResult *BackupResult      `json:"-"`
	MatchResult  *MatchResult       `json:"-"`
}

type backupRequestJSON struct {
	Source        endpoint.Endpoint `json:"source"`
	Target        endpoint.Endpoint `json:"target"`
	TargetOrigin  endpoint.Endpoint `json:"target_origin,omitempty"`
	Intermediate  bool              `json:"intermediate,omitempty"`
	SnapMode      string            `json:"snap_mode,omitempty"`
	SnapName      string            `json:"snap_name,omitempty"`
	SnapTime      string            `json:"snap_time,omitempty"` // Go duration or seconds
	SnapSize      int64             `json:"snap_size,omitempty"`
	Depth         int               `json:"depth,omitempty"`
	Include       []string          `json:"include,omitempty"`
	Exclude       []string          `json:"exclude,omitempty"`
	CreateParent  *bool             `json:"create_parent,omitempty"`
	Flags         *backup.SendRecv  `json:"flags,omitempty"`
	SyncDirection string            `json:"sync_direction,omitempty"`
}

type matchRequestJSON struct {
	Source                  endpoint.Endpoint `json:"source"`
	Target                  endpoint.Endpoint `json:"target"`
	Props                   []string          `json:"props,omitempty"`
	Cols                    []string          `json:"cols,omitempty"`
	Depth                   int               `json:"depth,omitempty"`
	Include                 []string          `json:"include,omitempty"`
	Exclude                 []string          `json:"exclude,omitempty"`
	NoWritten               bool              `json:"no_written,omitempty"`
	PreserveSourceSnapshots bool              `json:"preserve_source_snapshots,omitempty"`
}

// MarshalJSON encodes the document in native SDK form.
func (d Document) MarshalJSON() ([]byte, error) {
	out := documentJSON{Version: d.Version, Items: make([]itemJSON, len(d.Items))}
	for i := range d.Items {
		w, err := d.Items[i].toWire()
		if err != nil {
			return nil, err
		}
		out.Items[i] = w
	}
	return json.Marshal(out)
}

// UnmarshalJSON decodes native SDK JSON into typed Requests/Results.
func (d *Document) UnmarshalJSON(b []byte) error {
	var raw documentJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	d.Version = raw.Version
	d.Items = make([]Item, len(raw.Items))
	for i := range raw.Items {
		it, err := itemFromWire(raw.Items[i])
		if err != nil {
			return fmt.Errorf("items[%d]: %w", i, err)
		}
		d.Items[i] = it
	}
	return nil
}

func (it Item) toWire() (itemJSON, error) {
	w := itemJSON{Op: it.Op}
	switch it.Op {
	case OpBackup:
		if it.Backup != nil {
			br, err := backupRequestToJSON(it.Backup)
			if err != nil {
				return w, err
			}
			raw, err := json.Marshal(br)
			if err != nil {
				return w, err
			}
			w.Request = raw
		}
		if it.BackupResult != nil {
			raw, err := json.Marshal(it.BackupResult)
			if err != nil {
				return w, err
			}
			w.Result = raw
		}
	case OpMatch:
		if it.Match != nil {
			raw, err := json.Marshal(matchRequestToJSON(it.Match))
			if err != nil {
				return w, err
			}
			w.Request = raw
		}
		if it.MatchResult != nil {
			raw, err := json.Marshal(it.MatchResult)
			if err != nil {
				return w, err
			}
			w.Result = raw
		}
	default:
		return w, fmt.Errorf("unknown op %q", it.Op)
	}
	return w, nil
}

func itemFromWire(w itemJSON) (Item, error) {
	it := Item{Op: w.Op}
	switch w.Op {
	case OpBackup:
		if len(w.Request) > 0 {
			var br backupRequestJSON
			if err := json.Unmarshal(w.Request, &br); err != nil {
				return it, fmt.Errorf("request: %w", err)
			}
			req, err := br.toRequest()
			if err != nil {
				return it, err
			}
			it.Backup = req
		}
		if len(w.Result) > 0 {
			var res BackupResult
			if err := json.Unmarshal(w.Result, &res); err != nil {
				return it, fmt.Errorf("result: %w", err)
			}
			it.BackupResult = &res
		}
	case OpMatch:
		if len(w.Request) > 0 {
			var mr matchRequestJSON
			if err := json.Unmarshal(w.Request, &mr); err != nil {
				return it, fmt.Errorf("request: %w", err)
			}
			it.Match = mr.toRequest()
		}
		if len(w.Result) > 0 {
			var res MatchResult
			if err := json.Unmarshal(w.Result, &res); err != nil {
				return it, fmt.Errorf("result: %w", err)
			}
			it.MatchResult = &res
		}
	case "":
		return it, fmt.Errorf("op required")
	default:
		return it, fmt.Errorf("unknown op %q", w.Op)
	}
	return it, nil
}

func backupRequestToJSON(req *backup.Request) (backupRequestJSON, error) {
	out := backupRequestJSON{
		Source:       req.Source,
		Target:       req.Target,
		TargetOrigin: req.TargetOrigin,
		Intermediate: req.Intermediate,
		SnapName:     req.SnapName,
		SnapSize:     req.SnapSize,
		Depth:        req.Depth,
		Include:      req.Include,
		Exclude:      req.Exclude,
		CreateParent: req.CreateParent,
		Flags:        req.Flags,
	}
	if req.SnapMode != "" {
		out.SnapMode = string(req.SnapMode)
	}
	if req.SnapTime > 0 {
		out.SnapTime = req.SnapTime.String()
	}
	if req.SyncDirection != "" {
		out.SyncDirection = string(req.SyncDirection)
	}
	return out, nil
}

func (b backupRequestJSON) toRequest() (*backup.Request, error) {
	req := &backup.Request{
		Source:       b.Source,
		Target:       b.Target,
		TargetOrigin: b.TargetOrigin,
		Intermediate: b.Intermediate,
		SnapName:     b.SnapName,
		SnapSize:     b.SnapSize,
		Depth:        b.Depth,
		Include:      b.Include,
		Exclude:      b.Exclude,
		CreateParent: b.CreateParent,
		Flags:        b.Flags,
	}
	if b.Source.IsZero() {
		return nil, fmt.Errorf("backup request: source required")
	}
	if b.Target.IsZero() {
		return nil, fmt.Errorf("backup request: target required")
	}
	if b.SnapMode != "" {
		m, err := backup.ParseSnapMode(b.SnapMode)
		if err != nil {
			return nil, err
		}
		req.SnapMode = m
	}
	if b.SnapTime != "" {
		d, err := backup.ParseSnapTime(b.SnapTime)
		if err != nil {
			return nil, err
		}
		req.SnapTime = d
	}
	if b.SyncDirection != "" {
		dir, err := backup.ParseSyncDirection(b.SyncDirection)
		if err != nil {
			return nil, err
		}
		req.SyncDirection = dir
	}
	return req, nil
}

func matchRequestToJSON(req *match.Request) matchRequestJSON {
	return matchRequestJSON{
		Source:                  req.Source,
		Target:                  req.Target,
		Props:                   req.Props,
		Cols:                    req.Cols,
		Depth:                   req.Depth,
		Include:                 req.Include,
		Exclude:                 req.Exclude,
		NoWritten:               req.NoWritten,
		PreserveSourceSnapshots: req.PreserveSourceSnapshots,
	}
}

func (m matchRequestJSON) toRequest() *match.Request {
	return &match.Request{
		Source:                  m.Source,
		Target:                  m.Target,
		Props:                   m.Props,
		Cols:                    m.Cols,
		Depth:                   m.Depth,
		Include:                 m.Include,
		Exclude:                 m.Exclude,
		NoWritten:               m.NoWritten,
		PreserveSourceSnapshots: m.PreserveSourceSnapshots,
	}
}

