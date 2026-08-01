package backup

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
