package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bell-tower/zelta-go/endpoint"
)

func TestNewBackupResult(t *testing.T) {
	src := endpoint.Endpoint{
		User:     "root",
		Host:     "src.example.com",
		Dataset:  "apool/data",
		Snapshot: "snap1",
		Remote:   true,
	}
	tgt := endpoint.Endpoint{
		Host:    "tgt.example.com",
		Dataset: "bpool/data",
		Remote:  true,
	}
	start := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 26, 10, 5, 30, 0, time.UTC)

	r := NewBackupResult(
		src, tgt,
		3, []string{"pool/ds@snap1", "pool/ds@snap2", "pool/ds2@snap1"},
		nil, []string{"filter pattern info"},
		start, end,
		2496, 3, 0.09,
	)

	if r.OutputVersion.Command != "zelta backup" {
		t.Errorf("command = %q, want %q", r.OutputVersion.Command, "zelta backup")
	}
	if r.OutputVersion.VersMajor != 1 || r.OutputVersion.VersMinor != 1 {
		t.Errorf("version = %d.%d, want 1.1", r.OutputVersion.VersMajor, r.OutputVersion.VersMinor)
	}
	if r.SourceUser != "root" {
		t.Errorf("SourceUser = %q, want %q", r.SourceUser, "root")
	}
	if r.SourceHost != "src.example.com" {
		t.Errorf("SourceHost = %q", r.SourceHost)
	}
	if r.SourceDataset != "apool/data" {
		t.Errorf("SourceDataset = %q", r.SourceDataset)
	}
	if r.SourceSnapshot != "snap1" {
		t.Errorf("SourceSnapshot = %q", r.SourceSnapshot)
	}
	if r.TargetHost != "tgt.example.com" {
		t.Errorf("TargetHost = %q", r.TargetHost)
	}
	if r.TargetDataset != "bpool/data" {
		t.Errorf("TargetDataset = %q", r.TargetDataset)
	}
	if r.TargetSnapshot != "" {
		t.Errorf("TargetSnapshot = %q, want empty", r.TargetSnapshot)
	}
	if r.ReplicationStreamsSent != "3" {
		t.Errorf("ReplicationStreamsSent = %q", r.ReplicationStreamsSent)
	}
	if r.ReplicationStreamsRecv != "3" {
		t.Errorf("ReplicationStreamsRecv = %q", r.ReplicationStreamsRecv)
	}
	if r.ReplicationSize != "2496" {
		t.Errorf("ReplicationSize = %q, want 2496", r.ReplicationSize)
	}
	if r.ReplicationTime != "0.09" {
		t.Errorf("ReplicationTime = %q, want 0.09", r.ReplicationTime)
	}
	if len(r.SentStreams) != 3 {
		t.Errorf("len(SentStreams) = %d", len(r.SentStreams))
	}
	if len(r.ErrorMessages) != 1 || r.ErrorMessages[0] != "filter pattern info" {
		t.Errorf("ErrorMessages = %v", r.ErrorMessages)
	}
	if r.ReplicationErrorCode != "0" {
		t.Errorf("ReplicationErrorCode = %q, want 0", r.ReplicationErrorCode)
	}
	if r.StartTime == "" || r.EndTime == "" || r.RunTime == "" {
		t.Errorf("timestamps missing: start=%q end=%q run=%q", r.StartTime, r.EndTime, r.RunTime)
	}
}

func TestNewBackupResultErrors(t *testing.T) {
	src := endpoint.Endpoint{Dataset: "pool/ds"}
	tgt := endpoint.Endpoint{Dataset: "pool/ds", Remote: true, Host: "remote"}
	start := time.Now()
	end := time.Now()

	r := NewBackupResult(src, tgt, 0, nil, []string{"connection refused"}, nil, start, end, 0, 0, 0)
	if r.ReplicationErrorCode != "1" {
		t.Errorf("ReplicationErrorCode = %q, want 1", r.ReplicationErrorCode)
	}
	if len(r.ErrorMessages) != 1 || r.ErrorMessages[0] != "connection refused" {
		t.Errorf("ErrorMessages = %v", r.ErrorMessages)
	}
	if r.ReplicationSize != "" {
		t.Errorf("ReplicationSize = %q, want empty", r.ReplicationSize)
	}
}

func TestBackupResultScrubsCarriageReturns(t *testing.T) {
	src := endpoint.Endpoint{
		Raw:     "local\rhost:pool/source",
		Host:    "local\rhost",
		Dataset: "pool/source",
		Remote:  true,
	}
	tgt := endpoint.Endpoint{Dataset: "pool/target"}
	start := time.Now()
	end := time.Now()
	r := NewBackupResult(src, tgt, 0, nil,
		[]string{"source dataset 'local\rhost:pool/source' does not exist"},
		nil, start, end, 0, 0, 0)
	if strings.Contains(r.SourceHost, "\r") || r.SourceHost != "localhost" {
		t.Errorf("SourceHost = %q", r.SourceHost)
	}
	if strings.Contains(r.SourceEndpoint, "\r") || r.SourceEndpoint != "localhost:pool/source" {
		t.Errorf("SourceEndpoint = %q", r.SourceEndpoint)
	}
	if len(r.ErrorMessages) != 1 || strings.Contains(r.ErrorMessages[0], "\r") {
		t.Errorf("ErrorMessages = %v", r.ErrorMessages)
	}
	if !strings.Contains(r.ErrorMessages[0], "localhost:pool/source") {
		t.Errorf("ErrorMessages[0] = %q", r.ErrorMessages[0])
	}
	data, err := r.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\\r") || bytes.IndexByte(data, '\r') >= 0 {
		t.Errorf("marshaled JSON still has CR: %s", data)
	}
}

func TestBackupResultMarshal(t *testing.T) {
	src := endpoint.Endpoint{Dataset: "pool/data", User: "root", Host: "example.com", Remote: true}
	tgt := endpoint.Endpoint{Dataset: "pool/backup", Remote: true, Host: "backup.example.com"}
	start := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 26, 12, 1, 0, 0, time.UTC)

	r := NewBackupResult(src, tgt, 2, []string{"pool/data@snap1", "pool/data@snap2"}, nil, nil, start, end, 0, 2, 0)
	data, err := r.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	// Check envelope
	ov, ok := parsed["output_version"].(map[string]interface{})
	if !ok {
		t.Fatal("missing output_version")
	}
	if ov["command"] != "zelta backup" {
		t.Errorf("command = %v", ov["command"])
	}

	if parsed["sourceUser"] != "root" {
		t.Errorf("sourceUser = %v", parsed["sourceUser"])
	}
	if parsed["sourceHost"] != "example.com" {
		t.Errorf("sourceHost = %v", parsed["sourceHost"])
	}

	streams, ok := parsed["sentStreams"].([]interface{})
	if !ok || len(streams) != 2 {
		t.Errorf("sentStreams = %v, ok=%v", parsed["sentStreams"], ok)
	}
	if _, exists := parsed["errorMessages"]; exists {
		t.Error("errorMessages should be omitted when empty")
	}
}
