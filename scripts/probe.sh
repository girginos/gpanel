#!/usr/bin/env bash
# probe — CANLI panel sağlık yoklaması + API-smoke (deploy sonrası doğrulama). Salt-okunur.
# Kullanım: scripts/probe.sh [port]   (port verilmezse dinlenen porttan bulunur; vars. 8080)
# Bölümler:
#   (1) systemd servis durumları  (2) /healthz + yanıt süresi
#   (3) API-smoke: public 200 uç + auth-gerekli uçta negatif kontrol (401/403 beklenir)
set -uo pipefail
PORT="${1:-}"
FAIL=0

echo "[probe] systemd servis durumları:"
CEK_SERVISLER="girginospanel girginospanel-eklenti-calistirici girginospanel-eklenti-mail girginospanel-eklenti-tehdit"
for s in $CEK_SERVISLER; do
  st=$(systemctl is-active "$s" 2>/dev/null || echo "yok")
  printf "  %-42s %s\n" "$s" "$st"
  if [ "$s" = "girginospanel" ] && [ "$st" != "active" ]; then FAIL=1; fi
done

# Panel portu: argüman yoksa girginospanel'in dinlediği TCP porttan bul (vars. 8080)
if [ -z "$PORT" ]; then
  PORT=$(ss -ltnp 2>/dev/null | grep -i "girginospanel" | grep -oE ':[0-9]+' | tr -d ':' | sort -u | head -1)
fi
PORT="${PORT:-8080}"

# Şema tespiti: panel düz-HTTP (127.0.0.1:8080) veya HTTPS olabilir → ikisini de dene.
BASE=""
for sch in http https; do
  c=$(curl -sk -m 5 -o /dev/null -w '%{http_code}' "$sch://127.0.0.1:$PORT/healthz" 2>/dev/null || echo 000)
  if [ "$c" = "200" ]; then BASE="$sch://127.0.0.1:$PORT"; break; fi
done
[ -z "$BASE" ] && BASE="http://127.0.0.1:$PORT"   # 200 yoksa yine de raporla

# Gözlemlenebilirlik: portu dinleyen süreç var mı (pid)?
pid=$(ss -ltnp 2>/dev/null | grep -E "127\.0\.0\.1:$PORT " | grep -oE 'pid=[0-9]+' | head -1 | cut -d= -f2)
echo "[probe] panel: $BASE  (dinleyen pid: ${pid:-yok})"
if [ -z "$pid" ]; then echo "  🔴 $PORT portunu dinleyen süreç yok"; FAIL=1; fi

# --- (2) /healthz — mevcut kontrol (KORUNDU) + yanıt süresi ---
echo "[probe] GET $BASE/healthz"
read -r code ttime < <(curl -sk -m 5 -o /dev/null -w '%{http_code} %{time_total}' "$BASE/healthz" 2>/dev/null || echo "000 -")
printf "  HTTP %s  (yanıt: %s s)\n" "$code" "$ttime"
if [ "$code" != "200" ]; then FAIL=1; fi

# --- (3) API-smoke ---
echo "[probe] API-smoke:"

smoke_pos() { # smoke_pos <yol> <beklenen>  (public uç)
  local path="$1" want="$2" c
  c=$(curl -sk -m 5 -o /dev/null -w '%{http_code}' "$BASE$path" 2>/dev/null || echo 000)
  if [ "$c" = "$want" ]; then printf "  ✓ %-28s %s (public)\n" "$path" "$c"
  else printf "  🔴 %-28s %s (beklenen %s)\n" "$path" "$c" "$want"; FAIL=1; fi
}

smoke_neg() { # smoke_neg <yol>  (auth-gerekli → yalnız reddi doğrula)
  local path="$1" c
  c=$(curl -sk -m 5 -o /dev/null -w '%{http_code}' "$BASE$path" 2>/dev/null || echo 000)
  case "$c" in
    401|403) printf "  ✓ %-28s %s (auth gerekli — doğru reddetti)\n" "$path" "$c" ;;
    200)     printf "  🔴 %-28s 200 (YETKİSİZ ERİŞİM — auth atlatıldı!)\n" "$path"; FAIL=1 ;;
    *)       printf "  🔴 %-28s %s (beklenen 401/403)\n" "$path" "$c"; FAIL=1 ;;
  esac
}

smoke_pos "/healthz"       200   # public sağlık
smoke_pos "/api/v1/marka"  200   # public marka/branding ucu
smoke_neg "/api/v1/me"           # auth-gerekli: kimliksiz istek reddedilmeli

echo ""
if [ "$FAIL" = 0 ]; then echo "[probe] ✓ SAĞLIKLI"; else echo "[probe] 🔴 SORUN VAR"; exit 1; fi
