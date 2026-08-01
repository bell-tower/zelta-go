package backup

import (
	"strconv"
	"strings"
)

// HumanBytes renders a byte count like the upstream Awk h_num() helper:
// binary units (B, K, M, G, T, P, E), value truncated toward zero.
func HumanBytes(n int64) string {
	if n < 0 {
		return "-" + HumanBytes(-n)
	}
	suffix := "B"
	num := float64(n)
	for _, s := range []string{"K", "M", "G", "T", "P", "E"} {
		if num < 1024 {
			break
		}
		num /= 1024
		suffix = s
	}
	return strconv.FormatInt(int64(num), 10) + suffix
}

// streamParser accumulates replication telemetry from the zfs send/recv pipe
// stderr, mirroring the Awk oracle's Summary counters:
//   - "size N" from zfs send -P → replicationSize
//   - "received … stream in N seconds" from zfs recv -v
//     → replicationStreamsReceived + replicationTime
type streamParser struct {
	bytes   int64
	streams int
	secs    float64
	onLine  func(string)
}

func (p *streamParser) Line(line string) {
	if p.onLine != nil {
		p.onLine(line)
	}
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
			p.bytes += n
		}
	case "received":
		if len(fields) < 5 || fields[2] != "stream" || fields[3] != "in" {
			return
		}
		if secs, err := strconv.ParseFloat(fields[4], 64); err == nil {
			p.streams++
			p.secs += secs
		}
	}
}
