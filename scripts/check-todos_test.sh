#!/usr/bin/env sh

set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
checker=$script_dir/check-todos.sh
fixture=$(mktemp -d "${TMPDIR:-/tmp}/check-todos.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM

marker_one=$(printf 'TO%s' 'DO')
marker_two=$(printf 'FIX%s' 'ME')

git -C "$fixture" init -q
git -C "$fixture" config user.email checker@example.invalid
git -C "$fixture" config user.name checker
mkdir -p "$fixture/internal/constants"

write_and_stage() {
	path=$1
	content=$2
	printf '%s\n' "$content" >"$fixture/$path"
	git -C "$fixture" add -- "$path"
}

write_and_stage clean.txt "finished implementation"
"$checker" --repo "$fixture"

write_and_stage bad.go "// $marker_one: unfinished"
if "$checker" --repo "$fixture" >"$fixture/output" 2>&1; then
	printf 'checker accepted an ordinary marker\n' >&2
	exit 1
fi
if ! grep -q 'bad.go:1' "$fixture/output"; then
	printf 'checker did not report the violating file and line\n' >&2
	exit 1
fi

write_and_stage bad.go "// $marker_two: unfinished"
if "$checker" --repo "$fixture" >/dev/null 2>&1; then
	printf 'checker accepted the second marker class\n' >&2
	exit 1
fi

write_and_stage bad.go "finished implementation"
write_and_stage internal/constants/output_styles.go "instruction mentions $marker_one(human) exactly"
"$checker" --repo "$fixture"

write_and_stage internal/constants/output_styles.go "instruction mentions $marker_one(human) and $marker_one later"
if "$checker" --repo "$fixture" >/dev/null 2>&1; then
	printf 'checker accepted an extra marker on an allowlisted line\n' >&2
	exit 1
fi

printf 'todo checker self-test passed\n'
