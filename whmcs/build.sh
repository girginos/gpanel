#!/usr/bin/env bash
# GirginOSPanel WHMCS modulu paketi: gpanel-whmcs-<surum>.zip (yalniz modules/ agaci).
# Testler, mutasyon betigi, README ve gizli/gecici dosyalar pakete GIRMEZ.
#   bash whmcs/build.sh [cikti-dizini]
set -euo pipefail
KOK="$(cd "$(dirname "$0")" && pwd)"
CIKTI="${1:-$KOK/dist}"
SURUM="$(sed -n "s/.*const SURUM = '\([0-9][0-9.]*\)'.*/\1/p" "$KOK/modules/servers/gpanel/lib/Yardimci.php")"
[ -n "$SURUM" ] || { echo "surum okunamadi" >&2; exit 1; }

# Paket oncesi: testler + sozdizimi
php "$KOK/tests/run.php" >/dev/null || { echo "testler basarisiz — paket uretilmedi" >&2; exit 1; }

mkdir -p "$CIKTI"
ZIP="$CIKTI/gpanel-whmcs-$SURUM.zip"
rm -f "$ZIP"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -r "$KOK/modules" "$STAGE/modules"
# Yalniz beklenen uzantilar; gizli/gecici dosya sizmasin.
find "$STAGE" -type f \( -name '*.php' -o -name '*.tpl' \) -prune -o -type f -print | while read -r f; do
  echo "pakete girmemeli: $f" >&2; rm -f "$f"
done
if grep -rIlE 'gosp_[A-Za-z0-9]{12}_|BEGIN [A-Z ]*PRIVATE KEY' "$STAGE" >/dev/null 2>&1; then
  echo "gizli veri deseni bulundu — paket uretilmedi" >&2; exit 1
fi
( cd "$STAGE" && if command -v zip >/dev/null 2>&1; then
    zip -qr -X "$ZIP" modules
  else
    python3 - "$ZIP" <<'PY'
import os, sys, zipfile
with zipfile.ZipFile(sys.argv[1], 'w', zipfile.ZIP_DEFLATED) as z:
    for d, _, fs in os.walk('modules'):
        for f in sorted(fs):
            z.write(os.path.join(d, f))
PY
  fi )
echo "$ZIP"
( command -v unzip >/dev/null 2>&1 && unzip -l "$ZIP" ) || python3 -c "import zipfile,sys;print('\n'.join(zipfile.ZipFile(sys.argv[1]).namelist()))" "$ZIP"
