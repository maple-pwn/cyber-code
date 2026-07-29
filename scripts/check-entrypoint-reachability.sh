#!/usr/bin/env sh

set -eu

entrypoint=./cmd/cli
forbidden_paths='internal/commands internal/tools internal/voice internal/ui/components/chat.go internal/services/plugin_loader.go internal/services/plugins.go internal/services/mcp'
allowed_main_packages='cyber-code/cmd/cli
cyber-code/scripts/release-manifest'

if [ ! -f cmd/cli/main.go ]; then
	printf 'canonical entry point is missing: cmd/cli/main.go\n' >&2
	exit 1
fi

dependencies=$(go list -deps -f '{{.ImportPath}}' "$entrypoint")
for required in cyber-code/internal/cli cyber-code/internal/runtime cyber-code/internal/ui cyber-code/internal/protocol; do
	if ! printf '%s\n' "$dependencies" | grep -Fqx "$required"; then
		printf 'canonical entry point does not reach required package: %s\n' "$required" >&2
		exit 1
	fi
done

status=0
for target in $forbidden_paths; do
	remaining=''
	if [ -f "$target" ]; then
		remaining=$target
	elif [ -d "$target" ]; then
		remaining=$(find "$target" -type f -print)
	fi
	if [ -n "$remaining" ]; then
		printf 'confusing legacy product path remains:\n%s\n' "$remaining" >&2
		status=1
	fi
done

if [ "$status" -ne 0 ]; then
	exit "$status"
fi

main_packages=$(go list -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./... | sed '/^$/d' | sort)
if [ "$main_packages" != "$allowed_main_packages" ]; then
	printf 'unexpected main packages found; cmd/cli is the product entry and scripts/release-manifest is the only development tool:\n%s\n' "$main_packages" >&2
	exit 1
fi

printf 'canonical entry-point reachability check passed\n'
