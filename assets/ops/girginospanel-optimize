#!/usr/bin/env bash
# girginospanel-optimize — MariaDB + nginx'i sunucu kaynaklarına göre optimize eder.
# Kaynak-farkında · idempotent · güvenli (validate + rollback). Kurulumda ve sonradan çalıştırılabilir.
#
# Kullanım:
#   girginospanel-optimize            # hesapla, uygula (MariaDB restart dahil)
#   girginospanel-optimize --no-restart   # config yaz + dinamikleri SET GLOBAL ile uygula, MariaDB restart ETME
#   girginospanel-optimize --dry-run      # sadece hesaplanan değerleri göster
set -uo pipefail

NO_RESTART=0; DRY=0
for a in "$@"; do case "$a" in --no-restart) NO_RESTART=1;; --dry-run) DRY=1;; esac; done

log(){ printf '  %s\n' "$*"; }
hata(){ printf '  ✗ %s\n' "$*" >&2; }

# ---------- kaynak tespiti ----------
RAM_MB=$(free -m | awk '/^Mem:/{print $2}')
CPU=$(nproc)
[ -z "$RAM_MB" ] && RAM_MB=2048
[ -z "$CPU" ] && CPU=1

# ---------- MariaDB değerleri (kaynak-farkında) ----------
# buffer pool yüzdesi: küçük kutuda muhafazakâr (clamav/nginx/php ile paylaşımlı), büyük kutuda cömert
if   [ "$RAM_MB" -lt 2048 ]; then BP_PCT=20
elif [ "$RAM_MB" -lt 4096 ]; then BP_PCT=25
elif [ "$RAM_MB" -lt 8192 ]; then BP_PCT=40
else                              BP_PCT=50
fi
BP_MB=$(( RAM_MB * BP_PCT / 100 ))
BP_MB=$(( (BP_MB / 256) * 256 ))          # 256M katına yuvarla (instance hizalama)
[ "$BP_MB" -lt 256 ] && BP_MB=256
BP_INST=$(( BP_MB / 1024 )); [ "$BP_INST" -lt 1 ] && BP_INST=1; [ "$BP_INST" -gt 8 ] && BP_INST=8
# redo log: buffer pool'un ~1/3'ü, 128M–512M arası, 128M katı
LOG_MB=$(( BP_MB / 3 )); LOG_MB=$(( (LOG_MB / 128) * 128 ))
[ "$LOG_MB" -lt 128 ] && LOG_MB=128; [ "$LOG_MB" -gt 512 ] && LOG_MB=512
THREAD_CACHE=$(( CPU * 16 )); [ "$THREAD_CACHE" -gt 100 ] && THREAD_CACHE=100
IO_THREADS=$CPU; [ "$IO_THREADS" -gt 8 ] && IO_THREADS=8; [ "$IO_THREADS" -lt 4 ] && IO_THREADS=4

# ---------- nginx değerleri ----------
NGX_CONN=4096; [ "$RAM_MB" -lt 2048 ] && NGX_CONN=2048

echo "════════ Hesaplanan değerler (RAM=${RAM_MB}MB, CPU=${CPU}) ════════"
log "MariaDB: buffer_pool=${BP_MB}M (${BP_PCT}%, ${BP_INST} instance) · redo_log=${LOG_MB}M · thread_cache=${THREAD_CACHE} · io_threads=${IO_THREADS}"
log "MariaDB: flush_log_at_trx_commit=2 (perf) · io_capacity=1000/2000 (SSD) · skip_name_resolve=ON · utf8mb4"
log "nginx: worker_connections=${NGX_CONN} · worker_rlimit_nofile=65535 · multi_accept+epoll · http tuning"
[ "$DRY" = 1 ] && { echo "  (dry-run — değişiklik yapılmadı)"; exit 0; }

TS=$(date +%s)

# ================= MARIADB =================
echo "════════ MariaDB ════════"
MYSQL_CNF=/etc/my.cnf.d/girginospanel-tuning.cnf
[ -f "$MYSQL_CNF" ] && cp -a "$MYSQL_CNF" "${MYSQL_CNF}.bak.${TS}"
cat > "$MYSQL_CNF" <<CNF
# GirginOSPanel tuning — otomatik üretildi (RAM=${RAM_MB}MB, CPU=${CPU}). girginospanel-optimize ile yenile.
[mysqld]
# --- InnoDB buffer pool (en önemli cache; RAM'in %${BP_PCT}'i) ---
innodb_buffer_pool_size          = ${BP_MB}M
innodb_buffer_pool_instances     = ${BP_INST}
innodb_buffer_pool_dump_at_shutdown = ON
innodb_buffer_pool_load_at_startup  = ON

# --- InnoDB yazma/log ---
innodb_log_file_size             = ${LOG_MB}M
innodb_log_buffer_size           = 32M
innodb_flush_log_at_trx_commit   = 2
innodb_flush_method              = O_DIRECT
innodb_flush_neighbors           = 0
innodb_stats_on_metadata         = OFF

# --- Disk I/O (SSD/NVMe varsayımı) ---
innodb_io_capacity               = 1000
innodb_io_capacity_max           = 2000
innodb_read_io_threads           = ${IO_THREADS}
innodb_write_io_threads          = ${IO_THREADS}

# --- Bağlantı & thread ---
max_connections                  = 200
thread_cache_size                = ${THREAD_CACHE}
skip_name_resolve                = ON

# --- SQL dump/import & büyük upload dostu (phpMyAdmin, WordPress) ---
max_allowed_packet               = 256M
wait_timeout                     = 31536000
open_files_limit                 = 4294967295

# --- Tablo cache ---
table_open_cache                 = 4000
table_definition_cache           = 2000

# --- Geçici tablo & per-connection buffer (küçük tut: ×max_conn) ---
tmp_table_size                   = 64M
max_heap_table_size              = 64M
sort_buffer_size                 = 2M
join_buffer_size                 = 2M
read_buffer_size                 = 1M
read_rnd_buffer_size             = 1M

# --- Karakter seti (modern; WordPress) ---
character-set-server             = utf8mb4
collation-server                 = utf8mb4_unicode_ci

# --- Query cache kapalı (modern MariaDB) ---
query_cache_type                 = 0
query_cache_size                 = 0
CNF
log "yazıldı: $MYSQL_CNF"

# Dinamik olanları HEMEN uygula (restart'sız kazanç; restart başarısız olsa bile aktif)
mysql -u root 2>/dev/null <<SQL
SET GLOBAL innodb_io_capacity          = 1000;
SET GLOBAL innodb_io_capacity_max      = 2000;
SET GLOBAL innodb_flush_log_at_trx_commit = 2;
SET GLOBAL thread_cache_size           = ${THREAD_CACHE};
SET GLOBAL table_open_cache            = 4000;
SET GLOBAL table_definition_cache      = 2000;
SET GLOBAL innodb_stats_on_metadata    = OFF;
SET GLOBAL max_allowed_packet          = 268435456;
SET GLOBAL wait_timeout                = 31536000;
SET GLOBAL innodb_buffer_pool_dump_at_shutdown = ON;
SET GLOBAL innodb_buffer_pool_load_at_startup  = ON;
SQL
log "dinamik ayarlar SET GLOBAL ile uygulandı (restart'sız)"

# systemd LimitNOFILE — open_files_limit=4294967295'in ETKİLİ olması için gerekli.
# ⚠️ 'infinity' KULLANMA: MariaDB open_files_limit'i 64'e çökertir. Somut yüksek değer ver.
LIMITS_DIR=/etc/systemd/system/mariadb.service.d
mkdir -p "$LIMITS_DIR"
cat > "$LIMITS_DIR/girginospanel-limits.conf" <<LIM
[Service]
LimitNOFILE=1048576
LIM
systemctl daemon-reload
log "systemd LimitNOFILE=1048576 (open_files_limit için)"

if [ "$NO_RESTART" = 0 ]; then
  # restart: buffer_pool boyutu + redo_log + skip_name_resolve tam etki için gerekli
  log "MariaDB yeniden başlatılıyor (buffer_pool/redo_log/skip_name_resolve aktivasyonu)…"
  if systemctl restart mariadb && sleep 3 && systemctl is-active --quiet mariadb && mysql -u root -e "SELECT 1" >/dev/null 2>&1; then
    log "✓ MariaDB restart OK ($(mysql -u root -N -e "SELECT CONCAT(ROUND(@@innodb_buffer_pool_size/1048576),'M buffer_pool, ',@@innodb_flush_log_at_trx_commit,' flush, skip_name_resolve=',@@skip_name_resolve)" 2>/dev/null))"
  else
    hata "MariaDB restart BAŞARISIZ — tuning geri alınıyor"
    rm -f "$MYSQL_CNF"; [ -f "${MYSQL_CNF}.bak.${TS}" ] && cp -a "${MYSQL_CNF}.bak.${TS}" "$MYSQL_CNF"
    systemctl restart mariadb; sleep 3
    systemctl is-active --quiet mariadb && log "eski config ile geri döndü" || hata "MariaDB HÂLÂ DOWN — elle müdahale gerek!"
    exit 1
  fi
else
  log "(--no-restart) config yazıldı; buffer_pool/redo_log/skip_name_resolve sonraki restart'ta aktif olur"
fi

# ================= NGINX =================
echo "════════ nginx ════════"
NGINX_CONF=/etc/nginx/nginx.conf
NGX_PERF=/etc/nginx/conf.d/00-girginospanel-perf.conf
cp -a "$NGINX_CONF" "${NGINX_CONF}.bak.${TS}"

# 1) main + events (idempotent, nginx.conf içinde olmalı)
sed -i -E "s/^([[:space:]]*)worker_connections[[:space:]]+[0-9]+;/\1worker_connections ${NGX_CONN};/" "$NGINX_CONF"
grep -q 'worker_rlimit_nofile' "$NGINX_CONF" || sed -i '/^worker_processes/a worker_rlimit_nofile 65535;' "$NGINX_CONF"
grep -q 'multi_accept' "$NGINX_CONF" || sed -i '/worker_connections/a\    multi_accept on;\n    use epoll;' "$NGINX_CONF"

# 2) http-seviyesi tuning (conf.d — http{} include'a girer; nginx.conf'ta OLMAYAN direktifler)
# Not: client_max_body_size + types_hash_max_size nginx.conf http{}'de ZATEN tanımlı
# (panel yönetir) → burada TEKRAR ETME, yoksa "duplicate directive" ile nginx -t patlar.
cat > "$NGX_PERF" <<'NGX'
# GirginOSPanel nginx performans tuning — otomatik üretildi. girginospanel-optimize ile yenile.
server_tokens off;
tcp_nodelay on;
reset_timedout_connection on;
keepalive_requests 1000;
client_body_timeout 15s;
client_header_timeout 15s;
send_timeout 15s;
server_names_hash_bucket_size 128;
# PHP-FPM yanıt tamponları (yoğun yükte 502/504 azaltır) — per-location override edilebilir
fastcgi_buffers 16 16k;
fastcgi_buffer_size 32k;
fastcgi_busy_buffers_size 32k;
# gzip — GLOBAL (http): TÜM siteler (panel + müşteri vhostları) sıkıştırılır.
# text/html nginx tarafından zaten sıkıştırılır (listelenmez). comp_level 5 = iyi denge.
gzip on;
gzip_vary on;
gzip_comp_level 5;
gzip_min_length 256;
gzip_proxied any;
gzip_types text/plain text/css text/xml text/javascript application/javascript application/json application/xml application/xml+rss application/rss+xml application/atom+xml application/wasm application/vnd.ms-fontobject application/x-font-ttf font/ttf font/otf font/woff font/woff2 image/svg+xml image/x-icon;
# statik dosya deskriptör cache — açık dosya sayısını azaltır, I/O düşürür.
# DİKKAT (hosting paneli): errors=off ŞART — aksi halde bir domain/dosya HAZIR
# olmadan gelen istek "yok" sonucunu cache'ler ve dosya sonradan oluşsa bile
# stale 404/500 döner (valid süresince). errors=off + kısa valid = yeni oluşan
# domain/dosya anında servis edilir. valid 30s: dosya değişiklikleri hızlı yansır.
open_file_cache max=10000 inactive=60s;
open_file_cache_valid 30s;
open_file_cache_min_uses 2;
open_file_cache_errors off;
NGX

# 3) doğrula → başarısızsa geri al
if nginx -t >/dev/null 2>&1; then
  systemctl reload nginx
  log "✓ nginx -t OK, reload edildi (worker_connections=${NGX_CONN}, http tuning aktif)"
else
  hata "nginx -t BAŞARISIZ — geri alınıyor"
  cp -a "${NGINX_CONF}.bak.${TS}" "$NGINX_CONF"; rm -f "$NGX_PERF"
  nginx -t >/dev/null 2>&1 && systemctl reload nginx && log "eski nginx config ile geri döndü" || hata "nginx config hâlâ bozuk!"
  exit 1
fi

# ================= PHP =================
# Büyük form/import (phpMyAdmin, WordPress) takılmasın: max_input_vars=10000
# tüm PHP sürümlerine + phpMyAdmin pool'una uygula.
echo "════════ PHP ════════"
PHP_DROPIN='; GirginOSPanel: büyük form/import (phpMyAdmin, WordPress) — takılma önler
max_input_vars = 10000'
PHP_RESTART=0
# OPcache tuning (yoğun PHP yükü için; JIT sadece 8.x'te)
OPC_COMMON='; GirginOSPanel OPcache tuning — yoğun PHP yükü için
opcache.memory_consumption=256
opcache.interned_strings_buffer=16
opcache.max_accelerated_files=32531
opcache.max_wasted_percentage=10
opcache.validate_timestamps=1
opcache.revalidate_freq=2
opcache.save_comments=1
opcache.enable_file_override=0
opcache.fast_shutdown=1'
# remi sürümleri (varsa)
for d in /etc/opt/remi/php*/php.d; do
  [ -d "$d" ] || continue
  printf '%s\n' "$PHP_DROPIN" > "$d/99-girginospanel-input.ini"
  printf '%s\n' "$OPC_COMMON" > "$d/99-girginospanel-opcache.ini"
  # NOT: opcache JIT KASITLI kapalı — bazı kutularda opcode-handler eklentileriyle
  # çakışıp "JIT disabled" uyarısı basıyor (wp-cli çıktısını kirletiyor) + zaten
  # auto-disable oluyor. OPcache mem/file tuning gerçek kazanç; JIT marjinal.
  PHP_RESTART=1
done
# base php (phpMyAdmin base php-fpm)
if [ -d /etc/php.d ]; then
  printf '%s\n' "$PHP_DROPIN" > /etc/php.d/99-girginospanel-input.ini
  printf '%s\n' "$OPC_COMMON" > /etc/php.d/99-girginospanel-opcache.ini
  PHP_RESTART=1
fi
# phpMyAdmin pool açık satır (php_value)
PMA_POOL=/etc/php-fpm.d/phpmyadmin.conf
if [ -f "$PMA_POOL" ] && ! grep -q 'max_input_vars' "$PMA_POOL"; then
  cp -a "$PMA_POOL" "${PMA_POOL}.bak.${TS}"
  if grep -q 'php_value\[memory_limit\]' "$PMA_POOL"; then
    sed -i '/php_value\[memory_limit\]/a php_value[max_input_vars]      = 10000' "$PMA_POOL"
  else
    printf '\nphp_value[max_input_vars] = 10000\n' >> "$PMA_POOL"
  fi
fi
# FPM servislerini reload (çalışan olanları)
if [ "$PHP_RESTART" = 1 ]; then
  for svc in php-fpm php74-php-fpm php80-php-fpm php81-php-fpm php82-php-fpm php83-php-fpm php84-php-fpm php85-php-fpm; do
    systemctl is-active --quiet "$svc" 2>/dev/null && systemctl reload-or-restart "$svc" 2>/dev/null
  done
  log "✓ PHP max_input_vars=10000 tüm sürümlere + phpMyAdmin'e uygulandı"
fi

echo "════════ ✓ Optimizasyon tamamlandı ════════"
