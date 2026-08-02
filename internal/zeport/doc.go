// Package zeport implements the experimental zelta report / zeport verb:
// 24h snapshots_changed staleness checks on backup roots, templated NOTICE
// messages, and optional shell hooks (REPORT_MESSAGE_*, REPORT_COMMAND_*).
//
// CLI-only process concern — not part of the public Action Library.
// Distinct from internal/report (match cols + backup JSON presentation).
package zeport
