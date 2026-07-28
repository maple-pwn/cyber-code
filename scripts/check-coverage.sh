#!/usr/bin/env sh

set -eu

tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/cyber-code-coverage.XXXXXX")
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

unit_profile="$tmp_dir/unit.cover"
provider_profile="$tmp_dir/provider.cover"

go test -coverprofile="$unit_profile" \
	./internal/core \
	./internal/agent \
	./internal/permissions \
	./internal/security \
	./internal/session \
	./internal/tool \
	./internal/tool/builtin

# Cloud contract tests live in the parent provider package. -coverpkg makes
# those calls count toward the concrete adapter packages they exercise.
go test -coverpkg=./internal/provider/... -coverprofile="$provider_profile" ./internal/provider/...

check_package() {
	profile=$1
	package=$2
	threshold=$3
	awk -v target="$package" -v threshold="$threshold" '
		NR == 1 { next }
		{
			key = $1 " " $2
			split($1, location, ":")
			file[key] = location[1]
			statements[key] = $2
			if ($3 > 0) {
				covered[key] = 1
			}
		}
		END {
			covered_statements = 0
			total_statements = 0
			for (key in statements) {
				path = file[key]
				sub("/[^/]+$", "", path)
				if (path != target) {
					continue
				}
				total_statements += statements[key]
				if (covered[key]) {
					covered_statements += statements[key]
				}
			}
			if (total_statements == 0) {
				printf "%s has no coverage statements\n", target > "/dev/stderr"
				exit 2
			}
			percent = 100 * covered_statements / total_statements
			printf "%s %.1f%% (%d/%d), required %.1f%%\n", target, percent, covered_statements, total_statements, threshold
			if (covered_statements * 100 < total_statements * threshold) {
				exit 1
			}
		}
	' "$profile"
}

check_package "$unit_profile" cyber-code/internal/core 80
check_package "$unit_profile" cyber-code/internal/agent 80
check_package "$unit_profile" cyber-code/internal/permissions 90
check_package "$unit_profile" cyber-code/internal/security 90
check_package "$unit_profile" cyber-code/internal/session 80
check_package "$unit_profile" cyber-code/internal/tool 80
check_package "$unit_profile" cyber-code/internal/tool/builtin 80

check_package "$provider_profile" cyber-code/internal/provider 80
check_package "$provider_profile" cyber-code/internal/provider/anthropic 80
check_package "$provider_profile" cyber-code/internal/provider/openai 80
check_package "$provider_profile" cyber-code/internal/provider/azure 80
check_package "$provider_profile" cyber-code/internal/provider/bedrock 80
check_package "$provider_profile" cyber-code/internal/provider/vertex 80
check_package "$provider_profile" cyber-code/internal/provider/testkit 80
