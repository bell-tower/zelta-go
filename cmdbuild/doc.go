// Package cmdbuild turns data/cmds.tsv into zfs argv plans.
//
// Template rows carry action, remote role, command, static args, and var
// slots; Build expands vars into argv with zfs flag conventions. No shell
// recursion, no env, no argv — a public building block for actions and CLI
// alike (backup, rotate, lineage, zfs executor).
package cmdbuild
