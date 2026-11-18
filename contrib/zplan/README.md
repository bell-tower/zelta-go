# zplan

An interactive planner for `zelta prune`. Tune retention and filters and
see the fate of every snapshot as you type: each change re-plans with
Zelta. zplan never destroys anything. Enter prints the `zprune` command
for the plan, which previews and confirms again before destroying.

```sh
go install zelta.space/contrib/zplan@latest
zplan --prune-num=7 backup:tank/vm tank/vm
```

```
usage: zplan [OPTIONS] ENDPOINT [GUARD]
```

Options are `zelta prune`'s: `--match-endpoint`, `--prune-num`,
`--prune-time`, `--prune-grid`, `--prune-size`, `--prune-guard`,
`--no-prune-guard`, `--depth`/`-d`, `--exclude`/`-X`, and `--include`. They
set the starting values; every one but the guard endpoint is editable in
the planner. ZELTA_* variables and `zelta.env` apply as for `zelta`.

## The view

Each snapshot is a colored cell, oldest first: red is destroyed, blue is
kept by retention, cyan is at or newer than the guard match.

- **Several datasets:** one line each, all at the same scale.
- **One dataset:** a block of up to 1000 cells, each row labeled with the
  date of its first snapshot.
- When there are more snapshots than cells, each cell holds several and
  shows its two most common fates as a split: the older on the left,
  filled in eighths.

Colors are the terminal's own 16-color palette and the only backgrounds
are those splits, so light, dark, and solarized themes all work. With
`NO_COLOR` set, cells are shades: █ kept, ▓ guarded, ░ destroyed. Without a
terminal, zplan prints the plan once, in shades, and the command.

## Keys

| Key | Does |
|-----|------|
| ↑ ↓ Tab | Move between fields |
| ← → | Cycle the field's presets |
| typing, Backspace, ^U | Edit; ^U clears |
| ↓ past the last field | Select datasets; ↑ past the first goes back |
| → Space, ← | Zoom into the selected dataset, and back out |
| PgUp PgDn | Page through datasets |
| Enter | Quit and print the `zprune` command |
| Esc | Zoom out, then quit; ^C quits |
