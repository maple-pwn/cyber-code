#!/usr/bin/env sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo=$(CDPATH= cd -- "$script_dir/.." && pwd)

usage() {
	printf 'usage: %s [--self-test] [--repo PATH]\n' "$0"
}

case "${1:-}" in
	--self-test)
		shift
		if [ "$#" -ne 0 ]; then
			usage >&2
			exit 2
		fi
		exec sh "$script_dir/check-todos_test.sh"
		;;
	--repo)
		shift
		if [ "$#" -ne 1 ]; then
			usage >&2
			exit 2
		fi
		repo=$1
		;;
	-h|--help)
		usage
		exit
		;;
	"")
		;;
	*)
		usage >&2
		exit 2
		;;
esac

if ! git -C "$repo" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	printf 'marker check requires a Git work tree: %s\n' "$repo" >&2
	exit 2
fi

marker_one=$(printf 'TO%s' 'DO')
marker_two=$(printf 'FIX%s' 'ME')
matches=$(mktemp "${TMPDIR:-/tmp}/check-todos.matches.XXXXXX")
violations=$(mktemp "${TMPDIR:-/tmp}/check-todos.violations.XXXXXX")
trap 'rm -f "$matches" "$violations"' EXIT HUP INT TERM

status=0
git -C "$repo" grep -n -I -E "$marker_one|$marker_two" -- . >"$matches" || status=$?
case "$status" in
	0|1) ;;
	*)
		printf 'marker scan failed with status %s\n' "$status" >&2
		exit "$status"
		;;
esac

awk -v first="$marker_one" -v second="$marker_two" '
{
	original = $0
	first_colon = index($0, ":")
	remainder = substr($0, first_colon + 1)
	second_colon = index(remainder, ":")
	path = substr($0, 1, first_colon - 1)
	content = substr(remainder, second_colon + 1)
	if (path == "internal/constants/output_styles.go") {
		allowed = first "(human)"
		while ((position = index(content, allowed)) != 0) {
			content = substr(content, 1, position - 1) substr(content, position + length(allowed))
		}
	}
	if (index(content, first) != 0 || index(content, second) != 0) {
		print original
	}
}
' "$matches" >"$violations"

if [ -s "$violations" ]; then
	printf 'unclassified implementation markers found:\n' >&2
	cat "$violations" >&2
	exit 1
fi
