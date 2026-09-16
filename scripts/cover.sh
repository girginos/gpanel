#!/usr/bin/env bash
# cover — filtreli paket setinde test kapsamı (LİSANS-GİZLİ paketler HARİÇ) + TOPLAM %.
# CI ile birebir aynı filtre (temiz checkout'ta public repoda bulunmayan paketler):
#   cmd/server · cmd/eklenti-kur · cmd/gosp-baslatici · cmd/gosp-paketle ·
#   internal/eklenti · internal/lisans
# Kullanım: scripts/cover.sh          → kapsamı raporlar (bilgi amaçlı, 0 döner)
#           COVER_MIN=40 scripts/cover.sh → toplam < %40 ise 1 döner (eşik kapısı)
set -uo pipefail
cd "$(dirname "$0")/.."
GO="${GO:-go}"
FILTER='/(cmd/server|cmd/eklenti-kur|cmd/gosp-baslatici|cmd/gosp-paketle|internal/eklenti|internal/lisans)$'

command -v "$GO" >/dev/null 2>&1 || { echo "🔴 '$GO' bulunamadı"; exit 2; }

PKGS=$("$GO" list ./... 2>/dev/null | grep -vE "$FILTER")
n=$(printf '%s\n' "$PKGS" | grep -c . || true)
if [ "$n" = 0 ]; then echo "🔴 test edilecek paket yok (go list boş?)"; exit 2; fi
echo "[cover] $n paket test ediliyor (lisans-gizli hariç)…"

prof=$(mktemp "${TMPDIR:-/tmp}/gpanel-cover.XXXXXX")
trap 'rm -f "$prof"' EXIT

set -o pipefail
out=$("$GO" test -cover -coverprofile="$prof" $PKGS 2>&1); rc=$?
printf '%s\n' "$out"

notest=$(printf '%s\n' "$out"  | grep -c 'no test files' || true)
withtest=$(printf '%s\n' "$out" | grep -cE 'coverage: [0-9]' || true)

total="N/A"
if [ -s "$prof" ]; then
  t=$("$GO" tool cover -func="$prof" 2>/dev/null | awk '/^total:/{print $NF}')
  [ -n "$t" ] && total="$t"
fi

echo ""
echo "[cover] ═════════════════════════════════════════"
echo "[cover]  TOPLAM KAPSAM : $total"
echo "[cover]  test-li paket : $withtest"
echo "[cover]  testsiz paket : $notest"
echo "[cover] ═════════════════════════════════════════"
echo "[cover]  not: toplam %, test İÇEREN paketlerin ifadeleri üzerinden hesaplanır."

if [ "$rc" != 0 ]; then
  echo "[cover] 🔴 bazı testler BAŞARISIZ (go test çıkış kodu: $rc)"; exit "$rc"
fi

if [ -n "${COVER_MIN:-}" ] && [ "$total" != "N/A" ]; then
  num=${total%\%}
  if awk -v a="$num" -v b="$COVER_MIN" 'BEGIN{exit !(a+0 < b+0)}'; then
    echo "[cover] 🔴 kapsam $total < eşik %${COVER_MIN}"; exit 1
  fi
  echo "[cover] ✓ eşik sağlandı (≥ %${COVER_MIN})"
fi
echo "[cover] ✓ tamam"
