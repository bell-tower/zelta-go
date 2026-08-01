package report

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"git.belltower.it/djbell/zelta-go/endpoint"
)

// BackupResult mirrors the upstream Awk `zelta backup --json` schema.
type BackupResult struct {
	OutputVersion          OutputVersion `json:"output_version"`
	StartTime              string        `json:"startTime,omitempty"`
	EndTime                string        `json:"endTime,omitempty"`
	RunTime                string        `json:"runTime,omitempty"`
	SourceUser             string        `json:"sourceUser,omitempty"`
	SourceHost             string        `json:"sourceHost,omitempty"`
	SourceDataset          string        `json:"sourceDataset,omitempty"`
	SourceSnapshot         string        `json:"sourceSnapshot,omitempty"`
	SourceEndpoint         string        `json:"sourceEndpoint,omitempty"`
	SourceListTime         string        `json:"sourceListTime,omitempty"`
	SourceWritten          string        `json:"sourceWritten,omitempty"`
	TargetUser             string        `json:"targetUser,omitempty"`
	TargetHost             string        `json:"targetHost,omitempty"`
	TargetDataset          string        `json:"targetDataset,omitempty"`
	TargetSnapshot         string        `json:"targetSnapshot,omitempty"`
	TargetEndpoint         string        `json:"targetEndpoint,omitempty"`
	TargetListTime         string        `json:"targetListTime,omitempty"`
	TargetsCloned          string        `json:"targetsCloned,omitempty"`
	TargetsResumed         string        `json:"targetsResumed,omitempty"`
	ReplicationSize        string        `json:"replicationSize,omitempty"`
	ReplicationStreamsSent string        `json:"replicationStreamsSent,omitempty"`
	ReplicationStreamsRecv string        `json:"replicationStreamsReceived,omitempty"`
	ReplicationErrorCode   string        `json:"replicationErrorCode,omitempty"`
	ReplicationTime        string        `json:"replicationTime,omitempty"`
	SentStreams            []string      `json:"sentStreams,omitempty"`
	ErrorMessages          []string      `json:"errorMessages,omitempty"`
}

type OutputVersion struct {
	Command   string `json:"command"`
	VersMajor int    `json:"vers_major"`
	VersMinor int    `json:"vers_minor"`
}

// NewBackupResult builds backup JSON from available execution data.
// replicationSize is the raw byte count (Awk Summary parity); replicationTime
// is the summed zfs recv -v stream seconds; streamsRecv is the recv-confirmed
// stream count (0 falls back to streamsSent).
func NewBackupResult(
	src, tgt endpoint.Endpoint,
	streamsSent int, sentStreams []string,
	replicationErrors []string, messages []string,
	startTime time.Time,
	endTime time.Time,
	replicationSize int64, streamsRecv int, replicationTime float64,
) *BackupResult {
	r := &BackupResult{
		OutputVersion: OutputVersion{
			Command:   "zelta backup",
			VersMajor: 1,
			VersMinor: 1,
		},
	}
	if src.User != "" {
		r.SourceUser = src.User
	}
	if src.Host != "" {
		r.SourceHost = src.Host
	}
	if src.Dataset != "" {
		r.SourceDataset = src.Dataset
	}
	if src.Snapshot != "" {
		r.SourceSnapshot = src.Snapshot
	}
	if s := src.String(); s != "" {
		r.SourceEndpoint = s
	}
	if tgt.User != "" {
		r.TargetUser = tgt.User
	}
	if tgt.Host != "" {
		r.TargetHost = tgt.Host
	}
	if tgt.Dataset != "" {
		r.TargetDataset = tgt.Dataset
	}
	if tgt.Snapshot != "" {
		r.TargetSnapshot = tgt.Snapshot
	}
	if s := tgt.String(); s != "" {
		r.TargetEndpoint = s
	}
	if streamsSent > 0 {
		r.ReplicationStreamsSent = fmt.Sprintf("%d", streamsSent)
		if streamsRecv > 0 {
			r.ReplicationStreamsRecv = fmt.Sprintf("%d", streamsRecv)
		} else {
			r.ReplicationStreamsRecv = fmt.Sprintf("%d", streamsSent)
		}
	}
	if replicationSize > 0 {
		r.ReplicationSize = fmt.Sprintf("%d", replicationSize)
	}
	if replicationTime > 0 {
		r.ReplicationTime = strconv.FormatFloat(replicationTime, 'g', -1, 64)
	}
	r.SentStreams = sentStreams
	r.ErrorMessages = append(append([]string(nil), messages...), replicationErrors...)
	if !startTime.IsZero() {
		r.StartTime = startTime.Format(time.RFC3339)
	}
	if !endTime.IsZero() {
		r.EndTime = endTime.Format(time.RFC3339)
	}
	if !startTime.IsZero() && !endTime.IsZero() {
		r.RunTime = endTime.Sub(startTime).Round(time.Millisecond).String()
	}
	if len(replicationErrors) > 0 {
		r.ReplicationErrorCode = "1"
	} else {
		r.ReplicationErrorCode = "0"
	}
	return r
}

// Marshal returns the JSON-encoded backup result. Matches the upstream
// Awk schema (single JSON object with output_version, flat fields,
// sentStreams, errorMessages).
func (r *BackupResult) Marshal() ([]byte, error) {
	return json.Marshal(r)
}
