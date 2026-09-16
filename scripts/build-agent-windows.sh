#!/usr/bin/env bash
# Windows ajaninin AYRI derlemesi.
#
# 🔴 build-assets.sh'a BILEREK eklenmedi: iki artefakt ayni scriptte uretilse
# her panel derlemesi ajan artefaktini da tazeler, "bagimsiz guncelleme"
# kagit ustunde kalirdi. Ajan yalniz bu script ile, kendi basina derlenir.
#
# GOAMD64=v1: panel derlemesiyle ayni gerekce — eski islemcilerde v3 komutlari
# SIGILL ile hic calismaz (bkz. scripts/build-assets.sh basligi).
set -euo pipefail
cd "$(dirname "$0")/.."

export CGO_ENABLED=0 GOOS=windows GOARCH=amd64 GOAMD64=v1
echo "== girginospanel-agent (windows/amd64, GOAMD64=$GOAMD64) derleniyor =="
go build -trimpath -o assets/girginospanel-agent-windows.exe ./cmd/girginospanel-agent
ls -la assets/girginospanel-agent-windows.exe
echo "✓ Bitti. Bu artefakt panel yayinindan BAGIMSIZDIR; kanal: windows/dev"
