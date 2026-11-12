#!/bin/sh
#
# Run the Zelta ShellSpec compatibility suite against the Go binary.
#
#   test/run_shellspec.sh [standard|remote|setup|cleanup|SHELLSPEC-ARGS...]
#
# The suite creates file-backed pools named by SANDBOX_ZELTA_SRC_POOL and
# SANDBOX_ZELTA_TGT_POOL (default apool and bpool, backed by /tmp/<pool>.img)
# and refuses to run if those pools already exist. ShellSpec is used from
# PATH or downloaded into test/.shellspec-local.

set -eu

cd "$(dirname "$0")/.."
ROOT=$(pwd)
SHELLSPEC_VERSION=0.28.1

: "${SANDBOX_ZELTA_SRC_POOL:=apool}"
: "${SANDBOX_ZELTA_TGT_POOL:=bpool}"
: "${SANDBOX_ZELTA_SRC_DS:=$SANDBOX_ZELTA_SRC_POOL/treetop}"
: "${SANDBOX_ZELTA_TGT_DS:=$SANDBOX_ZELTA_TGT_POOL/backups}"
: "${SANDBOX_ZELTA_TMP_SUFFIX:=${LOGNAME:-$(id -un)}}"
export SANDBOX_ZELTA_SRC_POOL SANDBOX_ZELTA_TGT_POOL SANDBOX_ZELTA_SRC_DS SANDBOX_ZELTA_TGT_DS SANDBOX_ZELTA_TMP_SUFFIX
# Keep the user's zelta.env and environment out of the suite.
unset ZELTA_ENV ZELTA_CONFIG ZELTA_LOG_LEVEL ZELTA_LOG_MODE ZELTA_LOG_FILE 2>/dev/null || true

for pool in "$SANDBOX_ZELTA_SRC_POOL" "$SANDBOX_ZELTA_TGT_POOL"; do
	if [ -z "${SANDBOX_ZELTA_SRC_REMOTE:-}${SANDBOX_ZELTA_TGT_REMOTE:-}" ] && zpool list -H "$pool" >/dev/null 2>&1; then
		echo "refusing to run: pool '$pool' already exists" >&2
		exit 1
	fi
done

# Build the binary under test (specs also run "$REPO_ROOT/bin/zelta"), or use
# a prebuilt one from ZELTA_BINARY to test another build or implementation.
if [ -n "${ZELTA_BINARY:-}" ]; then
	mkdir -p bin
	cp "$ZELTA_BINARY" bin/zelta
	ln -sf zelta bin/zprune
	export ZELTA_BINARY
else
	make -s build
fi

# ShellSpec
if command -v shellspec >/dev/null 2>&1; then
	SHELLSPEC=shellspec
else
	local_dir="$ROOT/test/.shellspec-local"
	if [ ! -x "$local_dir/shellspec" ]; then
		echo "fetching shellspec $SHELLSPEC_VERSION"
		rm -rf "$local_dir"
		git clone -q --depth 1 --branch "$SHELLSPEC_VERSION" https://github.com/shellspec/shellspec.git "$local_dir"
	fi
	SHELLSPEC="$local_dir/shellspec"
fi

# Helper commands (sudo shim, fake ssh) live in a temporary directory.
shim=$(mktemp -d "${TMPDIR:-/tmp}/zelta-shellspec.XXXXXX")
trap 'rm -rf "$shim"' EXIT
PATH="$shim:$PATH"
export PATH

# The suite elevates ZFS commands with sudo; root without sudo gets a shim.
if ! command -v sudo >/dev/null 2>&1; then
	if [ "$(id -u)" -ne 0 ]; then
		echo "sudo is required to run the suite as a regular user" >&2
		exit 1
	fi
	printf '#!/bin/sh\nexec "$@"\n' >"$shim/sudo"
	chmod 755 "$shim/sudo"
fi

# "remote" mode runs the suite with separate remote source and target hosts
# reached through a fake ssh that executes commands locally with sh, so the
# remote code paths and every quoting layer are exercised without sshd.
if [ "${1:-}" = remote ]; then
	cat >"$shim/ssh" <<'EOF'
#!/bin/sh
while [ $# -gt 0 ]; do
	case "$1" in
	-n) exec </dev/null; shift ;;
	-[46AaCfGgKkMNqsTtVvXxYy]) shift ;;
	-[BbcDEeFIiJLlmOopQRSWw]) shift 2 ;;
	-*) shift ;;
	*) break ;;
	esac
done
[ $# -gt 0 ] || { echo "fake ssh: no destination" >&2; exit 255; }
shift
exec sh -c "$*"
EOF
	# The fake hosts share this filesystem, so scp only needs a local copy.
	cat >"$shim/scp" <<'EOF'
#!/bin/sh
while [ $# -gt 2 ]; do shift; done
src=${1#*:} dst=${2#*:}
[ "$src" -ef "$dst" ] 2>/dev/null || cp -p "$src" "$dst"
EOF
	chmod 755 "$shim/ssh" "$shim/scp"
	SANDBOX_ZELTA_SRC_REMOTE=zelta-src-host
	SANDBOX_ZELTA_TGT_REMOTE=zelta-tgt-host
	export SANDBOX_ZELTA_SRC_REMOTE SANDBOX_ZELTA_TGT_REMOTE
	shift
	set -- standard "$@"
fi

REPO_ROOT="$ROOT"
export REPO_ROOT

target="${1:-standard}"
case "$target" in
standard) set -- --tag install,initialize,standard,cleanup ;;
setup) set -- --tag install,initialize ;;
cleanup) set -- --tag cleanup ;;
*) ;;
esac
exec "$SHELLSPEC" "$@"
