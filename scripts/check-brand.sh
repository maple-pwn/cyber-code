#!/usr/bin/env sh

set -eu

strong_pattern='claude-code-go|claude-go|claude code go|CLAUDE_GO_'
strong_matches=$(git grep -nEI "$strong_pattern" -- . \
	':!docs/plans/**' \
	':!scripts/check-brand.sh' \
	':!internal/cli/root_test.go' \
	':!internal/config/loader_test.go' || true)
if [ -n "$strong_matches" ]; then
	printf 'legacy product identifiers found:\n%s\n' "$strong_matches" >&2
	exit 1
fi

name_matches=$(git grep -nEI 'claude code' -- README.MD docs internal tests \
	':!docs/plans/**' \
	':!internal/constants/betas.go' \
	':!internal/constants/github_app.go' \
	':!internal/constants/oauth.go' \
	':!internal/constants/product.go' \
	':!internal/constants/system.go' \
	':!internal/ui/app_test.go' || true)
if [ -n "$name_matches" ]; then
	printf 'unapproved Claude Code product references found:\n%s\n' "$name_matches" >&2
	exit 1
fi
