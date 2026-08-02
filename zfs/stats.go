package zfs

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
)

// PipeStats reports send/recv replication telemetry accumulated from zfs
// pipe output, mirroring the Awk oracle's Summary counters
// (replicationSize, replicationStreamsReceived, replicationTime).
type PipeStats struct {
	// Bytes is the sum of "size N" headers from zfs send -P.
	Bytes int64
	// Streams is the number of "received … stream in N seconds" lines
	// from zfs recv -v.
	Streams int
	// Secs is the summed per-stream seconds from zfs recv -v.
	Secs float64
}

// PipeStatsReporter is implemented by executors that accumulate pipe
// telemetry (currently *Real). Verbs reset the counters with TakeStats()
// before their own execution and read the result afterwards, so stats never
// leak between runs that share one executor.
type PipeStatsReporter interface {
	TakeStats() PipeStats
}

// parseStatsLine feeds one line of zfs send/recv pipe output into st:
//   - "size N" (zfs send -P, stderr) → Bytes
//   - "received … stream in N seconds" (zfs recv -v, stdout) → Streams + Secs
func parseStatsLine(st *PipeStats, line string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "size":
		if len(fields) < 2 {
			return
		}
		if n, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			st.Bytes += n
		}
	case "received":
		if len(fields) < 5 || fields[2] != "stream" || fields[3] != "in" {
			return
		}
		if secs, err := strconv.ParseFloat(fields[4], 64); err == nil {
			st.Streams++
			st.Secs += secs
		}
	}
}

// pipeSink splits zfs pipe output into lines, feeds each into the executor's
// stats accumulator, and forwards complete lines to StderrLog when set.
type pipeSink struct {
	exec *Real
	mu   sync.Mutex
	buf  []byte
}

func (s *pipeSink) Write(p []byte) (int, error) {
	n := len(p)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	for {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			break
		}
		line := s.buf[:i]
		s.exec.statsMu.Lock()
		parseStatsLine(&s.exec.stats, string(line))
		s.exec.statsMu.Unlock()
		if s.exec.StderrLog != nil {
			_, _ = s.exec.StderrLog.Write(append(append([]byte{}, line...), '\n'))
		}
		s.buf = s.buf[i+1:]
	}
	return n, nil
}
