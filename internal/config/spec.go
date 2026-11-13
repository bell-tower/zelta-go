// Package config defines Zelta's options and resolves their values from
// built-in defaults, zelta.env, policy files, the environment, and the
// command line, in increasing order of precedence.
//
// The option registry (Specs) is the single source of truth for command-line
// flags, environment variable names, policy keys, help text, and shell
// completion. It is a typed port of Zelta's historical zelta-opts.tsv.
package config

import (
	"slices"
	"strings"
)

// Type is how a flag changes its option.
type Type uint8

const (
	// True sets the option to "1".
	True Type = iota
	// False sets the option to "0".
	False
	// Set assigns Spec.Value, or the flag's argument when Value is empty.
	Set
	// ArgList appends the flag itself (e.g. "--raw") to a space-separated list.
	ArgList
	// List appends the flag's argument to a comma-separated list.
	List
	// Incr increments a numeric option.
	Incr
	// Decr decrements a numeric option.
	Decr
	// Invalid rejects the flag with Spec.Warning.
	Invalid
)

// TakesArg reports whether a flag of this type consumes an argument.
func (s *Spec) TakesArg() bool {
	return (s.Type == Set && s.Value == "") || s.Type == List
}

// Spec describes one command-line option and the option key it controls.
type Spec struct {
	// Verbs lists the commands accepting the flags; "all" means every verb.
	Verbs []string
	// Flags are the accepted spellings, e.g. "--verbose", "-v".
	Flags []string
	// Key is the option name without the ZELTA_ prefix.
	Key string
	// Type determines how the flag updates Key.
	Type Type
	// Value is the fixed value for Set flags that take no argument.
	Value string
	// Optional, when set, lets an argument-taking flag be given without a
	// value, in which case this value is used (e.g. "--retry" means 1).
	Optional string
	// Arg names the flag's argument in help output.
	Arg string
	// Desc is the help text.
	Desc string
	// Warning is printed whenever the flag is used (deprecations), or is
	// the error for Invalid flags.
	Warning string
	// Group orders flags in help output.
	Group string
	// Hidden omits the flag from help and completion.
	Hidden bool
	// Values lists suggested argument values for completion.
	Values []string
	// Complete names a dynamic completion: "endpoint", "file", "columns".
	Complete string
}

// Help groups, in display order.
const (
	GroupEndpoint = "Endpoint Options"
	GroupOutput   = "Output Options"
	GroupFilter   = "Dataset and Snapshot Filters"
	GroupSnapshot = "Snapshot Options"
	GroupSync     = "Sync Options"
	GroupConnect  = "Connection Options"
	GroupBookmark = "Bookmark Options"
	GroupOverride = "Advanced Send/Receive Overrides"
	GroupPass     = "Pass-Through zfs send/recv Flags"
	GroupMatch    = "Match Options"
	GroupPrune    = "Prune Options"
	GroupPolicy   = "Policy Options"
	GroupCommand  = "Command Options"
)

// GroupOrder lists help groups in display order.
var GroupOrder = []string{GroupEndpoint, GroupMatch, GroupPrune, GroupPolicy, GroupCommand, GroupSnapshot, GroupSync,
	GroupConnect, GroupBookmark, GroupFilter, GroupOutput, GroupOverride, GroupPass}

var (
	verbsBackup       = []string{"policy", "backup", "replicate", "sync", "rotate", "failover"}
	verbsBackupNoFO   = []string{"policy", "backup", "replicate", "sync", "rotate"}
	verbsSnapName     = []string{"policy", "backup", "replicate", "snapshot", "sync", "revert", "rotate", "clone", "failover"}
	verbsSnapMode     = []string{"policy", "backup", "replicate", "sync", "rotate", "clone"}
	verbsSendRecv     = []string{"policy", "backup", "replicate", "sync", "failover"}
	verbsAll          = []string{"all"}
	logModes          = []string{"text", "json"}
	snapModes         = []string{"IF_NEEDED", "ALWAYS", "SKIP", "0"}
	syncDirections    = []string{"PULL", "PUSH", "0"}
	pruneGuards       = []string{"latest", "unsynced", "none"}
	pruneNames        = []string{"zfsnap", "truenas", "sanoid"}
	passThroughFlags  = "-b,--backup,-c,--compressed,-D,--dedup,-e,--embed,--holds,-L,--largeblock,-p,--parsable,--proctitle,--props,--raw,--skipmissing,-V,-w"
	passThroughRecvFl = "-M,-u"
)

func flags(s string) []string { return strings.Split(s, ",") }

// Specs is the option registry.
var Specs = []*Spec{
	// Global options.
	{Verbs: verbsAll, Flags: flags("--verbose,-v"), Key: "LOG_LEVEL", Type: Incr, Desc: "increase verbosity (repeat for debug output)", Group: GroupOutput},
	{Verbs: verbsAll, Flags: flags("--quiet,-q"), Key: "LOG_LEVEL", Type: Decr, Desc: "decrease verbosity (repeat to hide warnings)", Group: GroupOutput},
	{Verbs: verbsAll, Flags: flags("--log-level"), Key: "LOG_LEVEL", Type: Set, Arg: "LEVEL", Desc: "set the log level: 0 errors, 1 warnings, 2 notices, 3 info, 4 debug", Group: GroupOutput, Values: []string{"0", "1", "2", "3", "4"}},
	{Verbs: verbsAll, Flags: flags("--log-mode"), Key: "LOG_MODE", Type: Set, Arg: "MODE", Desc: "set the log mode: text or json", Group: GroupOutput, Values: logModes},
	{Verbs: verbsAll, Flags: flags("--log-file"), Key: "LOG_FILE", Type: Set, Arg: "FILE", Desc: "append log output to FILE", Group: GroupOutput, Complete: "file"},
	{Verbs: verbsAll, Flags: flags("--text"), Key: "LOG_MODE", Type: Set, Value: "text", Desc: "print notices as plain text", Group: GroupOutput},
	{Verbs: verbsAll, Flags: flags("--help,-h,-?"), Key: "USAGE", Type: True, Desc: "show help", Group: GroupOutput},
	{Verbs: verbsAll, Flags: flags("--dryrun,--dry-run,-n"), Key: "DRYRUN", Type: True, Desc: "show commands but do not run them", Group: GroupOutput},
	{Verbs: verbsAll, Flags: flags("--depth,-d"), Key: "DEPTH", Type: Set, Arg: "LEVELS", Desc: "limit the recursion depth", Group: GroupFilter},
	{Verbs: verbsAll, Flags: flags("--exclude,-X"), Key: "EXCLUDE", Type: List, Arg: "PATTERN", Desc: "exclude source dataset or snapshot patterns", Group: GroupFilter},
	{Verbs: verbsAll, Flags: flags("--include"), Key: "INCLUDE", Type: List, Arg: "PATTERN", Desc: "include only source dataset or snapshot patterns", Group: GroupFilter},
	{Verbs: verbsAll, Flags: flags("--no-cache"), Key: "CACHE", Type: False, Desc: "ignore and do not update the cache", Group: GroupCommand},

	// Backup and other send/receive verbs.
	{Verbs: verbsBackup, Flags: flags("--json,-j"), Key: "LOG_MODE", Type: Set, Value: "json", Desc: "json output", Group: GroupOutput},
	{Verbs: verbsBackup, Flags: flags("--send-check"), Key: "SEND_CHECK", Type: True, Desc: "drop default zfs send options the source does not support", Group: GroupSync},
	{Verbs: verbsBackup, Flags: flags(passThroughFlags), Key: "SEND_OVERRIDE", Type: ArgList, Desc: "pass the flag to zfs send, replacing automatic send options", Group: GroupPass},
	{Verbs: verbsBackup, Flags: flags("--send-override"), Key: "SEND_OVERRIDE", Type: Set, Arg: "OPTIONS", Desc: "override all zfs send options", Group: GroupOverride},
	{Verbs: verbsBackup, Flags: flags("-F"), Key: "RECV_OVERRIDE", Type: ArgList, Desc: "pass -F to zfs recv (destructive)", Group: GroupPass, Warning: "destructive option 'F' used; consider 'zelta rotate' when possible"},
	{Verbs: verbsBackup, Flags: flags(passThroughRecvFl), Key: "RECV_OVERRIDE", Type: ArgList, Desc: "pass the flag to zfs recv, replacing automatic receive options", Group: GroupPass},
	{Verbs: verbsBackup, Flags: flags("--recv-override"), Key: "RECV_OVERRIDE", Type: Set, Arg: "OPTIONS", Desc: "override all zfs recv options", Group: GroupOverride},
	{Verbs: verbsBackupNoFO, Flags: flags("--rotate"), Key: "VERB", Type: Set, Value: "rotate", Desc: "rotate a diverged target (see zelta rotate)", Group: GroupSync},
	{Verbs: verbsBackupNoFO, Flags: flags("--replicate,-R"), Key: "VERB", Type: Set, Value: "replicate", Desc: "use zfs send --replicate", Group: GroupSync},
	{Verbs: verbsBackup, Flags: flags("--remember"), Key: "REMEMBER", Type: True, Desc: "remember the last synced snapshots and skip reading the target next time", Group: GroupSync},
	{Verbs: verbsBackup, Flags: flags("--resume"), Key: "RESUME", Type: True, Desc: "save partial transfers (default)", Group: GroupSync},
	{Verbs: verbsBackup, Flags: flags("--no-resume"), Key: "RESUME", Type: False, Desc: "discard failed partial transfers", Group: GroupSync},
	{Verbs: verbsSnapName, Flags: flags("--snap-name"), Key: "SNAP_NAME", Type: Set, Arg: "NAME", Desc: "specify a snapshot name; use '$(command)' for a dynamic name", Group: GroupSnapshot},
	{Verbs: verbsSnapName, Flags: flags("--snap-prefix"), Key: "SNAP_PREFIX", Type: Set, Arg: "PREFIX", Desc: "prefix default snapshot names", Group: GroupSnapshot},
	{Verbs: verbsSnapMode, Flags: flags("--snap-mode"), Key: "SNAP_MODE", Type: Set, Arg: "MODE", Desc: "when to snapshot: IF_NEEDED, ALWAYS, or 0", Group: GroupSnapshot, Values: snapModes},
	{Verbs: append(slices.Clone(verbsSnapMode), "snapshot"), Flags: flags("--snap-time"), Key: "SNAP_TIME", Type: Set, Arg: "DURATION", Desc: "skip the snapshot if every dataset was snapshotted within DURATION (or since an epoch time)", Group: GroupSnapshot},
	{Verbs: append(slices.Clone(verbsSnapMode), "snapshot"), Flags: flags("--snap-size"), Key: "SNAP_SIZE", Type: Set, Arg: "SIZE", Desc: "skip the snapshot unless SIZE bytes have been written", Group: GroupSnapshot},
	{Verbs: verbsSnapMode, Flags: flags("--no-snapshot"), Key: "SNAP_MODE", Type: False, Desc: "do not snapshot", Group: GroupSnapshot},
	{Verbs: verbsSnapMode, Flags: flags("--snapshot,--snapshot-always"), Key: "SNAP_MODE", Type: Set, Value: "ALWAYS", Desc: "always take a snapshot", Group: GroupSnapshot},
	{Verbs: verbsSnapMode, Flags: flags("--snapshot-skip"), Key: "SNAP_MODE", Type: Set, Value: "SKIP", Desc: "do not snapshot", Group: GroupSnapshot, Hidden: true},
	{Verbs: verbsSnapMode, Flags: flags("-s"), Key: "SNAP_MODE", Type: Set, Value: "IF_NEEDED", Hidden: true, Warning: "option '-s' for '--snapshot' is ambiguous and will be deprecated; use --snapshot, --snapshot-skip, --partial, or --skip-missing"},
	{Verbs: verbsSnapMode, Flags: flags("-S"), Key: "SNAP_MODE", Type: Set, Value: "ALWAYS", Hidden: true, Warning: "option '-S' is ambiguous and will be deprecated; use --snapshot or --partial"},
	{Verbs: verbsBackup, Flags: flags("--sync-direction"), Key: "SYNC_DIRECTION", Type: Set, Arg: "DIRECTION", Desc: "with two remote endpoints: PULL (default), PUSH, or 0 to proxy", Group: GroupConnect, Values: syncDirections},
	{Verbs: verbsBackup, Flags: flags("--push"), Key: "SYNC_DIRECTION", Type: Set, Value: "PUSH", Desc: "remote source connects to remote target", Group: GroupConnect},
	{Verbs: verbsBackup, Flags: flags("-t"), Key: "SYNC_DIRECTION", Type: Set, Value: "PUSH", Hidden: true, Warning: "option '-t' is deprecated; use '--push'"},
	{Verbs: verbsBackup, Flags: flags("--pull"), Key: "SYNC_DIRECTION", Type: Set, Value: "PULL", Desc: "remote target connects to remote source (default)", Group: GroupConnect},
	{Verbs: verbsBackup, Flags: flags("--no-pull"), Key: "SYNC_DIRECTION", Type: False, Desc: "proxy sync between remote endpoints", Group: GroupConnect},
	{Verbs: verbsBackup, Flags: flags("-T"), Key: "SYNC_DIRECTION", Type: Set, Value: "PULL", Hidden: true, Warning: "option '-T' is deprecated; use '--pull' (default)"},
	{Verbs: verbsBackup, Flags: flags("--initiator"), Key: "INITIATOR", Type: Invalid, Hidden: true, Warning: "--initiator is deprecated; use '--pull' (default) or '--push'"},
	{Verbs: verbsBackup, Flags: flags("--recv-pipe"), Key: "RECV_PIPE", Type: Set, Arg: "COMMAND", Desc: "pipe the stream through COMMAND, such as 'pv'", Group: GroupConnect},
	{Verbs: verbsBackup, Flags: flags("--parallel"), Key: "PARALLEL", Type: Set, Arg: "N", Desc: "sync up to N datasets at a time (default 1)", Group: GroupSync},
	{Verbs: verbsBackupNoFO, Flags: flags("--progress"), Type: Invalid, Hidden: true, Warning: "option '--progress' is deprecated; use '--recv-pipe'"},
	{Verbs: verbsBackupNoFO, Flags: flags("--rate-limit"), Type: Invalid, Hidden: true, Warning: "option '--rate-limit' is deprecated; use '--recv-pipe'"},
	{Verbs: verbsBackupNoFO, Flags: flags("-z"), Type: Invalid, Hidden: true, Warning: "option '-z' is deprecated; consider '--json'"},
	{Verbs: verbsBackupNoFO, Flags: flags("-h"), Key: "USAGE", Type: True, Hidden: true, Warning: "option '-h' is ambiguous; use '--holds' or see SEND/RECV options in 'zelta help options'"},
	{Verbs: verbsBackup, Flags: flags("--bookmark"), Key: "BOOKMARK_MODE", Type: Set, Value: "1", Desc: "bookmark successfully replicated source snapshots", Group: GroupBookmark},
	{Verbs: verbsBackup, Flags: flags("--no-bookmark"), Key: "BOOKMARK_MODE", Type: Set, Value: "0", Desc: "do not bookmark replicated source snapshots (default)", Group: GroupBookmark},
	{Verbs: verbsBackup, Flags: flags("--bookmark-mode"), Key: "BOOKMARK_MODE", Type: Set, Arg: "MODE", Desc: "set bookmark mode: 0 or 1", Group: GroupBookmark, Values: []string{"0", "1"}},
	{Verbs: verbsBackup, Flags: flags("--bookmark-prefix"), Key: "BOOKMARK_PREFIX", Type: Set, Arg: "PREFIX", Desc: "prefix for replication success bookmarks", Group: GroupBookmark},
	{Verbs: []string{"backup", "clone", "rebase", "failover"}, Flags: flags("--target-origin,--origin"), Key: "ORIGIN_ID", Type: Set, Arg: "ENDPOINT", Desc: "clone origin dataset already backed up on the target", Group: GroupSync, Complete: "endpoint"},

	// zelta revert.
	{Verbs: []string{"revert"}, Flags: flags("--force,-f"), Key: "REVERT_FORCE", Type: True, Desc: "attempt best-effort revert of an incomplete tree", Group: GroupCommand},

	// zelta rebase.
	{Verbs: []string{"rebase"}, Flags: flags("--rebase-file"), Key: "REBASE_FILE", Type: Set, Arg: "FILE", Desc: "override path to preserve file", Group: GroupCommand, Complete: "file"},

	// zelta failover and lock.
	{Verbs: []string{"failover", "lock"}, Flags: flags("--force,-f"), Key: "FORCE_UNMOUNT", Type: True, Desc: "force unmount during lock", Group: GroupCommand},
	{Verbs: []string{"failover", "lock"}, Flags: flags("--no-unmount"), Key: "NO_UNMOUNT", Type: True, Desc: "do not unmount locked datasets", Group: GroupCommand},

	// zfs send/receive options.
	{Verbs: verbsSendRecv, Flags: flags("-I,--intermediate"), Key: "SEND_INTR", Type: True, Desc: "intermediate mode: sync all possible snapshots (default)", Group: GroupSync},
	{Verbs: verbsSendRecv, Flags: flags("-i,--incremental"), Key: "SEND_INTR", Type: False, Desc: "incremental mode: sync only the latest snapshots", Group: GroupSync},
	{Verbs: verbsSendRecv, Flags: flags("--send-default"), Key: "SEND_DEFAULT", Type: Set, Arg: "OPTIONS", Desc: "zfs send options for unencrypted datasets", Group: GroupOverride},
	{Verbs: []string{"policy", "backup", "sync", "failover"}, Flags: flags("--send-decrypted"), Key: "SEND_DECRYPTED", Type: Set, Arg: "OPTIONS", Desc: "zfs send options when an encrypted dataset must be sent decrypted", Group: GroupOverride},
	{Verbs: verbsSendRecv, Flags: flags("--send-raw"), Key: "SEND_RAW", Type: Set, Arg: "OPTIONS", Desc: "zfs send options for encrypted datasets", Group: GroupOverride},
	{Verbs: verbsSendRecv, Flags: flags("--send-new"), Key: "SEND_NEW", Type: Set, Arg: "OPTIONS", Desc: "zfs send options for new datasets", Group: GroupOverride},
	{Verbs: verbsSendRecv, Flags: flags("--recv-default"), Key: "RECV_DEFAULT", Type: Set, Arg: "OPTIONS", Desc: "zfs recv options for all datasets", Group: GroupOverride},
	{Verbs: verbsSendRecv, Flags: flags("--recv-top"), Key: "RECV_TOP", Type: Set, Arg: "OPTIONS", Desc: "zfs recv options for the top dataset of a new backup", Group: GroupOverride},
	{Verbs: verbsSendRecv, Flags: flags("--recv-fs"), Key: "RECV_FS", Type: Set, Arg: "OPTIONS", Desc: "zfs recv options for new filesystems", Group: GroupOverride},
	{Verbs: verbsSendRecv, Flags: flags("--recv-vol"), Key: "RECV_VOL", Type: Set, Arg: "OPTIONS", Desc: "zfs recv options for new volumes", Group: GroupOverride},
	{Verbs: verbsSendRecv, Flags: flags("--recv-partial"), Key: "RECV_PARTIAL", Type: Set, Arg: "OPTIONS", Desc: "zfs recv options when resume is enabled", Group: GroupOverride},
	{Verbs: verbsSendRecv, Flags: flags("-o"), Key: "RECV_PROPS_ADD", Type: List, Arg: "PROPERTY=VALUE", Desc: "add 'zfs recv -o' property options", Group: GroupOverride},
	{Verbs: verbsSendRecv, Flags: flags("-x"), Key: "RECV_PROPS_DEL", Type: List, Arg: "PROPERTY", Desc: "add 'zfs recv -x' excluded properties", Group: GroupOverride},

	// zelta match.
	{Verbs: []string{"match"}, Flags: flags("-H"), Key: "SCRIPTING_MODE", Type: True, Desc: "suppress headers and separate columns with a tab", Group: GroupMatch},
	{Verbs: []string{"match"}, Flags: flags("-p"), Key: "PARSABLE", Type: True, Desc: "show sizes in exact numbers", Group: GroupMatch},
	{Verbs: []string{"match"}, Flags: flags("-o"), Key: "PROPLIST", Type: Set, Arg: "FIELD[,...]", Desc: "specify a list of match columns", Group: GroupMatch, Complete: "columns"},
	{Verbs: []string{"match"}, Flags: flags("--written"), Key: "LIST_WRITTEN", Type: True, Desc: "calculate sizes for datasets and snapshots (default)", Group: GroupMatch},
	{Verbs: []string{"match"}, Flags: flags("--no-written,--nowritten"), Key: "LIST_WRITTEN", Type: False, Desc: "skip size calculations for a faster listing", Group: GroupMatch},
	{Verbs: []string{"match"}, Flags: flags("--time"), Key: "CHECK_TIME", Type: True, Desc: "report the time of each zfs list", Group: GroupMatch},

	// zelta prune.
	{Verbs: []string{"prune"}, Flags: flags("--prune-time"), Key: "PRUNE_TIME", Type: Set, Arg: "DURATION", Desc: "keep snapshots newer than DURATION", Group: GroupPrune},
	{Verbs: []string{"prune"}, Flags: flags("--prune-num"), Key: "PRUNE_NUM", Type: Set, Arg: "N", Desc: "keep the newest N snapshots older than the match", Group: GroupPrune},
	{Verbs: []string{"prune"}, Flags: flags("--prune-grid"), Key: "PRUNE_GRID", Type: Set, Arg: "GRID", Desc: "GFS grid retention such as '30x1 day, 52x1 week, 1 year'", Group: GroupPrune},
	{Verbs: []string{"prune"}, Flags: flags("--match-endpoint,--guard-endpoint,--prune-guard-endpoint"), Key: "MATCH_ENDPOINT", Type: Set, Arg: "GUARD", Desc: "endpoint used for match validation and prune-guard", Group: GroupPrune, Complete: "endpoint"},
	{Verbs: []string{"prune"}, Flags: flags("--prune-guard"), Key: "PRUNE_GUARD", Type: Set, Arg: "MODE", Desc: "prune sync safety: latest, unsynced, or none", Group: GroupPrune, Values: pruneGuards},
	{Verbs: []string{"prune"}, Flags: flags("--no-prune-guard"), Key: "PRUNE_GUARD", Type: Set, Value: "none", Desc: "disable match-endpoint safety checks", Group: GroupPrune},
	{Verbs: []string{"prune"}, Flags: flags("--force,-f"), Key: "PRUNE_FORCE", Type: True, Desc: "destroy without asking for confirmation (zprune)", Group: GroupPrune},
	{Verbs: []string{"prune"}, Flags: flags("--prune-size"), Key: "PRUNE_SIZE", Type: Set, Arg: "SIZE", Desc: "select oldest eligible snapshots until SIZE is reclaimed", Group: GroupPrune},
	{Verbs: []string{"prune"}, Flags: flags("--no-ranges"), Key: "NO_RANGES", Type: True, Desc: "output individual snapshots instead of ranges", Group: GroupPrune},
	{Verbs: []string{"prune"}, Flags: flags("--visual"), Key: "PRUNE_VISUAL", Type: True, Desc: "print snapshot keep/destroy symbols in creation order", Group: GroupPrune},
	{Verbs: []string{"prune"}, Flags: flags("--prune-name"), Key: "PRUNE_NAME", Type: Set, Arg: "PATTERN", Desc: "snapshot name pattern: zfsnap, truenas, sanoid, or a glob", Group: GroupPrune, Values: pruneNames},
	{Verbs: []string{"prune"}, Flags: flags("--send-range"), Key: "SEND_RANGE", Type: Set, Arg: "SNAPSHOT", Desc: "internal: list source snapshots after the given match", Group: GroupPrune, Hidden: true},

	// zelta policy.
	{Verbs: []string{"policy"}, Flags: flags("-H"), Key: "NO_HEADER", Type: True, Desc: "scripting mode: no header, space-delimited dry-run table", Group: GroupPolicy},
	{Verbs: []string{"policy"}, Flags: flags("--config,-C"), Key: "CONFIG", Type: Set, Arg: "FILE", Desc: "read the policy configuration from FILE", Group: GroupPolicy, Complete: "file"},
	{Verbs: []string{"policy"}, Flags: flags("--save"), Key: "POLICY_SAVE", Type: Set, Arg: "PATTERN", Desc: "save the last backup matching PATTERN (see zelta replay) as a job", Group: GroupPolicy},
	{Verbs: []string{"policy"}, Flags: flags("--rm"), Key: "POLICY_RM", Type: Set, Arg: "PATTERN", Desc: "remove the saved job matching PATTERN", Group: GroupPolicy},
	{Verbs: []string{"policy"}, Flags: flags("--import"), Key: "POLICY_IMPORT", Type: Set, Arg: "FILE", Desc: "import the jobs of a zelta.conf FILE", Group: GroupPolicy, Complete: "file"},
	{Verbs: []string{"policy"}, Flags: flags("--retry"), Key: "RETRY", Type: Set, Optional: "1", Arg: "N", Desc: "retry failed syncs N times (default 1)", Group: GroupPolicy},
	{Verbs: []string{"policy"}, Flags: flags("--no-retry,--noretry"), Key: "RETRY", Type: False, Desc: "do not retry failed syncs", Group: GroupPolicy},
	{Verbs: []string{"policy"}, Flags: flags("--jobs"), Key: "JOBS", Type: Set, Arg: "N", Desc: "run policy jobs for up to N sites concurrently", Group: GroupPolicy},
	{Verbs: []string{"policy"}, Flags: flags("--no-threads"), Key: "JOBS", Type: False, Desc: "run policy jobs one at a time", Group: GroupPolicy},
	{Verbs: []string{"policy"}, Flags: flags("--backup-root"), Key: "BACKUP_ROOT", Type: Set, Arg: "ENDPOINT", Desc: "default target root for policy jobs", Group: GroupPolicy, Complete: "endpoint"},
	{Verbs: []string{"policy"}, Flags: flags("--archive-root"), Key: "ARCHIVE_ROOT", Type: Set, Arg: "ENDPOINT", Desc: "default archive root for rotated datasets", Group: GroupPolicy, Complete: "endpoint"},
	{Verbs: []string{"policy"}, Flags: flags("--host-prefix"), Key: "ADD_HOST_PREFIX", Type: Set, Optional: "1", Arg: "0|1", Desc: "include the source hostname in target names", Group: GroupPolicy},
	{Verbs: []string{"policy"}, Flags: flags("--ds-prefix,--prefix"), Key: "ADD_DATASET_PREFIX", Type: Set, Arg: "N", Desc: "include N parent dataset labels in target names (-1 for all)", Group: GroupPolicy},
	{Verbs: []string{"policy"}, Flags: flags("--backup-command"), Key: "BACKUP_COMMAND", Type: Set, Arg: "COMMAND", Desc: "command used to run backup jobs (for wrappers and testing)", Group: GroupPolicy},
}

// Legacy maps deprecated option keys to their replacements.
var Legacy = []struct {
	Old, New, Warning string
}{
	{"SNAPSHOT", "SNAP_MODE", ""},
	{"INTERMEDIATE", "SEND_INTR", ""},
	{"INTR_FLAGS", "SEND_INTR", "INTR_FLAGS will be deprecated and renamed to SEND_INTR."},
	{"SEND_FLAGS", "SEND_OVERRIDE", "SEND_FLAGS will be deprecated. See 'zelta help options' for new SEND options."},
	{"RECEIVE_FLAGS", "RECV_OVERRIDE", "RECEIVE_FLAGS will be deprecated. See 'zelta help options' for new RECV options."},
	{"REMOTE_SEND_COMMAND", "REMOTE_SEND", ""},
	{"REMOTE_RECV_COMMAND", "REMOTE_RECV", ""},
	{"THREADS", "JOBS", ""},
	{"HOST_PREFIX", "ADD_HOST_PREFIX", ""},
	{"PREFIX", "ADD_DATASET_PREFIX", ""},
	{"RECEIVE_PREFIX", "RECV_PIPE", ""},
	{"RECV_PREFIX", "RECV_PIPE", ""},
	{"WRITTEN", "LIST_WRITTEN", ""},
}

// EnvOnlyKeys are options without command-line flags that are still valid in
// zelta.env, the environment, and policy files.
var EnvOnlyKeys = []string{
	"REMOTE_COMMAND", "REMOTE_DEFAULT", "REMOTE_SEND", "REMOTE_RECV",
	"CREATE_PARENT", "LIST_WRITTEN", "SEND_REPLICATE", "CLONE_DEFAULT",
	"LOG_PREFIX", "SYSTIME", "JSON_PRETTY", "SNAP_MODE", "REMOTE_MULTIPLEX",
	"CACHE_DIR", "CACHE_TIMEOUT",
	"REPORT_MESSAGE_DEFAULT", "REPORT_MESSAGE_NONE", "REPORT_MESSAGE_ALL", "REPORT_MESSAGE_SOME", "REPORT_MESSAGE_OK",
	"REPORT_COMMAND_DEFAULT", "REPORT_COMMAND_NONE", "REPORT_COMMAND_ALL", "REPORT_COMMAND_SOME", "REPORT_COMMAND_OK",
}

// HasVerb reports whether the spec applies to verb.
func (s *Spec) HasVerb(verb string) bool {
	return slices.Contains(s.Verbs, "all") || slices.Contains(s.Verbs, verb)
}

// SpecsFor returns the specs that apply to verb, in registry order.
func SpecsFor(verb string) []*Spec {
	var out []*Spec
	for _, s := range Specs {
		if s.HasVerb(verb) {
			out = append(out, s)
		}
	}
	return out
}

// PolicyScoped reports whether key only affects 'zelta policy' itself and
// must not be forwarded to backup jobs.
func PolicyScoped(key string) bool {
	for _, s := range Specs {
		if s.Key == key && len(s.Verbs) == 1 && s.Verbs[0] == "policy" {
			return true
		}
	}
	return false
}

// PolicyKey reports whether key may appear in a policy file.
func PolicyKey(key string) bool {
	for _, s := range Specs {
		if s.Key == key && s.Key != "USAGE" && s.Key != "VERB" && (s.HasVerb("policy")) {
			return true
		}
	}
	return slices.Contains(EnvOnlyKeys, key) || knownDefault(key)
}

// KeyType returns the type of the first spec controlling key, and whether
// the key is controlled by any flag.
func KeyType(key string) (Type, bool) {
	for _, s := range Specs {
		if s.Key == key {
			return s.Type, true
		}
	}
	return Set, false
}

// LegacyKey returns the replacement for a deprecated key and its warning.
func LegacyKey(key string) (string, string, bool) {
	for _, l := range Legacy {
		if l.Old == key {
			return l.New, l.Warning, true
		}
	}
	return "", "", false
}
