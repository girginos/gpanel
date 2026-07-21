#!/usr/bin/env bash
# girginospanel-install — boş AlmaLinux 10 sunucuyu komple GirginOSPanel'e çevirir.
# Idempotent olacak şekilde tasarlandı (tekrar çalıştırılabilir). root ile çalıştır.
#
#   ./girginospanel-install.sh [--admin-parola <p>] [--admin-eposta <e>]
#
# assets/ dizini bu script'in yanında olmalı:
#   girginospanel-server  girginospanel-seed-admin  frontend-dist.tar.gz
#   migrations.tar.gz  nginx/*  php-fpm/*  phpmyadmin/*  systemd/*  ops/*
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
A="$HERE/assets"
ADMIN_PAROLA=""; ADMIN_EPOSTA="admin@local"
while [ $# -gt 0 ]; do case "$1" in
  --admin-parola) shift; ADMIN_PAROLA="$1" ;;
  --admin-eposta) shift; ADMIN_EPOSTA="$1" ;;
  *) echo "bilinmeyen: $1"; exit 2 ;;
esac; shift; done

c_g="\033[32m"; c_y="\033[33m"; c_r="\033[31m"; c_b="\033[1;34m"; c_0="\033[0m"
[ -t 1 ] || { c_g=; c_y=; c_r=; c_b=; c_0=; }
step(){ echo -e "\n${c_b}══ $* ══${c_0}"; }
ok(){ echo -e "  ${c_g}✓${c_0} $*"; }
warn(){ echo -e "  ${c_y}!${c_0} $*"; }
die(){ echo -e "  ${c_r}✗ $*${c_0}"; exit 1; }

[ "$(id -u)" = 0 ] || die "root gerekli"
[ -d "$A" ] || die "assets/ bulunamadı ($A)"
grep -qiE "AlmaLinux|Rocky|Red Hat|CentOS" /etc/os-release || warn "AlmaLinux/RHEL10 bekleniyordu — devam ediliyor"

PHP_VERS="74 80 81 82 83 84 85 86"
PHP_EXT="fpm cli mysqlnd mbstring bcmath intl gd soap opcache pdo xml zip pgsql ldap"

# ============ 1) REPO ============
step "1) Depolar (EPEL + Remi + CRB)"
dnf install -y epel-release >/dev/null 2>&1 && ok "EPEL"
rpm -q remi-release >/dev/null 2>&1 || dnf install -y https://rpms.remirepo.net/enterprise/remi-release-10.rpm >/dev/null 2>&1
rpm -q remi-release >/dev/null 2>&1 && ok "Remi" || die "Remi eklenemedi"
dnf config-manager --set-enabled crb >/dev/null 2>&1 && ok "CRB"

# ============ 2) TEMEL PAKETLER ============
step "2) Temel paketler"
dnf install -y nginx httpd mariadb-server valkey certbot python3-certbot-nginx \
  clamav clamav-update httpd-tools mod_proxy_html tar openssl policycoreutils-python-utils \
  setools-console jq bind bind-utils nftables unzip zip cronie xfsprogs sudo \
  rsync git curl >/dev/null 2>&1 \
  && ok "nginx, httpd, mariadb, valkey, certbot, clamav, bind, nftables, unzip/zip, araçlar" || die "temel paket kurulumu"

# ============ 3) PHP (5 sürüm + base + wp-cli) ============
step "3) PHP sürümleri (5 remi + base) + wp-cli"
BASE_PKGS="php php-fpm php-cli php-mysqlnd php-mbstring php-json php-pecl-redis6"
# 🔴 PHP batch kurulumu ONCESI: dnf oto-kilit kaynaklarini kapat (dnf-automatic/makecache
#    timer'i devredeyse toplu "dnf install" kilide takilir/yanlis-negatif uretir).
#    Managed panel guncellemeleri kendi yonetir; oto-update KAPALI (kilit contention + surpriz-patch onlenir).
systemctl disable --now dnf-automatic.timer dnf-makecache.timer >/dev/null 2>&1 || true
dnf install -y $BASE_PKGS >/dev/null 2>&1 && ok "base php + php-redis"
for v in $PHP_VERS; do
  pkgs=""; for e in $PHP_EXT; do pkgs="$pkgs php$v-php-$e"; done
  dnf install -y $pkgs php$v-php-pecl-redis6 >/dev/null 2>&1 && ok "php$v (+redis)" || warn "php$v bazı paketler atlandı"
done
if [ ! -x /usr/local/bin/wp ]; then
  curl -fsSL -o /usr/local/bin/wp https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar 2>/dev/null \
    && chmod +x /usr/local/bin/wp && ok "wp-cli" || warn "wp-cli indirilemedi (WordPress özellikleri için gerekli)"
else ok "wp-cli (mevcut)"; fi

# ============ 4) MARIADB ============
step "4) MariaDB"
systemctl enable --now mariadb >/dev/null 2>&1; sleep 2
systemctl is-active --quiet mariadb || die "MariaDB başlamadı"
DBPASS=$(openssl rand -hex 16)
mysql -u root <<SQL
CREATE DATABASE IF NOT EXISTS panel CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'panel'@'127.0.0.1' IDENTIFIED BY '$DBPASS';
ALTER USER 'panel'@'127.0.0.1' IDENTIFIED BY '$DBPASS';
GRANT ALL PRIVILEGES ON panel.* TO 'panel'@'127.0.0.1';
FLUSH PRIVILEGES;
SQL
ok "panel DB + kullanıcı (panel@127.0.0.1)"

# ============ 5) DİZİNLER + ENV ============
step "5) Dizinler + env"
mkdir -p /opt/girginospanel/bin /opt/girginospanel/frontend-dist /opt/girginospanel/src/migrations \
         /opt/girginospanel/pma-signon /etc/girginospanel /etc/ssl/girginospanel
JWT=$(openssl rand -hex 32); RADMIN=$(openssl rand -hex 24)
cat > /etc/girginospanel/env <<ENV
PANEL_LISTEN=127.0.0.1:8080
PANEL_ENV=production
PANEL_DB_DSN=panel:${DBPASS}@tcp(127.0.0.1:3306)/panel?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci
PANEL_JWT_SECRET=${JWT}
PANEL_JWT_LIFETIME_SEC=43200
PANEL_REDIS_ADMIN_PASS=${RADMIN}
ENV
chmod 600 /etc/girginospanel/env
ok "/etc/girginospanel/env (JWT + DB DSN + Redis admin üretildi)"

# ============ 6) ARTIFACT DEPLOY ============
step "6) Panel binary + frontend + migration"
install -m 0755 "$A/girginospanel-server" /opt/girginospanel/bin/girginospanel-server
[ -f "$A/girginospanel-seed-admin" ] && install -m 0755 "$A/girginospanel-seed-admin" /opt/girginospanel/bin/girginospanel-seed-admin
tar xzf "$A/frontend-dist.tar.gz" -C /opt/girginospanel/frontend-dist && ok "frontend-dist"
tar xzf "$A/migrations.tar.gz" -C /opt/girginospanel/src/migrations && ok "migrations ($(ls /opt/girginospanel/src/migrations/*.sql 2>/dev/null | wc -l) sql)"
# ops tool + signon
for t in "$A"/ops/*; do
  bn=$(basename "$t"); nm="${bn%.sh}"
  install -m 0755 "$t" "/usr/local/bin/$nm" 2>/dev/null
done
cp "$A/ops/"* /opt/girginospanel/src/scripts/ 2>/dev/null
ok "ops-tool'lar (/usr/local/bin: optimize, redis-setup, ftp-setup, backup-all, repair, jail, wp-redis)"

# ============ 7) PANEL SSL (self-signed) ============
step "7) Panel SSL (:8443 self-signed)"
if [ ! -f /etc/ssl/girginospanel/panel.crt ]; then
  openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
    -keyout /etc/ssl/girginospanel/panel.key -out /etc/ssl/girginospanel/panel.crt \
    -subj "/CN=girginospanel" >/dev/null 2>&1
fi
chmod 600 /etc/ssl/girginospanel/panel.key
ok "panel.crt / panel.key"

# ============ 8) NGINX ============
step "8) nginx (panel vhost + phpMyAdmin + perf)"
# http-seviyesi ayar (client_max_body_size 10240m) — idempotent.
# NOT: server_names_hash_bucket_size EKLENMEZ — girginospanel-optimize'ın 00-perf.conf'unda
# zaten var; burada da eklersek "duplicate directive" ile nginx -t patlar.
grep -q "client_max_body_size 10240m" /etc/nginx/nginx.conf || \
  sed -i '/^http {/a\    client_max_body_size 10240m;' /etc/nginx/nginx.conf
cp "$A/nginx/_panel.conf"    /etc/nginx/conf.d/_panel.conf
cp "$A/nginx/_default80.conf" /etc/nginx/conf.d/_default80.conf
cp "$A/nginx/php-fpm.conf"    /etc/nginx/conf.d/php-fpm.conf 2>/dev/null
nginx -t >/dev/null 2>&1 && ok "nginx -t OK" || { nginx -t; die "nginx config hatası"; }

# ============ 9) phpMyAdmin ============
step "9) phpMyAdmin"
mkdir -p /opt/phpmyadmin   # ÖNCE oluştur (yoksa strip-components extract patlar)
if [ ! -f /opt/phpmyadmin/index.php ]; then
  TMP=$(mktemp -d)
  if curl -fsSL -o "$TMP/pma.tar.gz" https://www.phpmyadmin.net/downloads/phpMyAdmin-latest-all-languages.tar.gz \
     && tar xzf "$TMP/pma.tar.gz" -C /opt/phpmyadmin --strip-components=1; then
    ok "phpMyAdmin indirildi + açıldı"
  else warn "phpMyAdmin indirilemedi (ağ?) — sonra elle: girginospanel-repair"; fi
  rm -rf "$TMP"
fi
if [ -f "$A/phpmyadmin/config.inc.php" ]; then
  BLOWFISH=$(openssl rand -hex 16)           # taze — prod secret DEĞİL
  PMACTRL=$(openssl rand -hex 16)            # pma control kullanıcı parolası (taze)
  sed -e "s/BLOWFISH_SECRET_BURAYA/$BLOWFISH/g" -e "s/PMA_CONTROL_PASS_BURAYA/$PMACTRL/g" \
    "$A/phpmyadmin/config.inc.php" > /opt/phpmyadmin/config.inc.php
  # pma control kullanıcısı + phpmyadmin DB + pmadb tabloları (gelişmiş özellikler)
  mysql -u root <<SQL 2>/dev/null
CREATE DATABASE IF NOT EXISTS phpmyadmin;
CREATE USER IF NOT EXISTS 'pma'@'127.0.0.1' IDENTIFIED BY '$PMACTRL';
CREATE USER IF NOT EXISTS 'pma'@'localhost' IDENTIFIED BY '$PMACTRL';
ALTER USER 'pma'@'127.0.0.1' IDENTIFIED BY '$PMACTRL';
ALTER USER 'pma'@'localhost' IDENTIFIED BY '$PMACTRL';
GRANT ALL PRIVILEGES ON phpmyadmin.* TO 'pma'@'127.0.0.1', 'pma'@'localhost';
FLUSH PRIVILEGES;
SQL
  [ -f /opt/phpmyadmin/sql/create_tables.sql ] && mysql -u root phpmyadmin < /opt/phpmyadmin/sql/create_tables.sql 2>/dev/null
fi
[ -f "$A/phpmyadmin/pma-signon.php" ] && cp "$A/phpmyadmin/pma-signon.php" /opt/girginospanel/pma-signon/ 2>/dev/null
cp "$A/php-fpm/phpmyadmin.conf" /etc/php-fpm.d/phpmyadmin.conf
mkdir -p /var/lib/phpmyadmin/{tmp,sessions}
chown -R nginx:nginx /opt/phpmyadmin /var/lib/phpmyadmin 2>/dev/null
restorecon -R /opt/phpmyadmin /var/lib/phpmyadmin >/dev/null 2>&1
setsebool -P httpd_can_network_connect_db 1 >/dev/null 2>&1
ok "phpMyAdmin pool + config + izinler"

# ============ 10) systemd + servisler ============
step "10) systemd + servisler"
cp "$A/systemd/girginospanel.service" /etc/systemd/system/girginospanel.service
# journald kalici: panel crash/db-fatal izleri reboot'ta silinmesin (reboot-dayaniklilik teshisi)
mkdir -p /var/log/journal && systemctl restart systemd-journald >/dev/null 2>&1 || true
systemctl daemon-reload
systemctl enable --now php-fpm >/dev/null 2>&1
for v in $PHP_VERS; do systemctl enable --now php$v-php-fpm >/dev/null 2>&1; done
ok "php-fpm (base + 5 sürüm)"

# ---- named (DNS sunucusu) — domainlerin ad sunucusu ----
NC=/etc/named.conf
if [ -f "$NC" ]; then
  cp -a "$NC" "$NC.gosp-bak" 2>/dev/null || true
  # dışarıdan sorgulanabilsin: tüm arayüzleri dinle (varsayılan yalnız 127.0.0.1)
  sed -i -E 's/listen-on port 53 \{[^}]*\}/listen-on port 53 { any; }/' "$NC"
  sed -i -E 's/listen-on-v6 port 53 \{[^}]*\}/listen-on-v6 port 53 { any; }/' "$NC"
  # açık-çözücü (open resolver / DNS amplification) olmasın — yalnızca yetkili DNS
  sed -i -E 's/recursion yes/recursion no/' "$NC"
  # panel zone include'u (WriteZone bunu doldurur) — idempotent
  grep -q 'girginospanel-zones.conf' "$NC" || \
    echo 'include "/etc/named/girginospanel-zones.conf";' >> "$NC"
fi
# panel zone include dosyası (boş başlar; panel domain ekledikçe dolar)
mkdir -p /etc/named
[ -f /etc/named/girginospanel-zones.conf ] || \
  printf '// girginospanel — otomatik üretildi\n' > /etc/named/girginospanel-zones.conf
chown root:named /etc/named/girginospanel-zones.conf 2>/dev/null || true
chmod 640 /etc/named/girginospanel-zones.conf 2>/dev/null || true
# zone dosyaları /var/named altında (SELinux named_zone_t context ŞART)
restorecon -R /var/named /etc/named >/dev/null 2>&1 || true
if named-checkconf >/dev/null 2>&1; then
  systemctl enable --now named >/dev/null 2>&1 && ok "named (DNS authoritative, :53 açık, recursion kapalı)" || warn "named başlatılamadı"
else
  warn "named-checkconf hata — DNS elle kontrol edilmeli"
fi

# ---- acme.sh (Let's Encrypt SSL) — panel /root/.acme.sh/acme.sh çağırır ----
# LE geçerli email ister (@ + nokta). admin@local gibi geçersizse contact'sız kaydet.
AEMAIL="$ADMIN_EPOSTA"; echo "$AEMAIL" | grep -qE '@[^@]+\.[^@]+$' || AEMAIL=""
if [ ! -x /root/.acme.sh/acme.sh ]; then
  if [ -n "$AEMAIL" ]; then curl -fsSL https://get.acme.sh 2>/dev/null | sh -s email="$AEMAIL" >/dev/null 2>&1 || true
  else curl -fsSL https://get.acme.sh 2>/dev/null | sh >/dev/null 2>&1 || true; fi
fi
if [ -x /root/.acme.sh/acme.sh ]; then
  /root/.acme.sh/acme.sh --set-default-ca --server letsencrypt >/dev/null 2>&1
  # LE hesabını ŞİMDİ kaydet (geçerli email varsa onunla, yoksa contact'sız) — issue anında hata olmasın
  if [ -n "$AEMAIL" ]; then /root/.acme.sh/acme.sh --register-account -m "$AEMAIL" --server letsencrypt >/dev/null 2>&1
  else /root/.acme.sh/acme.sh --register-account --server letsencrypt >/dev/null 2>&1; fi
  ok "acme.sh (Let's Encrypt CA + hesap kayıtlı + oto-yenileme cron)"
else
  warn "acme.sh kurulamadı — Let's Encrypt SSL için elle: curl https://get.acme.sh | sh"
fi

# ---- httpd (Apache backend — web_backend=apache seçeneği, nginx ön-proxy) ----
# nginx :80'de olduğu için Apache 127.0.0.1:10080'de dinler (mod_proxy_fcgi → php-fpm)
if [ -f /etc/httpd/conf/httpd.conf ]; then
  if grep -qE "^Listen 80$" /etc/httpd/conf/httpd.conf; then
    sed -i "s/^Listen 80$/Listen 127.0.0.1:10080/" /etc/httpd/conf/httpd.conf
  elif ! grep -qE "^Listen 127.0.0.1:10080" /etc/httpd/conf/httpd.conf; then
    echo "Listen 127.0.0.1:10080" >> /etc/httpd/conf/httpd.conf
  fi
  semanage port -l 2>/dev/null | grep -qE "http_port_t.*\b10080\b" || \
    semanage port -a -t http_port_t -p tcp 10080 2>/dev/null || \
    semanage port -m -t http_port_t -p tcp 10080 2>/dev/null
  if apachectl configtest >/dev/null 2>&1; then
    systemctl enable --now httpd >/dev/null 2>&1 && ok "httpd (Apache backend :10080, mod_proxy_fcgi)" || warn "httpd başlatılamadı"
  else warn "httpd configtest hata — Apache backend elle kontrol"; fi
fi

# ---- composer (per-domain PHP bağımlılık yönetimi) ----
if [ ! -x /usr/local/bin/composer ]; then
  curl -sS https://getcomposer.org/installer 2>/dev/null | php -- --install-dir=/usr/local/bin --filename=composer >/dev/null 2>&1
fi
[ -x /usr/local/bin/composer ] && ok "composer ($(/usr/local/bin/composer --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1))" || warn "composer kurulamadı"

# ---- günlük yedek cron (girginospanel-backup-all 03:00 UTC) ----
cat > /etc/cron.d/girginospanel-backup <<'CRON'
# girginospanel — günlük planlı yedek 03:00 UTC
SHELL=/bin/bash
PATH=/usr/local/bin:/usr/bin:/bin
0 3 * * * root /usr/local/bin/girginospanel-backup-all
CRON
ok "günlük yedek cron (03:00 UTC)"

# SELinux
setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1 && ok "SELinux httpd_can_network_connect"
restorecon -R /opt/girginospanel/bin /opt/girginospanel/frontend-dist >/dev/null 2>&1

# ============ 11) Valkey + optimize ============
step "11) Valkey (Redis) + performans tuning"
command -v girginospanel-redis-setup >/dev/null 2>&1 && girginospanel-redis-setup >/dev/null 2>&1 && ok "girginospanel-redis-setup" || warn "redis-setup atlandı"
command -v girginospanel-optimize >/dev/null 2>&1 && girginospanel-optimize >/dev/null 2>&1 && ok "girginospanel-optimize" || warn "optimize atlandı"
# WAF (ModSecurity + OWASP CRS) altyapısı — idempotent, per-domain opt-in (modül yükleme zararsız).
# İlk kurulumda connector derlemesi birkaç dakika sürebilir; başarısız olursa kurulum durmaz.
command -v girginospanel-waf-setup >/dev/null 2>&1 && girginospanel-waf-setup >/dev/null 2>&1 && ok "girginospanel-waf-setup (ModSecurity+CRS)" || warn "waf-setup atlandı (panel WAF modülsüz graceful çalışır)"

# ============ 12) Panel başlat (migration startup'ta koşar) ============
step "12) Panel başlatılıyor"
systemctl enable --now girginospanel >/dev/null 2>&1; sleep 3
systemctl enable --now nginx >/dev/null 2>&1; systemctl restart nginx >/dev/null 2>&1
if systemctl is-active --quiet girginospanel; then ok "girginospanel ACTIVE"; else journalctl -u girginospanel --no-pager -n 20; die "panel başlamadı"; fi

# ---- FTP setup (Pure-FTPd) — ŞİMDİ çalışır: migration ftp_accounts tablosunu oluşturdu ----
# (step 11'de değil çünkü GRANT SELECT ON panel.ftp_accounts tablo yokken patlıyordu)
sleep 2
command -v girginospanel-ftp-setup >/dev/null 2>&1 && girginospanel-ftp-setup >/dev/null 2>&1 && ok "girginospanel-ftp-setup (Pure-FTPd, MySQL backend)" || warn "ftp-setup atlandı"

# ============ 13) Yönetici erişimi ============
# 🔴 Panel admin girişi = sunucunun ROOT kullanıcısı (/etc/shadow hash doğrulaması).
# Ayrı panel parolası YOKTUR. Giriş: kullanıcı 'root' + bu sunucunun root parolası.
step "13) Yönetici erişimi (root + /etc/shadow)"
DSN="panel:${DBPASS}@tcp(127.0.0.1:3306)/panel?parseTime=true"
if [ -x /opt/girginospanel/bin/girginospanel-seed-admin ]; then
  # yardımcı users kaydı (ownership/audit); giriş yine sunucu root parolasıyla (/etc/shadow hash) doğrulanır
  /opt/girginospanel/bin/girginospanel-seed-admin -dsn "$DSN" -kullanici root \
    -parola "$(openssl rand -hex 16)" -eposta "$ADMIN_EPOSTA" >/dev/null 2>&1 \
    && ok "yönetici kaydı hazır" || warn "seed atlandı (kritik değil)"
fi
# root profili BOŞ gelsin — seed-admin'in sahte 'admin@local'/'Sistem Yöneticisi'
# değerlerini temizle (kullanıcı Profil sayfasından doldurur)
mysql panel -e "UPDATE users SET email='', full_name='' WHERE username='root' AND email='admin@local';" >/dev/null 2>&1 || true
ok "Giriş: kullanıcı 'root' + bu sunucunun root parolası"

# ============ 14) İzin onarımı ============
step "14) İzin/SELinux onarımı"
command -v girginospanel-repair >/dev/null 2>&1 && girginospanel-repair --quiet >/dev/null 2>&1 && ok "girginospanel-repair" || warn "repair atlandı"

# ============ 15) DOĞRULAMA ============
step "15) Doğrulama"
IP=$(hostname -I 2>/dev/null | awk '{print $1}')
CODE=$(curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:8443/ 2>/dev/null)
API=$(curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:8443/api/v1/domains 2>/dev/null)
echo -e "  servisler: $(systemctl is-active mariadb nginx valkey php-fpm named pure-ftpd girginospanel | tr '\n' ' ')"
echo -e "  panel :8443 → HTTP $CODE   ·   API (auth) → HTTP $API   ·   DNS :53 → $(systemctl is-active named)   ·   FTP :21 → $(systemctl is-active pure-ftpd)"
echo -e "  araçlar: SSL/acme.sh $([ -x /root/.acme.sh/acme.sh ] && echo ✓ || echo ✗)   ·   firewall/nft $(command -v nft >/dev/null && echo ✓ || echo ✗)   ·   unzip/zip $(command -v unzip >/dev/null && command -v zip >/dev/null && echo ✓ || echo ✗)   ·   composer $(command -v composer >/dev/null && echo ✓ || echo ✗)   ·   apache/httpd $(systemctl is-active httpd)"
echo
echo -e "${c_g}═══════════════════════════════════════════════${c_0}"
echo -e "${c_g} ✓ GirginOSPanel kurulumu tamamlandı${c_0}"
echo -e "   Panel:  ${c_b}https://${IP:-SUNUCU_IP}:8443${c_0}"
echo -e "   Kullanıcı: ${c_b}root${c_0}   Parola: ${c_b}bu sunucunun root parolası${c_0}"
echo -e "   (panel admin girişi sunucu root parolasını /etc/shadow hash'i ile doğrular)"
echo -e "${c_g}═══════════════════════════════════════════════${c_0}"
