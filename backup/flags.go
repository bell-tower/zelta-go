package backup

import "strings"

// SendRecv holds zfs send/recv flag fragments (oracle SEND_DEFAULT / RECV_*).
// Constructible by external modules; CLI merges env via internal/opt.SendRecvFrom.
type SendRecv struct {
	SendDefault    string   // SEND_DEFAULT
	RecvDefault    string   // RECV_DEFAULT (always prepended when set)
	RecvTop        string   // RECV_TOP — full root only
	RecvFS         string   // RECV_FS — full filesystem
	RecvVol        string   // RECV_VOL — full volume
	RecvPartial    string   // RECV_PARTIAL — when Resume
	Resume         bool     // RESUME gates RecvPartial
	RecvPropsAdd   []string // RECV_PROPS_ADD — repeated zfs recv -o properties
	RecvPropsDel   []string // RECV_PROPS_DEL — repeated zfs recv -x properties
	Bookmarks      bool     // create final-stream bookmarks after successful recv
	BookmarkPrefix string
	SendOverride   string // SEND_OVERRIDE — if set, replaces SendDefault
	RecvOverride   string // RECV_OVERRIDE — if set, replaces all recv flags
}

// DefaultSendRecv returns built-in oracle defaults (bin/zelta : ${ZELTA_*=…}).
func DefaultSendRecv() SendRecv {
	return SendRecv{
		SendDefault: "-L -c -e",
		RecvDefault: "",
		RecvTop:     "-o readonly=on",
		RecvFS:      "-u -x mountpoint -o canmount=noauto",
		RecvVol:     "",
		RecvPartial: "-s",
		Resume:      true,
	}
}

// SendFlags is the send -flags fragment (override wins).
func (s SendRecv) SendFlags() string {
	if s.SendOverride != "" {
		return s.SendOverride
	}
	return s.SendDefault
}

// RecvFlags is the recv -flags fragment, in oracle order: RECV_DEFAULT;
// fresh receives add TOP (root) + FS/VOL by source type; then RECV_PROPS_ADD,
// RECV_PROPS_DEL, RECV_PARTIAL (when Resume), and finally -o origin.
// RECV_OVERRIDE replaces the whole fragment.
//
// fresh marks a receive that starts a dataset on the target (no common
// snapshot yet — full backup, rotate seed, clone origin). Pure incremental
// receives pass fresh=false and skip the contextual TOP/FS/VOL properties.
func (s SendRecv) RecvFlags(sourceType string, root, fresh bool, origin string) string {
	if s.RecvOverride != "" {
		return s.RecvOverride
	}
	var parts []string
	if s.RecvDefault != "" {
		parts = append(parts, s.RecvDefault)
	}
	if fresh {
		if root && s.RecvTop != "" {
			parts = append(parts, s.RecvTop)
		}
		switch sourceType {
		case "volume":
			if s.RecvVol != "" {
				parts = append(parts, s.RecvVol)
			}
		default: // filesystem
			if s.RecvFS != "" {
				parts = append(parts, s.RecvFS)
			}
		}
	}
	for _, prop := range s.RecvPropsAdd {
		if prop != "" {
			parts = append(parts, "-o "+prop)
		}
	}
	for _, prop := range s.RecvPropsDel {
		if prop != "" {
			parts = append(parts, "-x "+prop)
		}
	}
	if s.Resume && s.RecvPartial != "" {
		parts = append(parts, s.RecvPartial)
	}
	if origin != "" {
		parts = append(parts, "-o origin="+origin)
	}
	return strings.Join(parts, " ")
}
