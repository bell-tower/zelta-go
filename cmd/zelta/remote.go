package main

import (
	"strings"

	"github.com/bell-tower/zelta-go/internal/opt"
	"github.com/bell-tower/zelta-go/zfs"
)

// newReal builds a production executor from CLI/env (zelta.env already injected).
func newReal() *zfs.Real {
	return &zfs.Real{Remote: remoteFromEnv()}
}

// remoteFromEnv maps ZELTA_REMOTE_* to a zfs.Remote.
//
// Default (plain ssh): structured SSHConfig.
// Custom REMOTE_COMMAND / SEND / RECV / DEFAULT: CommandRemote (Awk string parity,
// including mbuffer|socat-style prefixes).
func remoteFromEnv() zfs.Remote {
	cmd, _ := opt.Lookup("REMOTE_COMMAND")
	send, _ := opt.Lookup("REMOTE_SEND")
	recv, _ := opt.Lookup("REMOTE_RECV")
	def, _ := opt.Lookup("REMOTE_DEFAULT")
	cmd = strings.TrimSpace(cmd)
	send = strings.TrimSpace(send)
	recv = strings.TrimSpace(recv)
	def = strings.TrimSpace(def)

	if send != "" || recv != "" || def != "" || (cmd != "" && cmd != "ssh") {
		return zfs.CommandRemote{
			Command: cmd,
			Send:    send,
			Recv:    recv,
			Default: def,
		}
	}
	return zfs.SSHConfig{}
}
