package job_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bell-tower/zelta-go/backup"
	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/job"
	"github.com/bell-tower/zelta-go/match"
	"github.com/bell-tower/zelta-go/zfs"
)

func TestRoundTripBackupRequest(t *testing.T) {
	cp := false
	flags := backup.DefaultSendRecv()
	flags.Bookmarks = true
	req := backup.Request{
		Source:        endpoint.MustParse("tank/src"),
		Target:        endpoint.MustParse("user@bak:pool/tgt"),
		SnapMode:      backup.SnapNever,
		SnapName:      "z",
		SnapTime:      time.Hour,
		SnapSize:      1024,
		Depth:         2,
		Include:       []string{"/a"},
		Exclude:       []string{"/b"},
		CreateParent:  &cp,
		Flags:         &flags,
		SyncDirection: backup.DirectionPush,
		OnLine:        func(string) {}, // must not serialize
	}
	doc := &job.Document{
		Version: job.Version,
		Items:   []job.Item{job.FromBackup(req, nil)},
	}
	raw, err := job.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("OnLine")) {
		t.Fatalf("callback leaked: %s", raw)
	}
	got, err := job.Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].Op != job.OpBackup {
		t.Fatalf("items: %+v", got.Items)
	}
	br := got.Items[0].Backup
	if br == nil {
		t.Fatal("nil backup request")
	}
	if br.OnLine != nil {
		t.Fatal("OnLine must stay nil after import")
	}
	if br.Source.Dataset != "tank/src" || br.Target.Host != "bak" || br.Target.User != "user" {
		t.Fatalf("endpoints: src=%+v tgt=%+v", br.Source, br.Target)
	}
	if br.SnapMode != backup.SnapNever || br.SnapTime != time.Hour || br.SnapSize != 1024 {
		t.Fatalf("dials: mode=%s time=%v size=%d", br.SnapMode, br.SnapTime, br.SnapSize)
	}
	if br.SyncDirection != backup.DirectionPush {
		t.Fatalf("dir=%s", br.SyncDirection)
	}
	if br.Flags == nil || !br.Flags.Bookmarks {
		t.Fatalf("flags=%+v", br.Flags)
	}
}

func TestRoundTripMatchAndResult(t *testing.T) {
	req := match.Request{
		Source: endpoint.MustParse("a/src"),
		Target: endpoint.MustParse("b/tgt"),
		Depth:  1,
	}
	res := &match.Result{
		Source: req.Source,
		Target: req.Target,
		Pairs: []*match.Pair{{
			DSSuffix: "",
			Info:     "up-to-date",
			Match:    "@s1",
		}},
		Warnings: []string{"w"},
	}
	doc := &job.Document{
		Version: job.Version,
		Items:   []job.Item{job.FromMatch(req, res)},
	}
	raw, err := job.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := job.Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	it := got.Items[0]
	if it.Match == nil || it.Match.Depth != 1 {
		t.Fatalf("match req: %+v", it.Match)
	}
	if it.MatchResult == nil || len(it.MatchResult.Pairs) != 1 || it.MatchResult.Pairs[0].Info != "up-to-date" {
		t.Fatalf("match res: %+v", it.MatchResult)
	}
}

func TestBackupResultExport(t *testing.T) {
	req := backup.Request{
		Source: endpoint.MustParse("s"),
		Target: endpoint.MustParse("t"),
	}
	res := &backup.Result{
		ErrCode:   backup.ErrCodeUpToDate,
		Stats:     zfs.PipeStats{Bytes: 9, Streams: 1, Secs: 0.5},
		StartTime: time.Unix(1700000000, 0).UTC(),
		EndTime:   time.Unix(1700000001, 0).UTC(),
		Plan:      &backup.Plan{Skip: 1},
	}
	doc := &job.Document{
		Version: job.Version,
		Items:   []job.Item{job.FromBackup(req, res)},
	}
	raw, err := job.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	items := probe["items"].([]any)
	item := items[0].(map[string]any)
	result := item["result"].(map[string]any)
	if result["err_code"] != "up_to_date" {
		t.Fatalf("err_code=%v", result["err_code"])
	}
	if result["skip"].(float64) != 1 {
		t.Fatalf("skip=%v", result["skip"])
	}
}

func TestEndpointStringImport(t *testing.T) {
	const body = `{
	  "version": 1,
	  "items": [{
	    "op": "backup",
	    "request": {
	      "source": "tank/a",
	      "target": {"host": "r", "dataset": "pool/a"},
	      "snap_mode": "NEVER"
	    }
	  }]
	}`
	doc, err := job.Decode(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	br := doc.Items[0].Backup
	if br.Source.Dataset != "tank/a" || br.Target.Host != "r" {
		t.Fatalf("%+v / %+v", br.Source, br.Target)
	}
}

func TestUnknownOp(t *testing.T) {
	_, err := job.Unmarshal([]byte(`{"version":1,"items":[{"op":"rotate"}]}`))
	if err == nil || !strings.Contains(err.Error(), "unknown op") {
		t.Fatalf("err=%v", err)
	}
}

func TestEncodeDecodeIO(t *testing.T) {
	doc := &job.Document{
		Version: job.Version,
		Items: []job.Item{
			job.FromBackup(backup.Request{
				Source: endpoint.MustParse("s"),
				Target: endpoint.MustParse("t"),
			}, nil),
		},
	}
	var buf bytes.Buffer
	if err := job.Encode(&buf, doc); err != nil {
		t.Fatal(err)
	}
	got, err := job.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Items[0].Backup.Source.Dataset != "s" {
		t.Fatal(got.Items[0].Backup.Source)
	}
}

func TestMixedDocument(t *testing.T) {
	doc := &job.Document{
		Version: job.Version,
		Items: []job.Item{
			job.FromMatch(match.Request{
				Source: endpoint.MustParse("a"),
				Target: endpoint.MustParse("b"),
			}, nil),
			job.FromBackup(backup.Request{
				Source: endpoint.MustParse("a"),
				Target: endpoint.MustParse("b"),
			}, nil),
		},
	}
	raw, err := job.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := job.Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Items[0].Op != job.OpMatch || got.Items[1].Op != job.OpBackup {
		t.Fatalf("%v %v", got.Items[0].Op, got.Items[1].Op)
	}
}
