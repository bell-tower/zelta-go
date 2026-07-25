# Bounded Session Protocol

This repo is optimized for cheap, narrow implementation loops. The editor must
choose a bounded task before reading deeply or changing files.

## Choose the model by task

### Tight-loop model

Use for:

- one failing test;
- one golden mismatch;
- one package-level bug;
- formatting, wiring, or a small refactor with an existing contract.

Required output: changed files, focused test, full relevant test result, and
one next task. No architecture expansion.

### High-context model

Use for:

- extracting a bounded contract from Awk or docs;
- reviewing the architecture or agent system;
- designing a capability/test matrix;
- comparing competing API boundaries;
- reviewing safety or release readiness.

Required output: a short decision or contract with exact files, assumptions,
and tests. It should not implement a broad feature merely because it can ingest
the repository.

## Session opening

1. Read root `AGENTS.md`, `agents/11-roadmap.md`, and one relevant operation
   document.
2. Check `git status` and recent commits.
3. State each specified bounded task, files likely to change, and its stop condition.
4. Inspect only the code and fixtures needed for that task.

## Scope box

- One package or one tightly coupled CLI path.
- One contract or one behavior change.
- No new verb unless explicitly requested.
- No broad cleanup while fixing a test.
- No golden regeneration without explicit intent and an oracle comparison.
- No real ZFS or destructive command without explicit approval and a disposable
  target.

## Required loop

1. Run the smallest relevant test before editing when possible.
2. Make the smallest implementation change.
3. Run the focused test.
4. Run broader tests, vet, or build only if relevant to the change.
5. Review the diff for unrelated changes and inspect status.
6. Record an entry in `agents/10-deviations.md` only for intentional behavior
   differences or documented known gaps.
7. Stop and report after the specified bounded loops. Do not select additional roadmap work automatically.

## Escalation triggers

Stop and ask for direction when:

- the task needs more than one package without an existing contract;
- a golden and the stated contract disagree;
- a safety behavior is ambiguous;
- a test command runs longer than expected or begins touching real systems;
- the proposed change would alter a public API or data-table schema;
- the implementation would make an incomplete verb look release-ready.
