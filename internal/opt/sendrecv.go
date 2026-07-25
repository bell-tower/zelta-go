package opt

// SendRecv holds zfs send/recv flag fragments (oracle SEND_DEFAULT / RECV_*).
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
	BookmarkMode   string
	BookmarkPrefix string
	SendCheck      bool
	SendOverride   string // SEND_OVERRIDE — if set, replaces SendDefault
	RecvOverride   string // RECV_OVERRIDE — if set, replaces all recv flags
}

// Default returns built-in oracle defaults (bin/zelta : ${ZELTA_*=…}).
func Default() SendRecv {
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

// Resolve merges built-in defaults with the seeded environment
// (defaults + zelta.env-injected/process env + legacy aliases).
func Resolve() SendRecv {
	e, _ := seed()
	return SendRecvFrom(e)
}

// SendRecvFrom builds fragments from a resolved Env (e.g. opt.Parse output).
// Legacy aliases (SEND_FLAGS, RECEIVE_FLAGS) are handled by seed/Parse.
func SendRecvFrom(e Env) SendRecv {
	d := Default()
	set := func(key string, dst *string) {
		if v := e.Get(key); v != "" {
			*dst = v
		}
	}
	set("SEND_DEFAULT", &d.SendDefault)
	set("RECV_DEFAULT", &d.RecvDefault)
	set("RECV_TOP", &d.RecvTop)
	set("RECV_FS", &d.RecvFS)
	set("RECV_VOL", &d.RecvVol)
	set("RECV_PARTIAL", &d.RecvPartial)
	set("SEND_OVERRIDE", &d.SendOverride)
	set("RECV_OVERRIDE", &d.RecvOverride)
	d.Resume = e.Bool("RESUME", d.Resume)
	d.RecvPropsAdd = e.List("RECV_PROPS_ADD")
	d.RecvPropsDel = e.List("RECV_PROPS_DEL")
	d.BookmarkMode = e.Get("BOOKMARK_MODE")
	d.BookmarkPrefix = e.Get("BOOKMARK_PREFIX")
	d.SendCheck = e.Bool("SEND_CHECK", false)
	return d
}

// SendFlags is the send -flags fragment (override wins).
func (s SendRecv) SendFlags() string {
	if s.SendOverride != "" {
		return s.SendOverride
	}
	return s.SendDefault
}
