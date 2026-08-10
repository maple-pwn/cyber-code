#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repository_root"
export GOCACHE=${GOCACHE:-$(go env GOCACHE)}

pass() { printf 'PASS check=%s evidence=%s\n' "$1" "$2"; }
skip() { printf 'SKIP check=%s reason=%s\n' "$1" "$2"; }

go test ./internal/protocol ./internal/ui/... ./internal/runtimeapi >/dev/null
pass extension-protocol 'go-protocol-ui-runtime-tests'

if command -v pnpm >/dev/null 2>&1; then
  pnpm --filter @cyber/web test >/dev/null
  pass web 'vitest'
  pnpm --filter @cyber/desktop test >/dev/null
  pass desktop 'vitest'
else
  skip web 'pnpm-unavailable'
  skip desktop 'pnpm-unavailable'
fi

if command -v npm >/dev/null 2>&1; then
  npm test --prefix editors/vscode >/dev/null
  pass vscode 'node-test'
else
  skip vscode 'npm-unavailable'
fi

host_os=$(go env GOOS)
host_arch=$(go env GOARCH)
case "$host_os" in
  darwin)
    go test ./internal/platform ./internal/runtimeapi -run 'TestPortableTerminalBackend|TestProcessUsesFixedWorkspace|TestTerminalManager' >/dev/null
    go build -trimpath -o "${TMPDIR:-/tmp}/cyber-code-darwin-smoke" ./cmd/cli
    pass macos-native "darwin-$host_arch-cli-terminal"
    skip linux-native-tui 'requires-linux-runner'
    skip windows-conpty-job-object 'requires-windows-runner'
    ;;
  linux)
    go test ./internal/platform ./internal/runtimeapi -run 'TestPortableTerminalBackend|TestProcessUsesFixedWorkspace|TestTerminalManager' >/dev/null
    go build -trimpath -o "${TMPDIR:-/tmp}/cyber-code-linux-smoke" ./cmd/cli
    pass linux-native-tui "linux-$host_arch-cli-terminal"
    if command -v bwrap >/dev/null 2>&1; then
      go test ./internal/platform -run 'TestRequiredSandbox|TestProcessUsesFixedWorkspace' >/dev/null
      pass linux-strong-sandbox 'bubblewrap-present'
    else
      skip linux-strong-sandbox 'bubblewrap-unavailable'
    fi
    skip windows-conpty-job-object 'requires-windows-runner'
    skip macos-native 'requires-macos-runner'
    ;;
  windows)
    go test ./internal/platform ./internal/runtimeapi -run 'TestProcessUsesFixedWorkspace|TestTerminalManager' >/dev/null
    go build -trimpath -o "${TEMP:-.}/cyber-code-windows-smoke.exe" ./cmd/cli
    pass windows-conpty-job-object "windows-$host_arch-native-tests"
    skip linux-native-tui 'requires-linux-runner'
    skip macos-native 'requires-macos-runner'
    ;;
  *)
    skip native-platform "unsupported-host-$host_os-$host_arch"
    ;;
esac

go test ./internal/provider/bedrock ./internal/provider/vertex ./internal/provider/azure ./internal/provider/openai >/dev/null
pass provider-contracts 'deterministic-local-tests'

for provider in bedrock vertex azure openai_compatible; do
	case "$provider" in
		bedrock) variable=CYBER_CODE_ACCEPTANCE_BEDROCK_EVIDENCE ;;
		vertex) variable=CYBER_CODE_ACCEPTANCE_VERTEX_EVIDENCE ;;
		azure) variable=CYBER_CODE_ACCEPTANCE_AZURE_EVIDENCE ;;
		openai_compatible) variable=CYBER_CODE_ACCEPTANCE_OPENAI_COMPATIBLE_EVIDENCE ;;
	esac
	evidence=${!variable:-}
  if [[ -n "$evidence" && -s "$evidence" ]]; then
    pass "cloud-$provider" "$evidence"
  else
    skip "cloud-$provider" "set-$variable-to-redacted-smoke-evidence"
  fi
done
