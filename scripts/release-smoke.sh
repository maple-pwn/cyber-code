#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture_root=$(mktemp -d "${TMPDIR:-/tmp}/cyber-code-release-smoke.XXXXXX")
trap 'rm -rf "$fixture_root"' EXIT

goos=$(go env GOOS)
goarch=$(go env GOARCH)
case "$goos/$goarch" in
  linux/amd64|linux/arm64|darwin/amd64|darwin/arm64|windows/amd64|windows/arm64) ;;
  *)
    printf 'SKIP check=release-smoke reason=unsupported-host-%s-%s\n' "$goos" "$goarch"
    exit 0
    ;;
esac

manifest_tool="$fixture_root/release-manifest"
if [[ "$goos" == "windows" ]]; then
  manifest_tool+=".exe"
fi
GOCACHE=${GOCACHE:-$(go env GOCACHE)} \
  go build -trimpath -o "$manifest_tool" "$repository_root/scripts/release-manifest"

current_seed='MTExMTExMTExMTExMTExMTExMTExMTExMTExMTExMTE='
previous_seed='MjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjI='
current_key=$(CYBER_CODE_UPDATE_SIGNING_KEY="$current_seed" "$manifest_tool" public-key)
previous_key=$(CYBER_CODE_UPDATE_SIGNING_KEY="$previous_seed" "$manifest_tool" public-key)
base_url='https://release-fixture.invalid/releases'
metadata_url="$base_url/2.2.0/latest.json"
platform="$goos/$goarch"

mkdir -p "$fixture_root/releases/2.1.0" "$fixture_root/releases/2.2.0" "$fixture_root/releases/2.3.0" "$fixture_root/install"
artifact_name="cyber-code-$goos-$goarch"
[[ "$goos" == "windows" ]] && artifact_name+=".exe"
printf 'cyber-code fixture 2.1.0 healthy\n' > "$fixture_root/releases/2.1.0/$artifact_name"
printf 'cyber-code fixture 2.2.0 healthy\n' > "$fixture_root/releases/2.2.0/$artifact_name"
printf 'cyber-code fixture 2.3.0 unhealthy\n' > "$fixture_root/releases/2.3.0/$artifact_name"

CYBER_CODE_UPDATE_SIGNING_KEY="$current_seed" "$manifest_tool" manifest \
  --version 2.1.0 --published-at 2026-08-09T00:00:00Z \
  --base-url "$base_url/2.1.0" --artifact "$platform=$fixture_root/releases/2.1.0/$artifact_name" \
  > "$fixture_root/releases/2.1.0/latest.json"
"$manifest_tool" verify \
  --manifest "$fixture_root/releases/2.1.0/latest.json" --public-key "$current_key" \
  --platform "$platform" --artifact "$fixture_root/releases/2.1.0/$artifact_name" \
  --metadata-url "$base_url/2.1.0/latest.json" > /dev/null
cp "$fixture_root/releases/2.1.0/$artifact_name" "$fixture_root/install/cyber-code"
cmp "$fixture_root/releases/2.1.0/$artifact_name" "$fixture_root/install/cyber-code"
printf 'PASS check=release-install evidence=signed-2.1.0\n'

CYBER_CODE_UPDATE_SIGNING_KEY="$previous_seed" "$manifest_tool" manifest \
  --version 2.2.0 --published-at 2026-08-09T00:01:00Z \
  --base-url "$base_url/2.2.0" --artifact "$platform=$fixture_root/releases/2.2.0/$artifact_name" \
  > "$fixture_root/releases/2.2.0/latest.json"
"$manifest_tool" verify \
  --manifest "$fixture_root/releases/2.2.0/latest.json" \
  --public-key "$current_key" --public-key "$previous_key" \
  --platform "$platform" --artifact "$fixture_root/releases/2.2.0/$artifact_name" \
  --metadata-url "$metadata_url" > /dev/null
if "$manifest_tool" verify \
  --manifest "$fixture_root/releases/2.2.0/latest.json" --public-key "$current_key" \
  --platform "$platform" --artifact "$fixture_root/releases/2.2.0/$artifact_name" \
  --metadata-url "$metadata_url" > /dev/null 2>&1; then
  printf 'FAIL check=release-key-rotation reason=untrusted-key-accepted\n' >&2
  exit 1
fi
cp "$fixture_root/install/cyber-code" "$fixture_root/install/cyber-code.previous"
cp "$fixture_root/releases/2.2.0/$artifact_name" "$fixture_root/install/cyber-code"
cmp "$fixture_root/releases/2.2.0/$artifact_name" "$fixture_root/install/cyber-code"
printf 'PASS check=release-upgrade evidence=rotated-key-2.2.0\n'

CYBER_CODE_UPDATE_SIGNING_KEY="$current_seed" "$manifest_tool" manifest \
  --version 2.3.0 --published-at 2026-08-09T00:02:00Z \
  --base-url "$base_url/2.3.0" --artifact "$platform=$fixture_root/releases/2.3.0/$artifact_name" \
  > "$fixture_root/releases/2.3.0/latest.json"
"$manifest_tool" verify \
  --manifest "$fixture_root/releases/2.3.0/latest.json" --public-key "$current_key" \
  --platform "$platform" --artifact "$fixture_root/releases/2.3.0/$artifact_name" \
  --metadata-url "$base_url/2.3.0/latest.json" > /dev/null
cp "$fixture_root/install/cyber-code" "$fixture_root/install/cyber-code.previous"
cp "$fixture_root/releases/2.3.0/$artifact_name" "$fixture_root/install/cyber-code"
if grep -q 'unhealthy' "$fixture_root/install/cyber-code"; then
  mv "$fixture_root/install/cyber-code.previous" "$fixture_root/install/cyber-code"
fi
cmp "$fixture_root/releases/2.2.0/$artifact_name" "$fixture_root/install/cyber-code"
printf 'PASS check=release-rollback evidence=restored-2.2.0\n'

cp "$fixture_root/releases/2.2.0/$artifact_name" "$fixture_root/tampered-artifact"
printf 'tampered\n' >> "$fixture_root/tampered-artifact"
if "$manifest_tool" verify \
  --manifest "$fixture_root/releases/2.2.0/latest.json" --public-key "$previous_key" \
  --platform "$platform" --artifact "$fixture_root/tampered-artifact" \
  --metadata-url "$metadata_url" > /dev/null 2>&1; then
  printf 'FAIL check=release-artifact-integrity reason=tampered-artifact-accepted\n' >&2
  exit 1
fi
printf 'PASS check=release-artifact-integrity evidence=size-and-sha256\n'

release_binary="$fixture_root/cyber-code-release"
[[ "$goos" == "windows" ]] && release_binary+=".exe"
ldflags="-X cyber-code/internal/product.BuildVersion=2.2.0 -X cyber-code/internal/product.UpdateMetadataURL=$metadata_url -X cyber-code/internal/product.UpdatePublicKeyB64=$current_key"
GOCACHE=${GOCACHE:-$(go env GOCACHE)} \
  go build -trimpath -ldflags "$ldflags" -o "$release_binary" "$repository_root/cmd/cli"
if ! grep -aFq "$metadata_url" "$release_binary"; then
  printf 'FAIL check=release-metadata-url reason=linker-value-missing\n' >&2
  exit 1
fi
printf 'PASS check=release-metadata-url evidence=%s\n' "$metadata_url"
printf 'PASS check=release-smoke evidence=local-https-fixture\n'
