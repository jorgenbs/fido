#!/usr/bin/env bash
# scripts/smoke-test.sh
# Verifies Fido builds and basic commands work without real Datadog credentials.
set -euo pipefail

echo "=== Building fido ==="
go build -o ./bin/fido .

echo "=== Testing: fido --help ==="
./bin/fido --help

echo "=== Testing: fido list (with temp config) ==="
TMPDIR=$(mktemp -d)
mkdir -p "$TMPDIR/reports"
cat > "$TMPDIR/config.yml" <<EOF
datadog:
  api_key: "fake"
  app_key: "fake"
  site: "datadoghq.eu"
scan:
  interval: "15m"
  since: "24h"
EOF

./bin/fido --config "$TMPDIR/config.yml" list

echo "=== Testing: fido show (nonexistent issue) ==="
./bin/fido --config "$TMPDIR/config.yml" show nonexistent 2>&1 || true

echo "=== Running all unit tests ==="
go test ./... -v

echo "=== Building web UI ==="
cd web && npm run build

echo "=== All smoke tests passed ==="
cd ..
rm -rf "$TMPDIR" ./bin/fido
