#!/usr/bin/env bash
# ════════════════════════════════════════════════════════════════════
# gPanel E2E sarmalayici — DERLEME HOSTUNDA (181) calisir, hedefi surer.
#
# NEDEN VAR: paketle.sh'in 6 kapisi "paket duzgun mu" diye bakar
# (derleniyor mu, dosyalar tam mi). Hicbiri "KURULAN URUN CALISIYOR MU"
# sorusunu sormaz. 2026-09-10'daki harici test raporu bu boslugu gosterdi:
# 21 gercek eksik, hicbiri kod incelemesiyle degil, urunu KULLANARAK bulundu.
#
# Kullanim:
#   E2E_HOST=1.2.3.4 ./scripts/e2e.sh              # kurulu panele kos
#   E2E_HOST=1.2.3.4 E2E_KUR=1 ./scripts/e2e.sh    # once paketi kur, sonra kos
#   ./scripts/e2e.sh 1.2.3.4                       # host argumanla da olur
#
# Cikis: 0 = tum e2e testleri gecti.
# ════════════════════════════════════════════════════════════════════
set -uo pipefail

HOST="${1:-${E2E_HOST:-}}"
KUR="${E2E_KUR:-0}"
KANAL="${E2E_KANAL:-http://148.251.169.181:8899/install.sh}"
BURASI="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -z "$HOST" ]; then
  echo "KULLANIM: E2E_HOST=<ip> $0   (ya da: $0 <ip>)"
  echo "  E2E_KUR=1 verilirse once paket kurulur."
  exit 2
fi

uzak(){ ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15 "root@$HOST" "$@"; }

echo "════════ E2E hedef: $HOST ════════"
if ! uzak "echo baglanti-ok" >/dev/null 2>&1; then
  echo "🔴 $HOST'a anahtarla baglanilamiyor."
  echo "   Once anahtar kur:  ssh-copy-id root@$HOST   (ya da sshpass ile bir kez)"
  exit 2
fi

if [ "$KUR" = "1" ]; then
  echo "── paket kuruluyor ($KANAL) — birkac dakika surer"
  if ! uzak "curl -fsSL '$KANAL' -o /root/.e2e-install.sh && bash /root/.e2e-install.sh > /root/.e2e-kurulum.log 2>&1"; then
    echo "🔴 KURULUM DUSTU. Son satirlar:"
    uzak "tail -30 /root/.e2e-kurulum.log"
    exit 1
  fi
  echo "  ✓ kurulum bitti"
fi

echo "── test paketi gonderiliyor"
cat "$BURASI/e2e-jeton.py"  | uzak "cat > /root/.e2e-jeton.py"
cat "$BURASI/e2e-target.sh" | uzak "cat > /root/.e2e-target.sh"
# Windows'ta duzenlenmis olabilir → CR temizle (bkz. deploy tuzaklari)
uzak "sed -i 's/\r$//' /root/.e2e-jeton.py /root/.e2e-target.sh && chmod +x /root/.e2e-target.sh"
uzak "python3 -c \"import ast; ast.parse(open('/root/.e2e-jeton.py',encoding='utf-8').read())\"" \
  || { echo "🔴 jeton yardimcisi bozuk"; exit 1; }

echo "── e2e kosuyor"
echo
uzak "bash /root/.e2e-target.sh"
SONUC=$?

echo
if [ $SONUC -eq 0 ]; then
  echo "✅ E2E GECTI — kurulan urun calisiyor."
else
  echo "🔴 E2E DUSTU — paket URETILDI ama YAYINLANMAMALI."
fi
exit $SONUC
