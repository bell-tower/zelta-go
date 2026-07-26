# Upstream TODOs

These items belong in the Bourne+AWK lineage first. Do not treat the current
Go implementation as authoritative until the upstream contract is corrected
and captured by a test.

## Send-check and NetBSD receive compatibility

- Verify and correct upstream `send-check`; the current behavior is not trusted
  to work reliably.
- NetBSD does not support `zfs recv -x ...`; the generated receive command
  must avoid or feature-detect receive-property exclusion flags.
- `zfs send -e` fails when feature flags do not match; send-check
  must detect this case correctly and remove `-e` when appropriate rather than
  treating the probe as a generic success/failure.
- Reproduce against `root@netbsd` with pools `apool` and `bpool`, then add a
  deterministic regression fixture before porting the corrected behavior here.

This remains upstream-only work for now and must not block the Go line.

## Interrupted receive fixture

- Add a deterministic Bourne+AWK/integration fixture that interrupts a
  recursive `zfs recv -s`, discovers `receive_resume_token` per dataset, and
  resumes with `zfs send -t` without a blind ordinary retry.
- Verify the fixture on disposable Linux and BSD ZFS before treating the Go
  resume implementation as cross-lineage evidence.

## Rotate confirmation strictness

- Upstream `zelta rotate` reloads source/target state after rotation and reports
  remaining divergence, but does not make a failed up-to-date confirmation a
  separate non-zero status.
- If strict post-rotation confirmation is desired, add and document an
  upstream option first; do not invent a Go-only failure mode.
