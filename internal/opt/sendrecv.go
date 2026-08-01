package opt

import "git.belltower.it/djbell/zelta-go/backup"

// SendRecv is the public send/recv flag type (re-exported for CLI callers).
type SendRecv = backup.SendRecv

// Default returns built-in oracle defaults.
func Default() SendRecv {
	return backup.DefaultSendRecv()
}

// Resolve merges built-in defaults with the seeded environment
// (defaults + zelta.env-injected/process env + legacy aliases).
// CLI-only; library action paths must not call this.
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
	// BOOKMARK_MODE is "0"/"1" (or empty → false).
	d.Bookmarks = e.Get("BOOKMARK_MODE") == "1"
	d.BookmarkPrefix = e.Get("BOOKMARK_PREFIX")
	return d
}
