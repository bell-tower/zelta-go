package backup

import (
	"strings"

	"github.com/bell-tower/zelta-go/internal/units"
)

// printSummary reports permission problems, why datasets were not synced,
// and the transfer totals.
func (r *run) printSummary() {
	if len(r.sum.failedProps) > 0 {
		r.rt.Log.Warnf("missing `zfs allow` permissions: %s", strings.Join(r.sum.failedProps, ","))
	}
	if r.tree.upToDate == len(r.list) {
		status := "dataset"
		if len(r.list) != 1 {
			status = itoa(len(r.list)) + " datasets"
		}
		r.rt.Log.Noticef("%s up-to-date", status)
	} else {
		for _, suffix := range r.list {
			a := r.action(suffix)
			if a.statusNotice != "" {
				r.rt.Log.Noticef("%s: %s", a.statusNotice, r.tgt.DS+suffix)
			}
			if a.statusInfo != "" {
				r.rt.Log.Infof("%s: %s", a.statusInfo, r.tgt.DS+suffix)
			}
		}
	}
	if n := r.sum.streamsReceived; n > 0 {
		// Parallel runs report elapsed time; summed stream times would
		// exceed it. JSON keeps the summed replicationTime.
		seconds := r.sum.replicationTime
		if r.parallel > 1 && r.sum.parallelSeconds > 0 {
			seconds = r.sum.parallelSeconds
		}
		r.rt.Log.Noticef("%s sent, %d streams received in %s seconds",
			units.HumanBytes(float64(r.sum.replicationSize)), n, units.FormatNumber(seconds))
	}
}

// report records the JSON summary of the run.
func (r *run) report() {
	rt := r.rt
	if rt.JSON == nil {
		return
	}
	s := &r.sum
	rt.Report("startTime", s.startTime)
	if s.finished {
		rt.Report("endTime", s.endTime)
		rt.Report("runTime", s.endTime-s.startTime)
	}
	if r.src.ID != "" {
		rt.Report("sourceUser", r.src.User)
		rt.Report("sourceHost", r.src.Host)
		rt.Report("sourceDataset", r.src.DS)
		rt.Report("sourceSnapshot", r.src.Snap)
		rt.Report("sourceEndpoint", r.src.ID)
	}
	if r.tgt.ID != "" {
		rt.Report("targetUser", r.tgt.User)
		rt.Report("targetHost", r.tgt.Host)
		rt.Report("targetDataset", r.tgt.DS)
		rt.Report("targetSnapshot", r.tgt.Snap)
		rt.Report("targetEndpoint", r.tgt.ID)
	}
	if s.sourceListTime > 0 {
		rt.Report("sourceListTime", s.sourceListTime)
	}
	if s.targetListTime > 0 {
		rt.Report("targetListTime", s.targetListTime)
	}
	if s.sourceWritten > 0 {
		rt.Report("sourceWritten", s.sourceWritten)
	}
	if s.targetsCloned > 0 {
		rt.Report("targetsCloned", s.targetsCloned)
	}
	if s.targetsResumed > 0 {
		rt.Report("targetsResumed", s.targetsResumed)
	}
	if s.replicationSize > 0 {
		rt.Report("replicationSize", s.replicationSize)
	}
	rt.Report("replicationStreamsSent", s.streamsSent)
	rt.Report("replicationStreamsReceived", s.streamsReceived)
	rt.Report("replicationErrorCode", s.errorCode)
	if s.streamsReceived > 0 {
		rt.Report("replicationTime", s.replicationTime)
	}
	if len(s.failedProps) > 0 {
		rt.Report("failed_props", strings.Join(s.failedProps, ","))
	}
	rt.JSON.SentStreams = append(rt.JSON.SentStreams, s.sentStreams...)
}
