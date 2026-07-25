package opt

// defaults mirrors bin/zelta `: ${ZELTA_KEY:=value}` built-ins.
// SNAP_NAME is omitted: the oracle default is shell $(date ...); Go snap.go
// generates snapshot names itself.
var defaults = map[string]string{
	"LOG_LEVEL":       "2",
	"RESUME":          "1",
	"SNAP_MODE":       "IF_NEEDED",
	"SYNC_DIRECTION":  "PULL",
	"SEND_INTR":       "1",
	"SEND_DEFAULT":    "-L -c -e",
	"SEND_DECRYPTED":  "-L -c",
	"SEND_RAW":        "--raw",
	"SEND_NEW":        "-p",
	"RECV_TOP":        "-o readonly=on",
	"RECV_FS":         "-u -x mountpoint -o canmount=noauto",
	"RECV_PARTIAL":    "-s",
	"BOOKMARK_MODE":   "0",
	"BOOKMARK_PREFIX": "{targethost}_",
	"CREATE_PARENT":   "1",
	"LIST_WRITTEN":    "1",
	"REMOTE_COMMAND":  "ssh",
}
