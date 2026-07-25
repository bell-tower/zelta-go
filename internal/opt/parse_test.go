package opt

import (
	"strings"
	"testing"
)

func parse(t *testing.T, verb string, argv ...string) *Parsed {
	t.Helper()
	p, err := Parse(verb, argv)
	if err != nil {
		t.Fatalf("Parse(%s %v): %v", verb, argv, err)
	}
	return p
}

func TestParseBasicSet(t *testing.T) {
	p := parse(t, "backup", "--dryrun", "--log-level=4", "tank/src", "tank/tgt")
	if p.Env.Get("DRYRUN") != "1" {
		t.Errorf("DRYRUN = %q", p.Env.Get("DRYRUN"))
	}
	if p.Env.Get("LOG_LEVEL") != "4" {
		t.Errorf("LOG_LEVEL = %q", p.Env.Get("LOG_LEVEL"))
	}
	if len(p.Operands) != 2 || p.Operands[0] != "tank/src" {
		t.Errorf("Operands = %v", p.Operands)
	}
	// Defaults visible (oracle ipc-env shows RESUME=1 SEND_INTR=1 SNAP_MODE=IF_NEEDED)
	if p.Env.Get("RESUME") != "1" || p.Env.Get("SEND_INTR") != "1" || p.Env.Get("SNAP_MODE") != "IF_NEEDED" {
		t.Errorf("defaults: RESUME=%q SEND_INTR=%q SNAP_MODE=%q",
			p.Env.Get("RESUME"), p.Env.Get("SEND_INTR"), p.Env.Get("SNAP_MODE"))
	}
}

func TestParseNegations(t *testing.T) {
	p := parse(t, "backup", "--no-resume", "--no-snapshot", "a", "b")
	if p.Env.Get("RESUME") != "0" {
		t.Errorf("RESUME = %q", p.Env.Get("RESUME"))
	}
	if p.Env.Get("SNAP_MODE") != "0" {
		t.Errorf("SNAP_MODE = %q", p.Env.Get("SNAP_MODE"))
	}
}

func TestParseReceiveProperties(t *testing.T) {
	p := parse(t, "backup", "-o", "compression=lz4", "-o", "quota=10G", "-x", "mountpoint", "src", "tgt")
	if got := p.Env.List("RECV_PROPS_ADD"); len(got) != 2 || got[0] != "compression=lz4" || got[1] != "quota=10G" {
		t.Fatalf("RECV_PROPS_ADD=%v", got)
	}
	if got := p.Env.List("RECV_PROPS_DEL"); len(got) != 1 || got[0] != "mountpoint" {
		t.Fatalf("RECV_PROPS_DEL=%v", got)
	}
}

func TestParseAccumulation(t *testing.T) {
	// Oracle: -vv -X '*/tmp' --exclude=@hourly --snap-name=daily_1 -L --raw
	// → LOG_LEVEL=4, EXCLUDE='*/tmp,@hourly', SNAP_NAME=daily_1, SEND_OVERRIDE='-L --raw'
	p := parse(t, "backup", "-vv", "-X", "*/tmp", "--exclude=@hourly", "--snap-name=daily_1", "-L", "--raw", "a", "b")
	if p.Env.Get("LOG_LEVEL") != "4" {
		t.Errorf("LOG_LEVEL = %q", p.Env.Get("LOG_LEVEL"))
	}
	if p.Env.Get("EXCLUDE") != "*/tmp,@hourly" {
		t.Errorf("EXCLUDE = %q", p.Env.Get("EXCLUDE"))
	}
	if p.Env.Get("SNAP_NAME") != "daily_1" {
		t.Errorf("SNAP_NAME = %q", p.Env.Get("SNAP_NAME"))
	}
	if p.Env.Get("SEND_OVERRIDE") != "-L --raw" {
		t.Errorf("SEND_OVERRIDE = %q", p.Env.Get("SEND_OVERRIDE"))
	}
}

func TestParseIncrSeedsFromEnv(t *testing.T) {
	// Oracle: ZELTA_LOG_LEVEL=0 + -vq --log-level 3 → LOG_LEVEL=3
	t.Setenv("ZELTA_LOG_LEVEL", "0")
	p := parse(t, "match", "-vq", "--log-level", "3", "a", "b")
	if p.Env.Get("LOG_LEVEL") != "3" {
		t.Errorf("LOG_LEVEL = %q", p.Env.Get("LOG_LEVEL"))
	}
}

func TestParseOperandEndsFlags(t *testing.T) {
	// Oracle: match tank/a --written tank/b → TGT_ID=--written
	p := parse(t, "match", "tank/a", "--written", "tank/b")
	if len(p.Operands) != 3 || p.Operands[1] != "--written" {
		t.Errorf("Operands = %v", p.Operands)
	}
	if p.Changed["LIST_WRITTEN"] {
		t.Error("--written after operand must not apply")
	}
}

func TestParseDoubleDash(t *testing.T) {
	p := parse(t, "match", "-H", "--", "-p", "a", "b")
	if p.Env.Get("SCRIPTING_MODE") != "1" {
		t.Errorf("SCRIPTING_MODE = %q", p.Env.Get("SCRIPTING_MODE"))
	}
	if len(p.Operands) != 3 || p.Operands[0] != "-p" {
		t.Errorf("Operands = %v", p.Operands)
	}
}

func TestParseShortClusterValues(t *testing.T) {
	p := parse(t, "match", "-d2", "a", "b")
	if p.Env.Get("DEPTH") != "2" {
		t.Errorf("DEPTH = %q", p.Env.Get("DEPTH"))
	}
	p = parse(t, "match", "-nq", "-d", "5", "a", "b")
	if p.Env.Get("DRYRUN") != "1" || p.Env.Get("LOG_LEVEL") != "1" || p.Env.Get("DEPTH") != "5" {
		t.Errorf("DRYRUN=%q LOG_LEVEL=%q DEPTH=%q",
			p.Env.Get("DRYRUN"), p.Env.Get("LOG_LEVEL"), p.Env.Get("DEPTH"))
	}
}

func TestParseSyncDirection(t *testing.T) {
	p := parse(t, "backup", "--push", "a", "b")
	if p.Env.Get("SYNC_DIRECTION") != "PUSH" {
		t.Errorf("SYNC_DIRECTION = %q", p.Env.Get("SYNC_DIRECTION"))
	}
	p = parse(t, "backup", "--no-pull", "a", "b")
	if p.Env.Get("SYNC_DIRECTION") != "0" {
		t.Errorf("SYNC_DIRECTION = %q", p.Env.Get("SYNC_DIRECTION"))
	}
	p = parse(t, "backup", "-t", "a", "b")
	if p.Env.Get("SYNC_DIRECTION") != "PUSH" || len(p.Warnings) == 0 {
		t.Errorf("-t: SYNC_DIRECTION=%q warnings=%v", p.Env.Get("SYNC_DIRECTION"), p.Warnings)
	}
}

func TestParseInvalidOptions(t *testing.T) {
	if _, err := Parse("backup", []string{"--bogus", "a", "b"}); err == nil ||
		!strings.Contains(err.Error(), "invalid option '--bogus'") {
		t.Errorf("--bogus: %v", err)
	}
	if _, err := Parse("backup", []string{"--initiator", "a", "b"}); err == nil ||
		!strings.Contains(err.Error(), "deprecated") {
		t.Errorf("--initiator: %v", err)
	}
	if _, err := Parse("match", []string{"--dryrun=x", "a", "b"}); err == nil ||
		!strings.Contains(err.Error(), "invalid option assignment") {
		t.Errorf("--dryrun=x: %v", err)
	}
	// Verb gating: --prune-num is not a match option.
	if _, err := Parse("match", []string{"--prune-num=3", "a", "b"}); err == nil {
		t.Error("--prune-num must be invalid for match")
	}
}

func TestParseDeprecatedShortWarnings(t *testing.T) {
	// -F: arglist + destructive warning
	p := parse(t, "backup", "-F", "a", "b")
	if p.Env.Get("SEND_OVERRIDE") != "-F" {
		t.Errorf("SEND_OVERRIDE = %q", p.Env.Get("SEND_OVERRIDE"))
	}
	if len(p.Warnings) == 0 || !strings.Contains(p.Warnings[0], "destructive") {
		t.Errorf("-F warnings: %v", p.Warnings)
	}
	// -h on backup: usage; NO warning (oracle data quirk: the ambiguous text
	// sits in the DESCRIPTION column, WARNING $8 is empty).
	p = parse(t, "backup", "-h")
	if !p.Usage {
		t.Error("-h must set USAGE")
	}
	if len(p.Warnings) != 0 {
		t.Errorf("-h warnings: %v (oracle emits none)", p.Warnings)
	}
}

func TestParseLegacyAlias(t *testing.T) {
	t.Setenv("ZELTA_SEND_FLAGS", "-L --raw")
	p := parse(t, "backup", "a", "b")
	if p.Env.Get("SEND_OVERRIDE") != "-L --raw" {
		t.Errorf("SEND_OVERRIDE = %q", p.Env.Get("SEND_OVERRIDE"))
	}
	if len(p.Warnings) == 0 || !strings.Contains(p.Warnings[0], "SEND_FLAGS") {
		t.Errorf("legacy warnings: %v", p.Warnings)
	}
	// Oracle quirk: alias OVERWRITES the primary key (NewOpt[$3] = Opt[$4]).
	t.Setenv("ZELTA_SEND_OVERRIDE", "-c")
	p = parse(t, "backup", "a", "b")
	if p.Env.Get("SEND_OVERRIDE") != "-L --raw" {
		t.Errorf("SEND_OVERRIDE = %q, want alias to win (oracle)", p.Env.Get("SEND_OVERRIDE"))
	}
}

func TestParseEnvNormalization(t *testing.T) {
	t.Setenv("ZELTA_RESUME", "no")
	p := parse(t, "backup", "a", "b")
	if p.Env.Get("RESUME") != "0" {
		t.Errorf("RESUME = %q, want 0 (no→0)", p.Env.Get("RESUME"))
	}
	// Empty export counts as unset → default wins.
	t.Setenv("ZELTA_SYNC_DIRECTION", "")
	p = parse(t, "backup", "a", "b")
	if p.Env.Get("SYNC_DIRECTION") != "PULL" {
		t.Errorf("SYNC_DIRECTION = %q, want PULL", p.Env.Get("SYNC_DIRECTION"))
	}
}

func TestParseMatchFlags(t *testing.T) {
	p := parse(t, "match", "-H", "-p", "-o", "name,guid", "--no-written", "--time", "a", "b")
	if p.Env.Get("SCRIPTING_MODE") != "1" || p.Env.Get("PARSABLE") != "1" {
		t.Errorf("H/p: %q %q", p.Env.Get("SCRIPTING_MODE"), p.Env.Get("PARSABLE"))
	}
	if p.Env.Get("PROPLIST") != "name,guid" {
		t.Errorf("PROPLIST = %q", p.Env.Get("PROPLIST"))
	}
	if p.Env.Get("WRITTEN") != "0" || p.Env.Get("CHECK_TIME") != "1" {
		t.Errorf("WRITTEN=%q CHECK_TIME=%q", p.Env.Get("WRITTEN"), p.Env.Get("CHECK_TIME"))
	}
}

func TestParsePruneFlags(t *testing.T) {
	p := parse(t, "prune", "--prune-num=5", "--prune-time", "1day", "--no-prune-guard",
		"--match-endpoint=host:pool/g", "--visual", "--no-ranges", "pool/src")
	if p.Env.Get("PRUNE_NUM") != "5" || p.Env.Get("PRUNE_TIME") != "1day" {
		t.Errorf("PRUNE_NUM=%q PRUNE_TIME=%q", p.Env.Get("PRUNE_NUM"), p.Env.Get("PRUNE_TIME"))
	}
	if p.Env.Get("PRUNE_GUARD") != "none" || p.Env.Get("MATCH_ENDPOINT") != "host:pool/g" {
		t.Errorf("PRUNE_GUARD=%q MATCH_ENDPOINT=%q", p.Env.Get("PRUNE_GUARD"), p.Env.Get("MATCH_ENDPOINT"))
	}
	if p.Env.Get("PRUNE_VISUAL") != "1" || p.Env.Get("NO_RANGES") != "1" {
		t.Errorf("VISUAL=%q NO_RANGES=%q", p.Env.Get("PRUNE_VISUAL"), p.Env.Get("NO_RANGES"))
	}
	if len(p.Operands) != 1 {
		t.Errorf("Operands = %v", p.Operands)
	}
}
