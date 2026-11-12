#!/bin/sh
#
# Zelta uninstaller
#
# Removes files installed by install.sh, including those of the original Awk
# implementation (share/zelta-*.awk and helper links). Configuration files
# (zelta.env, zelta.conf) are kept unless --purge is given.

usage() {
	cat <<-EOF
	usage: uninstall.sh [--purge] PHASE...

	Remove Zelta installation files from the specified locations.

	PHASES:
	  system    Remove the system-wide installation (/usr/local/*)
	  home      Remove the default user installation (~/.local/*, ~/bin)
	  env       Remove a custom installation given by ZELTA_* environment variables
	  all       Remove from all locations

	OPTIONS:
	  --purge   Also remove zelta.env and zelta.conf
	EOF
	exit 0
}

Removed=""

remove() {
	for f in "$@"; do
		if [ -L "$f" ] || [ -f "$f" ]; then
			echo "- removing: $f"
			rm -f "$f" && Removed=1
		fi
	done
}

remove_dir() {
	if [ -d "$1" ] && rmdir "$1" 2>/dev/null; then
		echo "- removing directory: $1"
		Removed=1
	fi
}

# uninstall_from BIN SHARE ETC DOC
uninstall_from() {
	_bin=$1 _share=$2 _etc=$3 _doc=$4
	for name in zelta zprune zmatch zpull zpush zsync zeport zp; do
		remove "$_bin/$name"
	done
	if [ -d "$_share" ]; then
		remove "$_share"/zelta-*.awk "$_share"/zelta-*.tsv "$_share"/completions/zelta.bash \
			"$_share"/completions/_zelta "$_share"/completions/zelta.fish
		remove_dir "$_share/completions"
	fi
	for section in 7 8; do
		remove "$_doc/man$section"/zelta*."$section" "$_doc/man$section/zprune.$section"
		remove_dir "$_doc/man$section"
	done
	remove_dir "$_doc"
	remove "$_etc/zelta.env.example" "$_etc/zelta.conf.example"
	if [ -n "$Purge" ]; then
		remove "$_etc/zelta.env" "$_etc/zelta.conf"
		remove_dir "$_etc"
	fi
	remove_dir "$_share"
}

Purge=""
[ $# -eq 0 ] && usage
for phase in "$@"; do
	case "$phase" in
	--purge) Purge=1 ;;
	-h | --help) usage ;;
	esac
done
for phase in "$@"; do
	case "$phase" in
	--purge) ;;
	system | all)
		uninstall_from /usr/local/bin /usr/local/share/zelta /usr/local/etc/zelta /usr/local/man
		;;
	esac
	case "$phase" in
	home | all)
		uninstall_from "$HOME/bin" "$HOME/.local/share/zelta" "$HOME/.config/zelta" "$HOME/.local/share/zelta/doc"
		;;
	esac
	case "$phase" in
	env | all)
		[ -n "$ZELTA_BIN" ] && [ -n "$ZELTA_SHARE" ] && [ -n "$ZELTA_ETC" ] &&
			uninstall_from "$ZELTA_BIN" "$ZELTA_SHARE" "$ZELTA_ETC" "${ZELTA_DOC:-$ZELTA_SHARE/doc}"
		;;
	--purge | system | home) ;;
	*) echo "unknown phase: $phase" >&2; exit 1 ;;
	esac
done

[ -n "$Removed" ] || echo "No Zelta files found."
[ -n "$Purge" ] || echo "Configuration files were kept; use --purge to remove them."
exit 0
