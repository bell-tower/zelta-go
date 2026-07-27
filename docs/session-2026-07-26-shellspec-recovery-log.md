# Session recovery log — ShellSpec / rotate-after-revert (2026-07-26)

**Purpose:** Preserve useful findings from the ShellSpec port session **before** the
destructive “force full + always `-F`” wrong turn. That wrong turn is being
**erased from git history** (reset past those commits). This file is the Lethe
exception: memory without the bad code.

**Status of history rewrite:** Commits that introduced unconditional `zfs recv -F`
and unblocking `blocked sync: no target snapshots` into full sends are removed
from `main` via hard reset + force push. Do not reintroduce them.

---

## 1. Context

- **Goal:** Port Richard’s ShellSpec suite from `zelta-awk` to `zelta-go`; green
  GitHub CI (`go.yml`, `shell.yml`, `shellspec.yml`).
- **Lab:** macOS has no ZFS. Real ZFS work on `root@debian` and GitHub
  `ubuntu-latest` + `zfsutils-linux`.
- **Remotes:** Gitea `git@git.belltower.it:djbell/zelta-go.git`; GitHub mirror
  `bell-tower/zelta-go`. Scratch under repo-local `./tmp/` only.

---

## 2. Progress that was real (pre-lobotomy)

ShellSpec standard scenario moved roughly **44 ok / 7 not ok → ~50 ok** with
legitimate CLI/output/plan fixes — **not** force-recv.

| Area | Finding / fix direction | Notes |
|------|-------------------------|--------|
| Dry-run + JSON | Dry-run plan text → **stderr**; JSON path must not be `else if` chained off dry-run | Oracle-style: human dry-run and machine JSON are independent |
| Carriage return | Strip `\r` in `endpoint.Parse`; surface CR host as match/JSON warning | Spec: sanitized JSON CR test |
| Encrypted raw fallback | Suppress noisy “raw send unavailable” style stderr when fallback is intentional | Specs 28/34 class |
| Revert `-qq` | Suppress “to retain replica history…” when `LOG_LEVEL <= 0` | Spec quiet revert |
| Rotate wording (basic) | Expect lines like `source is written; snapshotting`, `renaming … to …_start`, streams line | Spec `040` `output_for_rotate_after_divergence` expects preserve suffix **`_start`** (match point), **not** SrcLast |
| Space in dataset names | `recv -o origin=…` must not be one argv blob that splits on spaces | Prefer separate argv: `-o`, `origin=value` (cmdbuild expandVar / rotate plan) |
| Install / sandbox | Go binary embeds logic; `check_install` must not require Awk share files; sandbox under `./tmp/zelta_sandbox_*` | `install.sh` man under `doc/` or `cmd/zelta/doc/` |
| Parent create | Missing **target parent** → create with canmount=noauto (retry readonly OpenZFS quirk) | Target **dataset missing** → `syncable (full)`; existing target with **0 snaps** → **blocked** |

Go unit tests were green on the legitimate path before the `-F` episode.

---

## 3. Test 31 / rotate-after-revert — useful observations

Spec: `test/spec/0100_standard/050_zelta_revert_spec.sh`

Sequence (after 022 tree + divergence):

1. `zelta snapshot --snap-name manual_test` SRC  
2. `add_tree_delta` (e.g. sub5)  
3. `zelta backup` SRC TGT  
4. `zelta snapshot --snap-name another_test` SRC  
5. `zelta revert -qq SRC@manual_test`  
6. `zelta rotate` SRC TGT  

### Spec expectations (oracle-shaped)

`output_for_rotate_after_revert` accepts stdout lines like:

- `renaming 'TGT' to 'TGT_zelta_*'`  
- `to ensure target is up-to-date, run: zelta backup …`  
- optional streams timing line  

**Important:** Spec `040` (rotate after **divergence**, not after revert) expects
rename to `TGT_start` (common match `@start`). Spec `050` expects `TGT_zelta_*`.
Those are different scenarios; **do not** unify suffixes with a single “always
SrcLast” hack.

### Revert lineage (Go)

`lineage.RevertPlan` roughly:

1. Rename live tree → `dataset_<snap>` (preserve)  
2. Clone preserve@snap → restore live names  
3. Optional post-revert recursive snapshot (`AfterSnapshot` / default zelta name)

After revert, source often has:

- `origin = <preserved_dataset>@manual_test` (or similar)  
- A new post-revert `@zelta_*` as SrcLast  
- **No** GUID match to target’s older `@zelta_*` from the prior backup  

Rotate contract (`agents/zelta-go/12-rotate.md`):

1. Match tree  
2. If target diverged → `zfs rename -fp` preserve name **tied to matching snapshot**  
3. Direct common snap → seed full of common snap onto **new** target path, then incr  
4. Else verify **source origin** snapshot exists on **preserved target** side  
5. `zfs recv -o origin=<preserved>@<snap>` for clone-origin path  
6. **Refuse** unsafe: no match/origin, missing origin snap, up-to-date source  

**Never** “just `zfs recv -F` into the live divergent target.”

### Failure modes seen in clean-ish runs (informative, not prescriptions)

- `rotate has no verified common snapshot or source origin` when root pair has
  empty Match **and** origin snap is not present on the target rows used for
  verification.  
- Dirty lab state (leftover `*_manual_test`, pre-created empty roots, residual
  clones) **confounds** diagnosis. Always destroy/recreate pools between
  theories.  
- Match after revert often shows **blocked / diverged** across the tree — that
  is expected input to **rotate**, not a cue to force-backup blocked pairs.

### What was **wrong** about the “root never gets snapshots” theory

- Zelta does **not** rely on `zfs send -R` for tree backup.  
- Product model: **`zelta match` per dataset** → plan per pair → send/recv.  
- That is **more flexible** than recursive send, not a bug vs Awk.  
- `blocked sync: no target snapshots` means: **both** datasets exist, target has
  zero snapshots → **do not send** (safety).  
- Missing target dataset → `syncable (full)`. Parent create is separate.  
- Contaminated debian state (existing empty `bpool/backups`, leftover preserves)
  produced symptoms misread as “must `-F` full into existing root.”

---

## 4. Contract violations that were introduced (and erased)

Do **not** re-land:

1. **`ApplySourceSnap`:** convert `blocked sync: … no target snapshots` → `KindFull`  
   - Violates `04-backup.md`: blocked = **no force**.  
2. **`recvFlags`:** unconditional `-F` on every full receive  
   - Violates README safety: no forced overwrites; rotate preserves via rename/clone.  
   - Changes option-passing vs Awk RECV_TOP/FS/VOL defaults (no default `-F`).  
3. **Preserve suffix = SrcLast when Match empty** solely to green TAP  
   - Papered over rotate naming; fights `12-rotate.md` (name tied to match/origin).  
4. **Spec-only** `The error should include "warning"` to silence ShellSpec WARNED  
   - Hides symptoms; does not fix planner.

Agent doc quotes (keep sacred):

- Backup plan: `blocked…` → Blocked | **no force**  
- Safety: **No force-overwrite of divergent targets**  
- Recv defaults: readonly + FS flags + partial `-s` — **not** `-F`  
- Rotate: preserve divergent target with rename; continuity **without** destructive receive  

---

## 5. Awk / product prime directives (from zelta-awk README)

Extracted for AGENTS guardrails:

- **Compliance:** preserve every version; when source and backup diverge, **keep both**.  
- **Safe defaults:** backups read-only; dangerous ops **rejected**, not discouraged.  
- **No destructive features;** never **requires force flags** to function correctly.  
- **`zelta revert`:** rewind **in place by renaming and cloning** — nondestructive
  alternative to a bare `zfs rollback` that would discard later snaps.  
- **`zelta rotate`:** multi-way rename/clone so backups keep rolling after
  divergence; **preserves all versions without destructive receives**.  
- **No forced overwrites:** rotate preserves divergent datasets **before**
  receiving new history — not `zfs receive -F` as the continuity mechanism.  
- **Remote + recursive on backup sets**, with **per-dataset** intelligence
  (match/report discrepancies) rather than blind `zfs send -R` only.  
- **Environment-agnostic:** decisions from metadata/features, not naming
  conventions alone.

---

## 6. ShellSpec / CI notes worth keeping

- Workflow: `shellspec.yml` — `noop` job + `standard` job (ZFS pools, testuser).  
- Runner: `./test/run_tests.sh standard` → tags `install,initialize,standard,cleanup`.  
- Env: `SANDBOX_ZELTA_SRC_POOL/TGT_POOL`, `SRC_DS/TGT_DS`, `SANDBOX_ZELTA_TMP_SUFFIX`.  
- Specs auto-generated; manual edits may be lost — prefer fixing **Go** to match
  oracle, not watering down specs.  
- `040` rotate: may **expect** stderr  
  `warning: insufficient snapshots; performing full backup for N datasets`  
  with a specific N (oracle count). Don’t “fix” by changing N via force-full
  of blocked pairs.  
- Debian: install Go ≥ module version (1.22+); or cross-build from macOS and
  `scp` binary to `/usr/local/bin/zelta`. Dirty pools → `zpool destroy` + recreate.

---

## 7. Likely honest next diagnosis path (after history rewrite)

1. Clean pools on debian; **no** leftover `*_manual_test`.  
2. Side-by-side **Awk vs Go** on same tree for steps 1–6 of §3.  
3. Dump `match -H`, origins, snap lists, `rotate -n` plans.  
4. Diff **plans** against `12-rotate.md` (seed common snap, origin recv, refuse).  
5. Fix only confirmed Go≠Awk gaps; **ask** before any force/unblock behavior.  
6. Preserve-name / stdout wording last, and only if oracle output differs for the
   same safe plan.

---

## 8. Git history note

Commits **removed from `main` tip** (names may vary after force-push):

- Introduced `-F` + unblock no-target-snapshots + SrcLast preserve suffix  
- Follow-on test expectation churn for `-F`  
- Match/SrcLast suffix toggle  
- Spec stderr paper  

**This log file** is the intentional remnant of that episode.

---

## 9. Reminder for future agents

If match says **blocked**, backup must **not** send that pair.  
If continuity after divergence is needed, that is **`zelta rotate`** (rename +
clone-origin / seeded incremental), **not** `zfs recv -F`.  
If the lab is dirty, **reset the lab**, do not change the product.
