#!/usr/bin/env bash
# smoke — derleme + statik kapı sanity (DB GEREKTİRMEZ). CI'da ve deploy öncesi çalışır.
# gofmt + go vet + go build (tüm) + cmd/server binary derlemesi.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "[smoke] gofmt..."
files=$(git ls-files '*.go' 2>/dev/null || find . -name '*.go' -not -path './vendor/*')
d=$(gofmt -l $files || true)
if [ -n "$d" ]; then echo "  🔴 gofmt gerekli:"; echo "$d"; exit 1; fi
echo "  ✓ format temiz"

echo "[smoke] go vet ./..."
go vet ./...
echo "  ✓ vet temiz"

echo "[smoke] go build ./... (tüm paketler)"
go build ./...
echo "  ✓ derleme temiz"

echo "[smoke] cmd/server binary derlemesi"
tmp=$(mktemp -d)
go build -o "$tmp/gpanel-smoke" ./cmd/server
sz=$(stat -c%s "$tmp/gpanel-smoke" 2>/dev/null || echo 0)
rm -rf "$tmp"
echo "  ✓ server binary derlendi (${sz} bayt)"

echo "[smoke] OK — derleme + vet + format geçti (DB'siz sanity)"
