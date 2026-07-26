#!/bin/sh
#
# Zelta Installer
#
#   PRE_RUN is executed before installation (default: build the Go binary).
#   Override with PRE_RUN='' to skip, or PRE_RUN='make -C doc' to also build
#   man pages.

: ${PRE_RUN="make"}
if [ -n "$PRE_RUN" ]; then
	echo "Running: $PRE_RUN"
	(eval "$PRE_RUN") || exit 1
fi

# Root-vs-user path defaults
if [ "$(id -u)" -eq 0 ]; then
	: ${ZELTA_BIN:="/usr/local/bin"}
	: ${ZELTA_SHARE:="/usr/local/share/zelta"}
	: ${ZELTA_ETC:="/usr/local/etc/zelta"}
	: ${ZELTA_DOC:="/usr/local/man"}
elif [ -z "$ZELTA_BIN" ] || [ -z "$ZELTA_SHARE" ] || [ -z "$ZELTA_ETC" ] || [ -z "$ZELTA_DOC" ]; then
	: ${ZELTA_BIN:="$HOME/bin"}
	: ${ZELTA_SHARE:="$HOME/.local/share/zelta"}
	: ${ZELTA_ETC:="$HOME/.config/zelta"}
	: ${ZELTA_DOC:="$ZELTA_SHARE/doc"}
fi

: ${ZELTA_CONFIG:="$ZELTA_ETC/zelta.conf"}
: ${ZELTA_ENV:="$ZELTA_ETC/zelta.env"}
ZELTA="$ZELTA_BIN/zelta"
ZPRUNE="$ZELTA_BIN/zprune"

# Helpers
copy_file() {
	if [ -z "$3" ]; then
		ZELTA_MODE="644"
	else
		ZELTA_MODE="$3"
	fi
	if [ ! -f "$2" ] || [ "$1" -nt "$2" ]; then
		echo "installing: $1 -> $2"
		cp "$1" "$2"
		chmod "$ZELTA_MODE" "$2"
		ZELTA_UPDATED=1
	fi
}

# Create directories
mkdir -p "$ZELTA_BIN" || exit 1
mkdir -p "$ZELTA_SHARE" || exit 1
mkdir -p "$ZELTA_ETC" || exit 1

# Install binaries
copy_file bin/zelta "$ZELTA" 755
copy_file bin/zprune "$ZPRUNE" 755

# Install shared data files
for f in share/zelta/zelta-*; do
	[ -f "$f" ] || continue
	copy_file "$f" "$ZELTA_SHARE/${f#share/zelta/}"
done

# Install man pages
if [ -n "$ZELTA_DOC" ]; then
	mkdir -p "${ZELTA_DOC}/man7" 2>/dev/null
	mkdir -p "${ZELTA_DOC}/man8" 2>/dev/null

	if [ -d doc/man7 ]; then
		for f in doc/man7/*.7; do
			[ -f "$f" ] || continue
			copy_file "$f" "${ZELTA_DOC}/man7/${f#doc/man7/}"
		done
	fi

	if [ -d doc/man8 ]; then
		for f in doc/man8/*.8; do
			[ -f "$f" ] || continue
			copy_file "$f" "${ZELTA_DOC}/man8/${f#doc/man8/}"
		done
	fi
fi

# Install environment file (always .example; only as default if missing or empty)
copy_file zelta.env "$ZELTA_ENV.example"
[ ! -s "$ZELTA_ENV" ] && copy_file zelta.env "$ZELTA_ENV"

# Install configuration file (always .example; only as default if missing or empty)
copy_file zelta.conf "$ZELTA_CONFIG.example"
[ ! -s "$ZELTA_CONFIG" ] && copy_file zelta.conf "$ZELTA_CONFIG"

# Up-to-date check
[ -z "$ZELTA_UPDATED" ] && echo "up-to-date"

# PATH conflict warning
if [ -n "$ZELTA_BIN" ]; then
	_other_zelta="$(command -v zelta 2>/dev/null)"
	if [ -n "$_other_zelta" ] && [ "$_other_zelta" != "$ZELTA" ]; then
		echo "WARNING: found another zelta in PATH: $_other_zelta" >&2
		echo "         Ensure $ZELTA_BIN is first in your PATH, or remove the other." >&2
	fi
fi

# Non-root post-install info
if [ "$(id -u)" -ne 0 ]; then
	if ! echo "$PATH" | grep -q "$ZELTA_BIN"; then
		echo ""
		echo "Add $ZELTA_BIN to your PATH:"
		echo "  export PATH=\"\$PATH:$ZELTA_BIN\""
		echo ""
	fi
fi

# Verify installation
"$ZELTA" version
