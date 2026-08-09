#!/usr/bin/env bash
set -euo pipefail

echo "[cyber-agent] Go client and adapters"
go test ./internal/cyberagent ./internal/cli ./internal/ui/adapter -race -count=1

echo "[cyber-agent] web/product/desktop input paths"
./node_modules/.bin/vitest run \
  packages/runtime-client/src/cyber-agent-event-source.test.ts \
  packages/product-app/src/pages/new-task.test.tsx \
  packages/product-app/src/pages/scope-review.test.tsx \
  apps/desktop/src/native.test.ts

echo "[cyber-agent] type contracts"
./node_modules/.bin/tsc -b packages/runtime-client packages/product-app apps/web apps/desktop --pretty false

echo "[cyber-agent] native Tauri contract"
cargo test --manifest-path apps/desktop/src-tauri/Cargo.toml

echo "[cyber-agent] platform/cloud/OAuth checks remain environment-specific PASS/SKIP"
