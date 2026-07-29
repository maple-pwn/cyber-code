#!/usr/bin/env sh

set -eu

# Keep this list limited to paths that have completed migration. Callers may
# provide explicit paths to check a different, similarly reviewed scope.
MIGRATED_PATHS="internal tests cmd editors"

usage() {
	printf 'usage: %s [--self-test] [PATH ...]\n' "$0"
}

scan() {
	pattern='not([[:space:]_-]+yet)?[[:space:]_-]+implemented|placeholder[[:space:]_-]+(response|success)|stub[[:space:]_-]+response|empty[[:space:]_-]+success|no-?op[[:space:]_-]+success|panic\([[:space:]]*"(TODO|not([[:space:]_-]+yet)?[[:space:]_-]+implemented)|return[[:space:]]+(nil|true|""|\{\})[[:space:]]*(//|#)[[:space:]]*(TODO|FIXME|placeholder|stub)'

	for target in "$@"; do
		if [ ! -e "$target" ]; then
			printf 'placeholder check target does not exist: %s\n' "$target" >&2
			return 2
		fi
	done

	status=0
	matches=$(grep -RInE -i --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=dist "$pattern" -- "$@") || status=$?
	case "$status" in
		0)
			printf 'placeholder markers found:\n%s\n' "$matches" >&2
			return 1
			;;
		1)
			return 0
			;;
		*)
			printf 'placeholder scan failed with status %s\n' "$status" >&2
			return "$status"
			;;
	esac
}

self_test() {
	tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/check-placeholders.XXXXXX")
	trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

	printf '%s\n' 'package clean' >"$tmp_dir/clean.go"
	scan "$tmp_dir/clean.go"
	printf '%s\n' 'package clean' >"$tmp_dir/--help"
	if ! (cd "$tmp_dir" && scan "--help") >/dev/null 2>&1; then
		printf 'self-test failed: option-like path was not scanned as a path\n' >&2
		return 1
	fi

	assert_rejected 'not ' 'implemented'
	assert_rejected 'not yet ' 'implemented'
	assert_rejected 'placeholder ' 'response'
	assert_rejected 'func unfinished() error { return nil // ' 'TODO }'
	assert_rejected 'no-op ' 'success'

	grep() { return 2; }
	scan_status=0
	assert_rejected 'forced scanner ' 'error' || scan_status=$?
	unset -f grep
	if [ "$scan_status" -ne 2 ]; then
		printf 'self-test failed: scanner error returned %s instead of 2\n' "$scan_status" >&2
		return 1
	fi

	printf 'placeholder checker self-test passed\n'
}

assert_rejected() {
	printf '%s%s\n' "$1" "$2" >"$tmp_dir/rejected.txt"
	status=0
	scan "$tmp_dir/rejected.txt" >/dev/null 2>&1 || status=$?
	case "$status" in
		1)
			return 0
			;;
		0)
			printf 'self-test failed: placeholder marker was accepted\n' >&2
			return 1
			;;
		*)
			return "$status"
			;;
	esac
}

case "${1:-}" in
	--self-test)
		shift
		if [ "$#" -ne 0 ]; then
			usage >&2
			exit 2
		fi
		self_test
		exit
		;;
	-h|--help)
		usage
		exit
		;;
esac

if [ "$#" -eq 0 ]; then
	# Word splitting is intentional for this maintained, space-free allowlist.
	# shellcheck disable=SC2086
	set -- $MIGRATED_PATHS
fi

scan "$@"
