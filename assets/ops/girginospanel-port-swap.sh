#!/usr/bin/env bash
# girginospanel-port-swap.sh — panel BACKEND portunu degistirir.
#
# 🔴 NEDEN AYRI BIR HELPER: backend portu degisince panel sureci YENIDEN
# BASLATILIR. Degisimi panelin kendi icinden yapsaydik, restart aninda surec
# olur ve dogrulama/rollback ADIMI HIC KOSMAZDI — port yanlis kalirsa panel
# bir daha acilmaz. Bu yuzden panel bizi `systemd-run` ile DETACH baslatir:
# panel olse de biz ayakta kalir, dogrular, gerekirse geri aliriz.
#
# Cagri:  girginospanel-port-swap.sh backend <eskiPort> <yeniPort> <logDosyasi>
#
# Panelin okudugu ciktilar (degistirme — degistir.go bunlari parse eder):
#   ADIM: <etiket>|<0|1>|<bilgi>
#   SONUC: OK | ROLLBACK <hata> | FAIL <hata>
set -uo pipefail

kullanim() {
  cat <<'USAGE'
Usage: girginospanel-port-swap.sh backend <old-port> <new-port> <log-file>

Internal helper, started by the panel (port management page) through
systemd-run so that it survives the panel restart it causes. Changes the
panel BACKEND port: PANEL_LISTEN in /etc/girginospanel/env and the proxy_pass
targets in /etc/nginx/conf.d/_panel.conf, validates with nginx -t, restarts
the panel, waits up to 30 s for the new port, checks the panel through nginx,
and rolls everything back (and verifies the rollback) if any step fails.
Run as root. Not meant to be run by hand.

Output protocol (parsed by the panel, do not change):
  ADIM: <label>|<0|1>|<info>          one line per step (1 = ok, 0 = failed)
  SONUC: OK | ROLLBACK <error> | FAIL <error>

Exit codes: 0 port changed, 1 rolled back or failed before anything changed
USAGE
}
case "${1:-}" in -h|--help|help) kullanim; exit 0 ;; esac

MOD="${1:-}"; ESKI="${2:-}"; YENI="${3:-}"; LOG="${4:-/dev/stdout}"

ENV_DOSYA=/etc/girginospanel/env
VHOST=/etc/nginx/conf.d/_panel.conf
TS=$(date +%s)

adim() { echo "ADIM: $1|$2|${3:-}"; }
bitir_ok()       { echo "SONUC: OK"; exit 0; }
bitir_rollback() { echo "SONUC: ROLLBACK $1"; exit 1; }
bitir_fail()     { echo "SONUC: FAIL $1"; exit 1; }

[ "$MOD" = "backend" ] || bitir_fail "unknown mode: $MOD"
[[ "$ESKI" =~ ^[0-9]+$ && "$YENI" =~ ^[0-9]+$ ]] || bitir_fail "port must be numeric"
[ -f "$ENV_DOSYA" ] || bitir_fail "env file not found: $ENV_DOSYA"
[ -f "$VHOST" ]     || bitir_fail "vhost not found: $VHOST"

# ---- 1) Yedek ----
ENV_YEDEK="${ENV_DOSYA}.yedek.port.${TS}"
VH_YEDEK="${VHOST}.yedek.port.${TS}"
cp -a "$ENV_DOSYA" "$ENV_YEDEK" || bitir_fail "could not back up env"
cp -a "$VHOST" "$VH_YEDEK"      || bitir_fail "could not back up vhost"
adim "backups taken" 1 "$ENV_YEDEK"

geri_al() {
  cp -a "$ENV_YEDEK" "$ENV_DOSYA" 2>/dev/null
  cp -a "$VH_YEDEK" "$VHOST" 2>/dev/null
  systemctl restart girginospanel >/dev/null 2>&1
  nginx -s reload >/dev/null 2>&1
}

# ---- 2) env: PANEL_LISTEN ----
# 127.0.0.1:8080 veya :8080 — yalniz PORT kismini degistir, adresi KORU.
if grep -qE '^PANEL_LISTEN=' "$ENV_DOSYA"; then
  sed -i -E "s|^(PANEL_LISTEN=.*:)[0-9]+\s*$|\1${YENI}|" "$ENV_DOSYA"
else
  echo "PANEL_LISTEN=127.0.0.1:${YENI}" >> "$ENV_DOSYA"
fi
if ! grep -qE "^PANEL_LISTEN=.*:${YENI}\s*$" "$ENV_DOSYA"; then
  geri_al; bitir_rollback "could not update env (PANEL_LISTEN)"
fi
adim "env PANEL_LISTEN" 1 "-> ${YENI}"

# ---- 3) nginx proxy_pass ----
# Once ESKI portun kac kez gectigini SAY (pozitif dogrulama icin referans).
ESKI_ADET=$(grep -cE "proxy_pass[[:space:]]+https?://(127\.0\.0\.1|localhost|\[::1\]):${ESKI}\b" "$VHOST" || true)
if [ "${ESKI_ADET:-0}" -eq 0 ]; then
  geri_al
  bitir_rollback "no proxy_pass to port :${ESKI} found in the vhost — the file is not in the expected format, left untouched"
fi

# Adres bicimi ne olursa olsun (127.0.0.1 / localhost / [::1], sondaki slash
# olsun olmasin) yalniz PORT kismini degistir.
sed -i -E "s|(proxy_pass[[:space:]]+https?://(127\.0\.0\.1|localhost|\[::1\]):)${ESKI}\b|\1${YENI}|g" "$VHOST"

# POZITIF dogrulama: yeni port en az ESKI_ADET kadar var mi VE eski port sifirlandi mi?
YENI_ADET=$(grep -cE "proxy_pass[[:space:]]+https?://(127\.0\.0\.1|localhost|\[::1\]):${YENI}\b" "$VHOST" || true)
KALAN=$(grep -cE "proxy_pass[[:space:]]+https?://(127\.0\.0\.1|localhost|\[::1\]):${ESKI}\b" "$VHOST" || true)
if [ "${YENI_ADET:-0}" -lt "$ESKI_ADET" ] || [ "${KALAN:-0}" -ne 0 ]; then
  geri_al
  bitir_rollback "could not update proxy_pass (expected ${ESKI_ADET}, new ${YENI_ADET:-0}, old remaining ${KALAN:-0})"
fi
adim "nginx proxy_pass" 1 "${ESKI} -> ${YENI} (${YENI_ADET} entries)"

# ---- 4) nginx -t ----
if ! CIKTI=$(nginx -t 2>&1); then
  geri_al; bitir_rollback "nginx -t: $(echo "$CIKTI" | tail -1)"
fi
adim "nginx -t" 1 "syntax OK"

# ---- 5) paneli yeni portla baslat ----
if ! systemctl restart girginospanel >/dev/null 2>&1; then
  geri_al
  # Rollback'in kendisini de dogrula: "geri alindi" deyip paneli olu
  # birakmak, hatanin kendisinden daha kotu.
  sleep 3
  if curl -fsS -m 4 -o /dev/null "http://127.0.0.1:${ESKI}/healthz" 2>/dev/null; then
    bitir_rollback "panel restart failed — rolled back (old port verified)"
  fi
  bitir_rollback "panel restart failed AND the rollback could not be verified — MANUAL INSPECTION NEEDED"
fi
adim "panel restart" 1 ""

# ---- 6) DOGRULA: yeni portta gercekten dinliyor mu ----
# 30s icinde saglik bekle (panel acilirken DB/goc adimlari surebilir).
BASARI=0
for _ in $(seq 1 30); do
  sleep 1
  if curl -fsS -m 3 -o /dev/null "http://127.0.0.1:${YENI}/healthz" 2>/dev/null; then BASARI=1; break; fi
  # healthz yoksa ham TCP kabul et
  if (exec 3<>"/dev/tcp/127.0.0.1/${YENI}") 2>/dev/null; then exec 3<&- 3>&-; BASARI=1; break; fi
done
if [ "$BASARI" -ne 1 ]; then
  geri_al
  # Rollback dogrulama: eski portta geri geldi mi?
  sleep 2
  if curl -fsS -m 3 -o /dev/null "http://127.0.0.1:${ESKI}/healthz" 2>/dev/null; then
    bitir_rollback "new port ${YENI} did not respond — rolled back (old port verified)"
  fi
  bitir_rollback "new port ${YENI} did not respond AND old port ${ESKI} is not responding either — MANUAL INSPECTION NEEDED"
fi
adim "new port verified" 1 "127.0.0.1:${YENI}"

# ---- 7) nginx reload (proxy hedefi degisti) ----
if ! nginx -s reload >/dev/null 2>&1; then
  adim "nginx reload" 0 "reload failed — the panel is running on the new port"
else
  adim "nginx reload" 1 ""
fi

# ---- 8) Panel disaridan (nginx uzerinden) hala erisilebiliyor mu ----
DIS_PORT=$(grep -oE '^[[:space:]]*listen[[:space:]]+(127\.0\.0\.1:)?[0-9]+[[:space:]]+ssl' "$VHOST" | grep -oE '[0-9]+' | tail -1)
if [ -n "$DIS_PORT" ]; then
  # 🔴 Bu ZINCIR TESTI belirleyicidir: panel yeni portta ayakta olsa bile
  # nginx ona ulasamiyorsa panel DISARIDAN ERISILEMEZ. Eskiden bu durum
  # yalnizca ADIM|0 olarak gecilip "SONUC: OK" deniyordu.
  ZINCIR=0
  for _ in $(seq 1 10); do
    if curl -fsSk -m 4 -o /dev/null "https://127.0.0.1:${DIS_PORT}/healthz" 2>/dev/null \
       || curl -sk -m 4 -o /dev/null -w '%{http_code}' "https://127.0.0.1:${DIS_PORT}/" 2>/dev/null | grep -qE '^[23]'; then
      ZINCIR=1; break
    fi
    sleep 1
  done
  if [ "$ZINCIR" = "1" ]; then
    adim "nginx -> backend chain" 1 ":${DIS_PORT} -> :${YENI}"
  else
    adim "nginx -> backend chain" 0 "external port :${DIS_PORT} is not responding"
    geri_al
    sleep 2
    bitir_rollback "panel is unreachable from outside (:${DIS_PORT}) — rolled back"
  fi
fi

bitir_ok
