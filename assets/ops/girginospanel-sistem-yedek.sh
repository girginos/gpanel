#!/bin/bash
# girginospanel-sistem-yedek — panel-backup'in KAPSAMADIGI felaket-kurtarma ogeleri.
# 3'lu multi-agent denetim sonrasi genisletildi (v2): web katmani + gercek e-postalar.
# Gunluk cron. Retention 7 gun. umask 077 -> tum ciktilar root-only (600/700).
set -uo pipefail
umask 077   # sirlar (mysql dump, secrets, ssl key) dunya-okunur OLMASIN

HEDEF=/var/backups/girginospanel-sistem
GUN=$(date +%F-%H%M)
D="$HEDEF/$GUN"
mkdir -p "$D"; chmod 700 "$HEDEF" "$D"
LOG="$D/yedek.log"; : > "$LOG"
log(){ echo "[$(date +%T)] $*" | tee -a "$LOG"; }
yt(){ # yt <cikti-adi> <tar-hedefleri...>  (yoksa atla, hata loglama)
  local ad="$1"; shift
  local mevcut=(); local p
  for p in "$@"; do [ -e "/$p" ] && mevcut+=("$p"); done
  if [ ${#mevcut[@]} -eq 0 ]; then log "-- $ad: hedef yok, atlandi"; return; fi
  if tar czf "$D/$ad.tar.gz" -C / "${mevcut[@]}" 2>>"$LOG"; then
    log "OK $ad ($(du -h "$D/$ad.tar.gz"|cut -f1))"
  else log "!! $ad tar HATASI"; fi
}

log "=== sistem yedek v2 basladi: $D ==="

# 1) TUM MySQL DB (mail + domain + panel + grantlar). InnoDB tutarli (--single-transaction).
#    NOT: 143 MyISAM tablo --single-transaction kapsaminda degil; gece penceresi + DR
#    amaci icin kabul. Tam tutarlilik istenirse MyISAM->InnoDB migrasyonu onerilir.
if mysqldump --all-databases --single-transaction --quick --routines --triggers --events 2>>"$LOG" | gzip > "$D/mysql-all.sql.gz"; then
  log "OK MySQL tum DB ($(du -h "$D/mysql-all.sql.gz"|cut -f1))"
else log "!! MySQL dump HATASI"; fi

# 2) Panel config + secrets (JWT, DB parolalari, db.key, proxy.secret)
yt etc-girginospanel etc/girginospanel

# 3) systemd unitleri (panel + eklenti + per-tenant php-fpm + ip)
( cd /etc/systemd/system 2>/dev/null && tar czf "$D/systemd-units.tar.gz" \
    girginospanel*.service girginospanel*.service.d php-fpm-c_*.service 2>>"$LOG" ) \
  && log "OK systemd-units ($(du -h "$D/systemd-units.tar.gz"|cut -f1))" || log "!! systemd-units"

# 4) OpenDKIM anahtarlari + config (mail imzalama)
yt opendkim etc/opendkim etc/opendkim.conf

# 5) named (BIND) zone + config
yt named var/named etc/named.conf etc/named

# 6) Postfix + Dovecot config + MAIL SSL sertifikalari
yt mail-config-ssl etc/postfix etc/dovecot etc/pki/mail

# 7) [YENI] WEB SSL sertifikalari + ACME store/hesap (yeniden-issue + LE limit onleme)
yt web-ssl-acme etc/pki/girginospanel opt/girginospanel/acme root/.acme.sh

# 8) [YENI] nginx + apache vhost'lari (siteler bunlarsiz servis edilemez)
yt web-vhosts etc/nginx etc/httpd

# 9) [YENI] per-tenant PHP-FPM havuz configleri (systemd unit'lerin okudugu)
yt php-fpm etc/php-fpm-tenant etc/php-fpm.d

# 10) [YENI] GERCEK E-POSTALAR (maildir) — DB'de kutu tanimi var ama mesajlar burada
yt vmail var/vmail

# 11) [YENI] cron (panel + sistem-yedek + tenant crontablari)
yt cron etc/cron.d var/spool/cron

# 12) panel uygulama (bin + frontend-dist + eklentiler/parali plugin + surum); logs HARIC
tar czf "$D/opt-girginospanel.tar.gz" -C / --exclude="opt/girginospanel/logs" \
    --exclude="opt/girginospanel/acme" opt/girginospanel 2>>"$LOG" \
  && log "OK opt-girginospanel ($(du -h "$D/opt-girginospanel.tar.gz"|cut -f1))" || log "!! opt-girginospanel"

# retention: 7 gunden eski yedek dizinleri
find "$HEDEF" -mindepth 1 -maxdepth 1 -type d -name "20*" -mtime +7 -exec rm -rf {} + 2>>"$LOG" || true

# ek guvence: cikti dosyalarini kesin 600 yap
chmod 600 "$D"/* 2>/dev/null || true

log "=== sistem yedek v2 TAMAM ==="
ls -la "$D" | tee -a "$LOG"
