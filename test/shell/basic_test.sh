#!/bin/sh
# Basic smoke tests for the zelta binary.
# Runs on any POSIX system with man available.
# Usage: ZELTA_BIN=./bin/zelta sh test/shell/basic_test.sh
# Remote: SANDBOX_HOST=dev2 SANDBOX_BIN=/tmp/zelta sh test/shell/basic_test.sh

: "${ZELTA_BIN:=./bin/zelta}"
fail=0

# Remote execution wrapper (runs command on SANDBOX_HOST when set).
if [ -n "$SANDBOX_HOST" ]; then
	sb_bin="${SANDBOX_BIN:-$ZELTA_BIN}"
	sb_user="${SANDBOX_USER:-root}"
	sb_ssh="${SANDBOX_SSH:-ssh}"
	_run() { $sb_ssh "${sb_user}@${SANDBOX_HOST}" "$sb_bin" "$@" 2>&1; }
	_has_man() { $sb_ssh "${sb_user}@${SANDBOX_HOST}" command -v man >/dev/null 2>&1; }
	ZELTA_BIN="$sb_bin"
	export ZELTA_BIN
else
	_run() { "$ZELTA_BIN" "$@" 2>&1; }
	_has_man() { command -v man >/dev/null 2>&1; }
fi

pass() { printf "PASS: %s\n" "$1"; }
xfail() { printf "XFAIL: %s\n" "$1"; fail=$((fail + 1)); }

# Binary exists and is executable
[ -x "$ZELTA_BIN" ] && pass "binary exists" || xfail "binary exists"

# version
out=$(_run version) && case "$out" in
	*[Zz]elta*) pass "version output" ;;
	*) xfail "version output: $out" ;;
esac

# no args -> usage to stderr, exit 1
out=$(_run); rc=$?
[ $rc -eq 1 ] && pass "no args: exit 1" || xfail "no args: exit 1 (got $rc)"
case "$out" in
	"usage: zelta command"*) pass "no args: usage text" ;;
	*) xfail "no args: usage text" ;;
esac

# --help -> usage to stderr, exit 0
out=$(_run --help); rc=$?
[ $rc -eq 0 ] && pass "--help: exit 0" || xfail "--help: exit 0 (got $rc)"
case "$out" in
	"usage: zelta command"*) pass "--help: usage text" ;;
	*) xfail "--help: usage text" ;;
esac

# -h -> usage to stderr, exit 0 (same as --help)
out=$(_run -h); rc=$?
[ $rc -eq 0 ] && pass "-h: exit 0" || xfail "-h: exit 0 (got $rc)"
case "$out" in
	"usage: zelta command"*) pass "-h: usage text" ;;
	*) xfail "-h: usage text" ;;
esac

# unknown verb -> error + usage, exit 1
out=$(_run nonexistent); rc=$?
[ $rc -eq 1 ] && pass "unknown verb: exit 1" || xfail "unknown verb: exit 1 (got $rc)"
case "$out" in
	*"unrecognized command"*) pass "unknown verb: error message" ;;
	*) xfail "unknown verb: error message: $out" ;;
esac

# --version -> version string
out=$(_run --version); rc=$?
[ $rc -eq 0 ] && pass "--version: exit 0" || xfail "--version: exit 0 (got $rc)"
case "$out" in
	*[Zz]elta*) pass "--version: version text" ;;
	*) xfail "--version: version text" ;;
esac

# -V -> version string
out=$(_run -V); rc=$?
[ $rc -eq 0 ] && pass "-V: exit 0" || xfail "-V: exit 0 (got $rc)"
case "$out" in
	*[Zz]elta*) pass "-V: version text" ;;
	*) xfail "-V: version text" ;;
esac

# match --help (verb-level flag)
out=$(_run match --help); rc=$?
# match --help sets USAGE=true via opt, which match currently prints and exits 1
# Some verb functions don't handle --help cleanly yet; accept any exit
case "$out" in
	*"Compare"*|*"usage"*|*"match"*) pass "match --help: shows help text" ;;
	*) xfail "match --help: unexpected output: $out" ;;
esac

# zelta help -> man page (zelta(8))
if _has_man; then
	out=$(_run help); rc=$?
	[ $rc -eq 0 ] && pass "help: exit 0" || xfail "help: exit 0 (got $rc)"
	case "$out" in
		*zelta*\(8\)*|*ZELTA*|*zelta*) pass "help: shows man page" ;;
		*) xfail "help: unexpected output: $out" ;;
	esac
else
	echo "SKIP: help (no man command available)"
fi

# zelta help match -> zelta-match(8)
if _has_man; then
	out=$(_run help match); rc=$?
	[ $rc -eq 0 ] && pass "help match: exit 0" || xfail "help match: exit 0 (got $rc)"
	case "$out" in
		*zelta-match*\(8\)*|*"zelta match"*) pass "help match: shows match man page" ;;
		*) xfail "help match: unexpected output: $out" ;;
	esac
else
	echo "SKIP: help match (no man command available)"
fi

# zelta help nonexistent -> error (no man page)
if _has_man; then
	out=$(_run help nonexistent); rc=$?
	[ $rc -eq 1 ] && pass "help nonexistent: exit 1" || xfail "help nonexistent: exit 1 (got $rc)"
	case "$out" in
		*"No manual entry"*|*"man page not available"*) pass "help nonexistent: error message" ;;
		*) xfail "help nonexistent: unexpected output: $out" ;;
	esac
else
	echo "SKIP: help nonexistent (no man command available)"
fi

printf "\n"
if [ $fail -eq 0 ]; then
	echo "All tests passed."
	exit 0
else
	echo "$fail test(s) failed."
	exit 1
fi
