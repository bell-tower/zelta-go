package backup

import "strings"

// SendRecv holds zfs send/recv flag fragments (oracle SEND_DEFAULT / RECV_*).
// Constructible by external modules; CLI merges env via internal/opt.SendRecvFrom.
type SendRecv struct {
	SendDefault    string   `json:"send_default,omitempty"`
	RecvDefault    string   `json:"recv_default,omitempty"`
	RecvTop        string   `json:"recv_top,omitempty"`
	RecvFS         string   `json:"recv_fs,omitempty"`
	RecvVol        string   `json:"recv_vol,omitempty"`
	RecvPartial    string   `json:"recv_partial,omitempty"`
	Resume         bool     `json:"resume,omitempty"`
	RecvPropsAdd   []string `json:"recv_props_add,omitempty"`
	RecvPropsDel   []string `json:"recv_props_del,omitempty"`
	Bookmarks      bool     `json:"bookmarks,omitempty"`
	BookmarkPrefix string   `json:"bookmark_prefix,omitempty"`
	SendOverride   string   `json:"send_override,omitempty"`
	RecvOverride   string   `json:"recv_override,omitempty"`
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
// RECV_PROPS_DEL, and RECV_PARTIAL (when Resume). RECV_OVERRIDE replaces the
// whole fragment.
//
// The clone-origin receive property (-o origin=…) is NOT part of this
// whitespace-joined fragment. Callers insert it as its own argv pair
// (["-o", "origin=…"]) just before the target dataset after building the recv
// argv, because origin values may contain spaces (dataset names with spaces):
// a "-o origin=…" token with an embedded space would make zfs recv read the
// space after -o as part of the property name, and a whitespace-joined
// fragment would be split apart by tokenization.
//
// fresh marks a receive that starts a dataset on the target (no common
// snapshot yet — full backup, rotate seed, clone origin). Pure incremental
// receives pass fresh=false and skip the contextual TOP/FS/VOL properties.
func (s SendRecv) RecvFlags(sourceType string, root, fresh bool) string {
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
	return strings.Join(parts, " ")
}
