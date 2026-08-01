// Package zlog is the leveled log sink for the zelta CLI.
//
// It mirrors the Awk oracle's report() → `zelta ipc-log` pipeline in-process:
// every message carries a level 0-4, messages above LOG_LEVEL are dropped,
// and formatting follows bin/zelta `zelta_log()`:
//
//   - LOG_FILE set: every line prefixed (error:/warning:/notice:/info:/debug:)
//     and appended to the file.
//   - Otherwise LOG_MODE "" or "text" (terminal): notice → stdout unprefixed,
//     info → stderr unprefixed, all other levels → prefixed to stderr.
//   - Otherwise (LOG_MODE json): every line prefixed to stderr.
//
// LOG_PREFIX prepends job context to every message (policy children).
// LOG_MODE=text drops the prefix (oracle quirk).
//
// Library callers (match, backup) may pass a *Sink via Request; a nil sink
// disables leveled output entirely.
package zlog
