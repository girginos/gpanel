#!/usr/bin/env bash
# deploy — MANUEL CD (canlı sunucuda root ile). Güvenli sıra:
#   smoke → build → binary yedekle → değiştir → restart → probe → başarısızsa ROLLBACK.
# CD'yi otomatik tetiklemez; operatör bilinçli çalıştırır.
set -euo pipefail
cd "$(dirname "$0")/.."
SVC=girginospanel.service

BIN=$(systemctl show -p ExecStart "$SVC" 2>/dev/null | grep -oP 'path=\K[^ ;]+' | head -1)
if [ -z "$BIN" ] || [ ! -f "$BIN" ]; then echo "🔴 binary yolu bulunamadı ($SVC ExecStart)"; exit 1; fi
echo "[deploy] hedef binary : $BIN"
echo "[deploy] servis       : $SVC"

echo "[deploy] 1/5 smoke (derleme + statik kapılar)"
./scripts/smoke.sh

echo "[deploy] 2/5 yeni binary derle"
NEW=$(mktemp)
go build -o "$NEW" ./cmd/server

echo "[deploy] 3/5 mevcut binary yedekle → $BIN.bak"
cp -f "$BIN" "$BIN.bak"

echo "[deploy] 4/5 değiştir + restart"
install -m0755 "$NEW" "$BIN"; rm -f "$NEW"
systemctl restart "$SVC"
sleep 2

echo "[deploy] 5/5 canlı probe"
if ./scripts/probe.sh; then
  echo "[deploy] ✓ BAŞARILI ($BIN güncellendi; yedek: $BIN.bak)"
else
  echo "[deploy] 🔴 PROBE BAŞARISIZ — ROLLBACK yapılıyor"
  cp -f "$BIN.bak" "$BIN"; systemctl restart "$SVC"
  sleep 2; ./scripts/probe.sh || true
  echo "[deploy] rollback tamam (eski binary geri yüklendi)"; exit 1
fi
