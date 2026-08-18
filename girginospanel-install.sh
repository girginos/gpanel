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

# 🔴 PATH'İ KENDİMİZ KURUYORUZ — `sudo` /usr/local/bin'i ATAR.
# AlmaLinux/RHEL varsayılanı:  Defaults secure_path = /sbin:/bin:/usr/sbin:/usr/bin
# Belgelenen kurulum/güncelleme komutu `curl ... | sudo bash` olduğu için bu
# yol HER MÜŞTERİDE çalışır ve kendi araçlarımız (girginospanel-*, composer,
# wp) bare-name çağrıldığında "command not found" verir. Ölçüldü: araç dosya
# olarak VARDI, yalnızca PATH'te yoktu; kurulum yine de yeşil bitiyordu.
case ":$PATH:" in
  *:/usr/local/bin:*) : ;;
  *) export PATH="/usr/local/sbin:/usr/local/bin:$PATH" ;;
esac


# --waf : ModSecurity v3 + OWASP CRS kur (KAYNAKTAN DERLER, ~10 dk).
# Varsayilan KAPALI — her kurulumu yavaslatmamak icin. Sonradan da kurulur:
#   girginospanel-waf-setup
KUR_WAF=0

HERE="$(cd "$(dirname "$0")" && pwd)"
A="$HERE/assets"
ADMIN_PAROLA=""; ADMIN_EPOSTA="admin@local"
while [ $# -gt 0 ]; do case "$1" in
  --admin-parola) shift; ADMIN_PAROLA="$1" ;;
  --admin-eposta) shift; ADMIN_EPOSTA="$1" ;;
  --waf) KUR_WAF=1 ;;
  -h|--help)
    echo "kullanim: $0 [--admin-parola P] [--admin-eposta E] [--waf]"
    echo "  --waf : ModSecurity v3 + OWASP CRS kur (kaynaktan derler, ~10 dk)"
    exit 0 ;;
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

# 🔴 php86 remi'de HENUZ YAYINLANMADI. Listede kalirsa her kurulumda
# sessiz basarisizlik olur: paket kurulmaz, /etc/opt/remi/php86 hic
# olusmaz, `systemctl enable php86-php-fpm` ciktisi yutulur.
PHP_VERS="74 80 81 82 83 84 85"
PHP_EXT="fpm cli mysqlnd mbstring bcmath intl gd soap opcache pdo xml zip pgsql ldap"

# ============ 1) REPO ============
step "1) Depolar (EPEL + Remi + CRB)"
dnf install -y epel-release >/dev/null 2>&1 && ok "EPEL"
# 🔴 EPEL SART: unrar, clamav-freshclam, sshpass EPEL'den geliyor. Eklenemezse
# asagidaki TEK `dnf install` komple non-zero doner, hicbir paket kurulmaz ve
# "temel paket kurulumu" hatasinin KOK NEDENI gorunmez.
rpm -q epel-release >/dev/null 2>&1 || dnf repolist enabled 2>/dev/null | grep -qi epel \
  || die "EPEL deposu eklenemedi — unrar/clamav-freshclam/sshpass kurulamaz"
rpm -q remi-release >/dev/null 2>&1 || dnf install -y https://rpms.remirepo.net/enterprise/remi-release-10.rpm >/dev/null 2>&1
rpm -q remi-release >/dev/null 2>&1 && ok "Remi" || die "Remi eklenemedi"
dnf config-manager --set-enabled crb >/dev/null 2>&1 && ok "CRB"

# ============ 2) TEMEL PAKETLER ============
step "2) Temel paketler"
dnf install -y nginx httpd mariadb-server valkey certbot python3-certbot-nginx \
  clamav clamav-freshclam httpd-tools mod_proxy_html tar openssl policycoreutils-python-utils \
  setools-console jq bind bind-utils nftables unzip zip cronie xfsprogs sudo \
  bubblewrap rsync git curl acl \
  bzip2 lftp sshpass unrar bsdtar >/dev/null 2>&1 \
  && ok "nginx, httpd, mariadb, valkey, certbot, clamav, bind, nftables, unzip/zip/bzip2, bubblewrap, acl, araçlar" || die "temel paket kurulumu"

# RAR açıcı (dosya yöneticisi .rar extract) — PRİMER: bsdtar (libarchive, appstream base'de
# GÜVENİLİR RAR/RAR5 okur; kendisi de path-traversal reddeder). 🔴 NOT: AlmaLinux 10 default
# `7z` (7-Zip 26.02) RAR codec İÇERMEZ → kullanılmaz. bsdtar yoksa unar/unrar fallback.
if command -v bsdtar >/dev/null 2>&1 || command -v unar >/dev/null 2>&1 || command -v unrar >/dev/null 2>&1; then
  ok "RAR açıcı mevcut ($(command -v bsdtar unar unrar 2>/dev/null | head -1))"
elif dnf install -y bsdtar >/dev/null 2>&1; then
  ok "bsdtar (libarchive — rar/rar5/zip/7z extract)"
elif dnf install -y unar >/dev/null 2>&1 || dnf install -y unrar >/dev/null 2>&1; then
  ok "unar/unrar (rar extract)"
else
  warn "RAR açıcı kurulamadı — dosya yöneticisi .rar extract devre dışı (zip/tar çalışır)"
fi

# ============ 2b) DİSK KOTASI (XFS user quota — CloudLinux paritesi) ============
# Per-tenant disk + inode kotası XFS *user* quota ile uygulanır (dosyalar c_<sk>:c_<sk>
# sahipli → user quota tam eşleşir + kaçış-korumalı). Kök fs XFS + `noquota` ise kota
# ancak MOUNT anında açılır (canlı remount ile açılamaz) → GRUB'a `rootflags=uquota` yaz.
# Taze kurulumda kurulum sonrası reboot ile kota AKTİF gelir.
step "2b) Disk kotası (XFS user quota)"
dnf install -y quota xfsprogs >/dev/null 2>&1 && ok "quota + xfsprogs" || warn "quota paketleri atlandı"
ROOTFS_TYPE=$(findmnt -no FSTYPE / 2>/dev/null || echo "")
ROOTFS_OPTS=$(findmnt -no OPTIONS / 2>/dev/null || echo "")
if [ "$ROOTFS_TYPE" != "xfs" ]; then
  warn "kök fs XFS değil ($ROOTFS_TYPE) — XFS disk kotası atlandı"
elif echo "$ROOTFS_OPTS" | grep -qwE 'usrquota|uquota|quota'; then
  ok "kök XFS user quota zaten aktif"
else
  if grep -q 'rootflags=uquota' /etc/default/grub 2>/dev/null; then
    ok "GRUB rootflags=uquota zaten ekli"
  else
    if grep -q '^GRUB_CMDLINE_LINUX=' /etc/default/grub 2>/dev/null; then
      sed -i 's/^\(GRUB_CMDLINE_LINUX="[^"]*\)"/\1 rootflags=uquota"/' /etc/default/grub
    else
      echo 'GRUB_CMDLINE_LINUX="rootflags=uquota"' >> /etc/default/grub
    fi
    # mevcut boot girdilerini de güncelle (BLS) + grub.cfg'yi yeniden üret (BIOS + EFI).
    command -v grubby >/dev/null 2>&1 && grubby --update-kernel=ALL --args="rootflags=uquota" >/dev/null 2>&1 || true
    grub2-mkconfig -o /boot/grub2/grub.cfg >/dev/null 2>&1 || true
    for cfg in /boot/efi/EFI/*/grub.cfg; do [ -f "$cfg" ] && grub2-mkconfig -o "$cfg" >/dev/null 2>&1 || true; done
    ok "GRUB rootflags=uquota eklendi (kök XFS)"
  fi
  warn "Disk kotası için TEK SEFERLİK reboot sonrası aktif olur (kök fs remount ile açılamaz)."
fi

# ============ 3) PHP (5 sürüm + base + wp-cli) ============
# ============ 2c) FIREWALL — firewalld KAPAT, panel devralır ============
# 🔴 Panel firewall'ı kendi nftables tablosunu (girginos_fw) yönetir. AlmaLinux 10
# varsayılan firewalld ile ÇAKIŞIR: nftables'ta 'drop' kesindir → firewalld panelin
# portlarını (8443/80/443/53/21) düşürür, panelin 'accept'i bunu ezemez. Tek otorite
# panel olsun diye firewalld'yi durdur+kapat+mask ediyoruz. (Panel binary'si açılışta
# ayrıca FirewalldDevral ile bunu garanti eder — bu adım kurulumda erken kapatır.)
step "2c) Firewall (firewalld kapat — panel devralır)"
if systemctl cat firewalld.service >/dev/null 2>&1; then
  systemctl disable --now firewalld >/dev/null 2>&1 || true
  systemctl mask firewalld >/dev/null 2>&1 || true
  ok "firewalld durduruldu + mask edildi (tek firewall = panel nftables)"
else
  ok "firewalld kurulu değil — panel nftables zaten tek firewall"
fi

step "3) PHP sürümleri (5 remi + base) + wp-cli"
BASE_PKGS="php php-fpm php-cli php-mysqlnd php-mbstring php-json php-pecl-zip php-pecl-redis6 php-gd php-bcmath php-intl php-soap php-ldap php-sodium php-opcache"
# 🔴 PHP batch kurulumu ONCESI: dnf oto-kilit kaynaklarini kapat (dnf-automatic/makecache
#    timer'i devredeyse toplu "dnf install" kilide takilir/yanlis-negatif uretir).
#    Managed panel guncellemeleri kendi yonetir; oto-update KAPALI (kilit contention + surpriz-patch onlenir).
systemctl disable --now dnf-automatic.timer dnf-makecache.timer >/dev/null 2>&1 || true
dnf install -y $BASE_PKGS >/dev/null 2>&1 && ok "base php + php-redis"

# 🔴 DOGRULAMA: yukaridaki dnf `2>/dev/null` ile sessiz; eklenti kurulmazsa
# kurulum "basarili" gorunur ama phpMyAdmin/lisans dogrulama calismaz.
php_eksik=""
# 🔴 `grep -qix "$_m"` TAM SATIR eslesmesi ister. `php -m` opcache modulunu
# "Zend OPcache" olarak listeler -> kosul ASLA tutmaz ve her kurulumda
# "base PHP eklentileri EKSIK: opcache" yazilir, oysa modul YUKLUDUR
# (olculdu: php -m | grep -ci zend.opcache = 2). Ayrica `php -m | grep -q`
# SIGPIPE uretir; listeyi bir kez alip kabuk-ici substring testi yapiyoruz.
PHP_MODLISTE=$(php -m 2>/dev/null | tr "A-Z" "a-z" | tr -d " ")
for _m in gd bcmath intl soap ldap sodium opcache mysqlnd mbstring zip; do
  case "$PHP_MODLISTE" in
    *"$_m"*) : ;;
    *) php_eksik="$php_eksik $_m" ;;
  esac
done
if [ -n "$php_eksik" ]; then
  warn "base PHP eklentileri EKSIK:$php_eksik — phpMyAdmin/lisans dogrulama etkilenebilir"
else
  ok "base PHP eklentileri dogrulandi (gd, bcmath, intl, soap, ldap, sodium, opcache)"
fi
for v in $PHP_VERS; do
  pkgs=""; for e in $PHP_EXT; do pkgs="$pkgs php$v-php-$e"; done
  dnf install -y $pkgs php$v-php-pecl-redis6 >/dev/null 2>&1 && ok "php$v (+redis)" || warn "php$v bazı paketler atlandı"
done
# 🔴 wp-cli kurulumu ARACA devredildi. Eskiden burada satır içi bir curl
# vardı; hatayı `2>/dev/null` ile gizliyor, yalnızca `warn` basıp kuruluma
# devam ediyordu. GitHub hız sınırı (429/503) verdiğinde dosya hiç inmiyor,
# kurulum "başarılı" diyor ve WordPress sayfası çalışma anında
# "Could not open input file: /usr/local/bin/wp" ile patlıyordu.
# Araç: kendi aynamızı önceler, indirdiğini ÇALIŞTIRARAK doğrular, ve
# updater tarafından her güncellemede tekrar çağrılır (self-heal).
if [ -f "$A/ops/girginospanel-wpcli-kur" ]; then
  install -m 0755 "$A/ops/girginospanel-wpcli-kur" /usr/local/bin/girginospanel-wpcli-kur
  if girginospanel-wpcli-kur; then
    WPCLI_DURUM="OK"
  else
    WPCLI_DURUM="YOK"
    warn "wp-cli kurulamadı — WordPress özellikleri çalışmaz. Sonra: girginospanel-wpcli-kur"
  fi
else
  WPCLI_DURUM="ARAC-YOK"
  warn "ops/girginospanel-wpcli-kur pakette yok — wp-cli kurulmadı"
fi

# WordPress ön koşulları: SELinux kiracı dizin etiketleri + imagick.
# 🔴 İkisi de kurulumda SESSİZCE atlanıyordu ve canlı müşteride ard arda
# patladı: Enforcing'te WordPress eklenti kuramıyor ("FTP bilgileri"
# formu, sonra "Permission denied"), imagick yok diye Site Health uyarıyor.
if [ -f "$A/ops/girginospanel-wp-onkosul" ]; then
  install -m 0755 "$A/ops/girginospanel-wp-onkosul" /usr/local/bin/girginospanel-wp-onkosul
  girginospanel-wp-onkosul || warn "WordPress ön koşulları eksik kaldı — sonra: girginospanel-wp-onkosul"
else
  warn "ops/girginospanel-wp-onkosul pakette yok — SELinux etiketi + imagick atlandı"
fi

# ============ 4) MARIADB ============
step "4) MariaDB"
systemctl enable --now mariadb >/dev/null 2>&1; sleep 2
systemctl is-active --quiet mariadb || die "MariaDB başlamadı"

# my.cnf güvenlik sertleştirmesi: MySQL dışa KAPALI (yalnız loopback) + LOCAL INFILE kapalı.
# Panel ve müşteri siteleri 127.0.0.1 üzerinden bağlanır; 3306 internete AÇILMAZ.
cat > /etc/my.cnf.d/zz-girginospanel-security.cnf <<'MYCNF'
# GirginOSPanel güvenlik sertleştirmesi (installer)
[mysqld]
bind-address = 127.0.0.1
local-infile = 0
MYCNF
systemctl restart mariadb >/dev/null 2>&1; sleep 2
systemctl is-active --quiet mariadb || die "MariaDB (güvenlik sertleştirmesi sonrası) başlamadı"
ok "MariaDB güvenlik: 3306 dışa kapalı (bind 127.0.0.1) + local-infile kapalı"

# 🔴 SIR ROTASYONU YOK — kurulum sır bakımından IDEMPOTENT olmalı.
# Eski davranış: her çalıştırmada YENİ rastgele DBPASS üretilip canlı MySQL
# kullanıcısına ALTER USER uygulanıyordu. Betik aynı sunucuda ikinci kez koştuğunda
# (dev kanalı "güncelleme" akışı tam olarak budur) çalışan panel süreci hâlâ ESKİ
# parolayı bellekte tutuyordu → bu adım ile adım 12'deki restart arasındaki TÜM
# istekler `Error 1045 Access denied for user 'panel'@'127.0.0.1'` alıyordu; betik
# o aralıkta herhangi bir nedenle die ederse panel KALICI bozuk kalıyordu.
# (Oturum kontrolü DB hatasında fail-open olduğundan bu aynı zamanda bir güvenlik
# penceresiydi — ayrı iş.)
# Yeni davranış: sır YOKSA üretilir, VARSA env'deki değer aynen korunur ve MySQL
# kullanıcısı ona hizalanır — sürüklenmiş bir parolayı da onarır. N'inci koşu
# 1'inci koşuyla aynı sonucu verir.
ENVF=/etc/girginospanel/env
env_deger() { [ -f "$ENVF" ] && sed -n "s/^$1=//p" "$ENVF" | head -1; }

# DSN biçimi: panel:<PAROLA>@tcp(127.0.0.1:3306)/panel?...  (parola hex → '@' içermez)
DBPASS=$(env_deger PANEL_DB_DSN | sed -n 's/^panel:\(.*\)@tcp(.*/\1/p')
if [ -n "$DBPASS" ]; then DBPASS_KAYNAK="mevcut env'den korundu"
else DBPASS=$(openssl rand -hex 16); DBPASS_KAYNAK="yeni üretildi"; fi

mysql -u root <<SQL
CREATE DATABASE IF NOT EXISTS panel CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'panel'@'127.0.0.1' IDENTIFIED BY '$DBPASS';
ALTER USER 'panel'@'127.0.0.1' IDENTIFIED BY '$DBPASS';
GRANT ALL PRIVILEGES ON panel.* TO 'panel'@'127.0.0.1';
FLUSH PRIVILEGES;
SQL
# Doğrula: "ALTER USER hata vermedi" YETMEZ — parolanın gerçekten çalıştığını panel
# kullanıcısıyla bağlanarak kanıtla. Aksi halde bozukluk ancak adım 12'de görülürdü.
mysql -u panel -p"$DBPASS" -h 127.0.0.1 -e 'SELECT 1' panel >/dev/null 2>&1 \
  || die "panel DB kullanıcısı doğrulanamadı (parola env ile hizalanmadı) — kurulum durduruldu"
ok "panel DB + kullanıcı (panel@127.0.0.1) — parola $DBPASS_KAYNAK, bağlantı DOĞRULANDI"

# ============ 5) DİZİNLER + ENV ============
step "5) Dizinler + env"
# 🔴 src/scripts SART: assets/ops dosyalari buraya kopyalanir ve SSH
# chroot jail config-i (50-gosp-jail.conf) buradan okunur. Dizin yoksa
# `cp ... 2>/dev/null` SESSIZCE basarisiz olur; panel her acilista
# 'SSH IZOLASYON UYGULANMADI' loglar ve kiraci CHROOTSUZ tam kabuk alir.
mkdir -p /opt/girginospanel/src/scripts \
  /opt/girginospanel/bin /opt/girginospanel/frontend-dist /opt/girginospanel/src/migrations \
         /opt/girginospanel/src/eklentiler /opt/girginospanel/eklentiler \
         /opt/girginospanel/pma-signon /etc/girginospanel /etc/ssl/girginospanel
# Sırlar: DBPASS gibi JWT ve Redis admin parolası da YALNIZCA yoksa üretilir.
# JWT rotasyonu tüm oturumları düşürür; Redis parolası rotasyonu redis-setup'ın
# yazdığı değerle senkron kalmayabilir. Yeniden kurulumda ikisine de dokunma.
JWT=$(env_deger PANEL_JWT_SECRET);           [ -n "$JWT" ]    || JWT=$(openssl rand -hex 32)
RADMIN=$(env_deger PANEL_REDIS_ADMIN_PASS);  [ -n "$RADMIN" ] || RADMIN=$(openssl rand -hex 24)
OMUR=$(env_deger PANEL_JWT_LIFETIME_SEC);    [ -n "$OMUR" ]   || OMUR=43200
DINLE=$(env_deger PANEL_LISTEN);             [ -n "$DINLE" ]  || DINLE=127.0.0.1:8080

# 🔴 Bizim yönetmediğimiz anahtarları KORU. Eski `cat > env` env'i komple eziyordu;
# sonradan eklenen PANEL_LISANS_SUNUCU gibi satırlar her kurulumda siliniyor ve panel
# lisans sunucusu için koddaki/DB'deki varsayılana geri düşüyordu.
EKSTRA=""
if [ -f "$ENVF" ]; then
  EKSTRA=$(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$ENVF" \
    | grep -vE '^(PANEL_LISTEN|PANEL_ENV|PANEL_DB_DSN|PANEL_JWT_SECRET|PANEL_JWT_LIFETIME_SEC|PANEL_REDIS_ADMIN_PASS)=')
fi

# Atomik yaz: yazma sırasında betik ölürse yarım/boş env kalmasın (env'siz panel açılmaz).
ENVTMP=$(mktemp /etc/girginospanel/.env.XXXXXX)
chmod 600 "$ENVTMP"
cat > "$ENVTMP" <<ENV
PANEL_LISTEN=${DINLE}
PANEL_ENV=production
PANEL_DB_DSN=panel:${DBPASS}@tcp(127.0.0.1:3306)/panel?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci
PANEL_JWT_SECRET=${JWT}
PANEL_JWT_LIFETIME_SEC=${OMUR}
PANEL_REDIS_ADMIN_PASS=${RADMIN}
ENV
if [ -n "$EKSTRA" ]; then printf '%s\n' "$EKSTRA" >> "$ENVTMP"; fi
mv -f "$ENVTMP" "$ENVF"
chmod 600 "$ENVF"
EKSAY=0; [ -n "$EKSTRA" ] && EKSAY=$(printf '%s\n' "$EKSTRA" | wc -l)
ok "$ENVF (DB DSN + JWT + Redis admin korundu/üretildi; $EKSAY ek anahtar korundu)"

# ============ 6) ARTIFACT DEPLOY ============
step "6) Panel binary + frontend + migration"
install -m 0755 "$A/girginospanel-server" /opt/girginospanel/bin/girginospanel-server
[ -f "$A/girginospanel-seed-admin" ] && install -m 0755 "$A/girginospanel-seed-admin" /opt/girginospanel/bin/girginospanel-seed-admin
tar xzf "$A/frontend-dist.tar.gz" -C /opt/girginospanel/frontend-dist && ok "frontend-dist"
tar xzf "$A/migrations.tar.gz" -C /opt/girginospanel/src/migrations && ok "migrations ($(ls /opt/girginospanel/src/migrations/*.sql 2>/dev/null | wc -l) sql)"
# Lisanslı eklenti payload'ları: ikili sunucuya gelir ama gate KAPALI (aktif=0).
# Lisans girilene kadar çalıştırılmaz; lisans girilince kurulum bunu yerine koyar.
if [ -d "$A/eklentiler" ]; then
  cp -a "$A/eklentiler/." /opt/girginospanel/src/eklentiler/ 2>/dev/null
  chmod -R 0755 /opt/girginospanel/src/eklentiler 2>/dev/null
  ok "eklenti payload ($(find /opt/girginospanel/src/eklentiler -type f 2>/dev/null | wc -l) dosya)"
fi
# ops tool + signon
# 🔴 Iki ayri hedef, iki ayri anlam:
#   /usr/local/bin        -> operatorun elle calistirdigi KOMUTLAR
#   src/scripts           -> panelin RUNTIME'da okudugu kaynak dosyalar
#                            (SSH chroot jail betigi + sshd config sablonu)
# Eskiden `.conf` dosyalari da /usr/local/bin'e 0755 ile kuruluyordu (yanlis
# yer, yanlis izin) ve src/scripts kopyalamasi `2>/dev/null` ile YUTULUYORDU:
# dizin olmadigi icin sessizce basarisiz oluyor, panel her acilista
# "SSH IZOLASYON UYGULANMADI" logluyor ve kiraci CHROOTSUZ tam kabuk aliyordu.
mkdir -p /opt/girginospanel/src/scripts
_ops_n=0
for t in "$A"/ops/*; do
  [ -f "$t" ] || continue
  bn=$(basename "$t")
  case "$bn" in
    *.conf|*.service|*.timer)
      # Yapilandirma: yalnizca kaynak dizinine, calistirilabilir DEGIL
      install -m 0644 "$t" "/opt/girginospanel/src/scripts/$bn" || die "ops config: $bn"
      ;;
    *)
      nm="${bn%.sh}"
      install -m 0755 "$t" "/usr/local/bin/$nm" || die "ops tool: $nm"
      # Panel bazi betikleri runtime'da buradan okur (jail vb.)
      install -m 0755 "$t" "/opt/girginospanel/src/scripts/$nm" || die "ops kaynak: $nm"
      ;;
  esac
  _ops_n=$((_ops_n+1))
done
# Dogrula: SSH izolasyonu icin sart olan iki dosya gercekten yerinde mi?
for zorunlu in 50-gosp-jail.conf girginospanel-jail; do
  # 🔴 die, warn DEGIL: bu dosyalar olmadan SSH acilan kiraci chroot'a
  # HAPSEDILEMEZ ve sunucudaki her seyi gorur. Guvenlik arizasiyla
  # "kurulum tamamlandi" demek, sessizce savunmasiz birakmak olur.
  [ -e "/opt/girginospanel/src/scripts/$zorunlu" ] \
    || die "ops: $zorunlu src/scripts icinde YOK — SSH chroot izolasyonu KURULAMAZ (pakette eksik)"
done
ok "ops-tool'lar ($_ops_n dosya: /usr/local/bin + src/scripts)"

# 🔴 COZULEBILIRLIK NOBETCISI: dosyayi kurmak YETMEZ, PATH ten
# cagrilabildigi de kanitlanmali. `sudo` /usr/local/bin i PATH ten attigi
# icin araclar kurulu oldugu halde "command not found" veriyor ve kurulum
# yine YESIL bitiyordu (49.12.158.182 de tam olarak bu oldu: 6 adim sessizce
# atlandi). Olcum URETIM yolunu (command -v) kullanir, kendi konusunu kurmaz.
_coz_eksik=""
for _t in girginospanel-wpcli-kur girginospanel-wp-onkosul girginospanel-repair \
          girginospanel-optimize girginospanel-redis-setup girginospanel-ftp-setup; do
  [ -x "/usr/local/bin/$_t" ] || continue
  command -v "$_t" >/dev/null 2>&1 || _coz_eksik="$_coz_eksik $_t"
done
if [ -n "$_coz_eksik" ]; then
  die "ops araclari kurulu ama PATH ten cagrilamiyor:$_coz_eksik  (PATH=$PATH)"
fi
ok "ops araclari PATH ten cozulebiliyor"

# 🔴 port-swap helper OZEL YOL: panel bunu tam olarak
# /usr/local/sbin/girginospanel-port-swap.sh yolunda arar
# (internal/portyonetim/degistir.go: BackendHelperYolu). Ust taraftaki ops
# dongusu ".sh" uzantisini soyup /usr/local/bin'e attigi icin ESLESMIYORDU
# -> backend port degistirme her taze kurulumda "bash helper yok" ile oluydu.
if [ -f "$A/ops/girginospanel-port-swap.sh" ]; then
  install -D -m 0700 "$A/ops/girginospanel-port-swap.sh" \
    /usr/local/sbin/girginospanel-port-swap.sh \
    && ok "port-swap helper (/usr/local/sbin)" \
    || die "port-swap helper kurulamadi"
  # Yanlis yere dusmus kopyayi temizle (karisiklik olmasin)
  rm -f /usr/local/bin/girginospanel-port-swap 2>/dev/null
else
  warn "port-swap helper pakette YOK — backend port degistirme calismaz"
fi


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
nginx -t >/dev/null 2>&1 && # 🔴 /var/cache/nginx UST DIZIN IZNI: RPM bunu 0700 root:root birakiyor.
# Alt dizin (girgincache) dogru sahiplenmis olsa bile nginx worker UST dizini
# TRAVERSE edemedigi icin cache loader sunu veriyor:
#   [crit] opendir() "/var/cache/nginx/girgincache" failed (13: Permission denied)
# Belirti sinsi: nginx AYAKTA kalir, yalnizca fastcgi_cache sessizce olur.
if [ -d /var/cache/nginx ]; then
  chmod 0755 /var/cache/nginx; chown root:root /var/cache/nginx
fi
ok "nginx -t OK" || { nginx -t; die "nginx config hatası"; }

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
# pma internal-auth token (pma-signon.php + panel API aynı dosyayı okur → rastgele değer eşleşir).
# Yoksa üret (root:apache 0640 → pma FPM pool [apache] okur, başkası okuyamaz). Var olana dokunma.
if [ ! -s /etc/girginospanel/pma-internal.token ]; then
  openssl rand -hex 32 > /etc/girginospanel/pma-internal.token
  chown root:apache /etc/girginospanel/pma-internal.token 2>/dev/null || true
  chmod 640 /etc/girginospanel/pma-internal.token
fi
cp "$A/php-fpm/phpmyadmin.conf" /etc/php-fpm.d/phpmyadmin.conf
mkdir -p /var/lib/phpmyadmin/{tmp,sessions}
# ── PMA IZINLERI ────────────────────────────────────────────────────
# 🔴 Eskiden hepsi `nginx:nginx` yapiliyordu — ama phpMyAdmin FPM havuzu
# (assets/php-fpm/phpmyadmin.conf) `user = apache` olarak calisiyor.
# PMA yalnizca dosyalar 644/755 (dunyaya okunur) oldugu icin calisiyordu.
# Sonuc: `config.inc.php` 0644 ve icinde `blowfish_secret` + kontrol
# kullanicisi PAROLASI var -> sunucudaki HERHANGI bir yerel kullanici
# (tenant dahil) okuyabiliyordu. `sessions` dizini de 0755 idi, yani
# baskasinin PMA oturum dosyalari okunabiliyordu.
#
# Dogru model: PHP'yi apache calistirir, nginx yalnizca statik dosyalari
# okur. Bu yuzden /opt/phpmyadmin DIZINI 755 kalir (nginx statik okusun),
# ama config.inc.php root:apache 640 olur (nginx PHP kaynagini okumaz,
# FPM'e devreder). /var/lib/phpmyadmin'e nginx'in hic isi yoktur.
chown -R root:apache /opt/phpmyadmin 2>/dev/null
chmod 755 /opt/phpmyadmin 2>/dev/null
[ -f /opt/phpmyadmin/config.inc.php ] && chmod 640 /opt/phpmyadmin/config.inc.php
chown -R apache:apache /var/lib/phpmyadmin 2>/dev/null
chmod 750 /var/lib/phpmyadmin 2>/dev/null
chmod 700 /var/lib/phpmyadmin/tmp /var/lib/phpmyadmin/sessions 2>/dev/null
# SELinux etiketleri KALICI kural olarak yazilir. restorecon tek basina
# yetmez: kural yoksa bir sonraki `restorecon -R /` etiketi geri alir ve
# SELinux Enforcing'e alinirsa PMA + panel 403 doner.
if command -v semanage >/dev/null 2>&1; then
  semanage fcontext -a -t httpd_sys_content_t '/opt/phpmyadmin(/.*)?' 2>/dev/null
  semanage fcontext -a -t httpd_sys_rw_content_t '/var/lib/phpmyadmin(/.*)?' 2>/dev/null
  semanage fcontext -a -t httpd_sys_content_t '/opt/girginospanel/frontend-dist(/.*)?' 2>/dev/null
  semanage fcontext -a -t httpd_sys_content_t '/opt/girginospanel/pma-signon(/.*)?' 2>/dev/null
fi
restorecon -R /opt/phpmyadmin /var/lib/phpmyadmin /opt/girginospanel/frontend-dist /opt/girginospanel/pma-signon >/dev/null 2>&1
# Dogrulama: sir tasiyan dosya dunyaya okunur KALMAMALI.
if [ -f /opt/phpmyadmin/config.inc.php ]; then
  _m=$(stat -c%a /opt/phpmyadmin/config.inc.php)
  case "$_m" in
    *[2367]) echo "  UYARI: config.inc.php izni $_m — dünyaya okunur, sır sızıyor" ;;
  esac
fi
setsebool -P httpd_can_network_connect_db 1 >/dev/null 2>&1
ok "phpMyAdmin pool + config + izinler"

# ============ 10) systemd + servisler ============
step "10) systemd + servisler"
cp "$A/systemd/girginospanel.service" /etc/systemd/system/girginospanel.service
# journald kalici: panel crash/db-fatal izleri reboot'ta silinmesin (reboot-dayaniklilik teshisi)
mkdir -p /var/log/journal && systemctl restart systemd-journald >/dev/null 2>&1 || true
# panel DB'sinin günlük yedeği (03:30) — dosyayı kopyalamak YETMEZ, aşağıda enable --now
# edilir; aksi halde timer hiç ateşlenmez ve kurulum sessizce YEDEKSİZ kalırdı.
for u in girginospanel-db-backup.service girginospanel-db-backup.timer; do
  [ -f "$A/systemd/$u" ] && cp "$A/systemd/$u" "/etc/systemd/system/$u"
done
systemctl daemon-reload
if [ -f /etc/systemd/system/girginospanel-db-backup.timer ]; then
  systemctl enable --now girginospanel-db-backup.timer >/dev/null 2>&1
  systemctl is-active --quiet girginospanel-db-backup.timer \
    && ok "günlük panel DB yedeği ACTIVE (03:30 → /var/backups/girginospanel/db, 14 gün)" \
    || warn "DB yedek timer'ı başlatılamadı — günlük panel DB yedeği çalışmayabilir"
fi
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

# ---- Node.js (Laravel Toolkit: npm/vite build + çoklu sürüm 'n') ----
if ! command -v node >/dev/null 2>&1; then
  curl -fsSL https://rpm.nodesource.com/setup_22.x 2>/dev/null | bash - >/dev/null 2>&1 || true
  dnf install -y nodejs >/dev/null 2>&1 || true
fi
if command -v npm >/dev/null 2>&1 && [ ! -d /usr/local/n/versions/node ]; then
  npm install -g n >/dev/null 2>&1 || true
  N_PREFIX=/usr/local n 20 >/dev/null 2>&1 || true
  N_PREFIX=/usr/local n 22 >/dev/null 2>&1 || true
fi
if [ -d /usr/local/n/versions/node ]; then ok "node ($(ls /usr/local/n/versions/node 2>/dev/null | tr '\n' ' '))"; else warn "node kurulamadı (Laravel npm özellikleri devre dışı)"; fi

# ---- günlük yedek cron (girginospanel-backup-all 03:00 UTC) ----
cat > /etc/cron.d/girginospanel-backup <<'CRON'
# girginospanel — günlük planlı yedek 03:00 UTC
SHELL=/bin/bash
PATH=/usr/local/bin:/usr/bin:/bin
0 3 * * * root /usr/local/bin/girginospanel-backup-all
CRON
# crond'u ŞİMDİ başlat + enable et (AlmaLinux preset yalnız enable eder, reboot'a kadar
# başlatmaz → yedek cron'u ilk reboot'a kadar çalışmazdı). enable --now idempotent.
systemctl enable --now crond >/dev/null 2>&1
systemctl is-active --quiet crond && ok "günlük yedek cron + crond ACTIVE (03:00 UTC)" || warn "crond başlatılamadı — yedek cron çalışmayabilir"

# SELinux
setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1 && ok "SELinux httpd_can_network_connect"
# Batch5A: nginx(httpd_t) tenant home içeriğini (public_html) okuyabilsin — bu boolean'lar
# KAPALI iken try_files dosyayı "yok" sanar → tüm siteler 404. (Panel açılışında
# ensureHTTPDHomeBooleans ile de garanti edilir; bu satır ilk-boot için.)
setsebool -P httpd_enable_homedirs=on httpd_read_user_content=on >/dev/null 2>&1 && ok "SELinux httpd home okuma (homedirs + user_content)"
restorecon -R /opt/girginospanel/bin /opt/girginospanel/frontend-dist >/dev/null 2>&1
# Batch5A: per-tenant php-fpm socket dizinleri /run/php-fpm-<sk>/ için fcontext (httpd_var_run_t).
# Mevcut /run/php-fpm(/.*)? kuralı tireli yolu kapsamaz → nginx→FPM 500. Idempotent.
# (Panel açılışında da ensureFPMSELinuxFcontext ile garanti edilir; bu satır ilk boot öncesi içindir.)
if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce)" != "Disabled" ] && command -v semanage >/dev/null 2>&1; then
  semanage fcontext -l 2>/dev/null | grep -q "/run/php-fpm-\[" || \
    semanage fcontext -a -t httpd_var_run_t "/run/php-fpm-[^/]+(/.*)?" 2>/dev/null || true
  ok "SELinux fcontext: per-tenant php-fpm socket (httpd_var_run_t)"
fi

# ============ 11) Valkey + optimize ============
step "11) Valkey (Redis) + performans tuning"
if [ ! -x "/usr/local/bin/girginospanel-redis-setup" ]; then
  die "girginospanel-redis-setup pakette YOK — paketleme hatasi"
elif ! command -v girginospanel-redis-setup >/dev/null 2>&1; then
  die "girginospanel-redis-setup kurulu ama PATH ten cagrilamiyor (PATH=$PATH)"
elif girginospanel-redis-setup >/dev/null 2>&1; then
  ok "girginospanel-redis-setup"
else
  warn "girginospanel-redis-setup calisti ama HATA dondu — sonra elle: girginospanel-redis-setup"
fi
if [ ! -x "/usr/local/bin/girginospanel-optimize" ]; then
  die "girginospanel-optimize pakette YOK — paketleme hatasi"
elif ! command -v girginospanel-optimize >/dev/null 2>&1; then
  die "girginospanel-optimize kurulu ama PATH ten cagrilamiyor (PATH=$PATH)"
elif girginospanel-optimize >/dev/null 2>&1; then
  ok "girginospanel-optimize"
else
  warn "girginospanel-optimize calisti ama HATA dondu — sonra elle: girginospanel-optimize"
fi

# ============ 12) Panel başlat (migration startup'ta koşar) ============
step "12) Panel başlatılıyor"
# 🔴 BAYAT SUREC: `enable --now` ZATEN CALISAN servisi YENIDEN BASLATMAZ.
# Adim 6'da binary'yi DEGISTIRDIK; restart etmezsek ESKI surec calismaya
# devam eder. O zaman yeni binary diskte durur ama devrede olmaz ve
# provisioner'in her acilista garanti ettigi seyler (catch-all belge
# kokleri, cache zone, WAF senkronu) YENIDEN OLUSTURULMAZ. Operator
# "ACTIVE" gorup yeni surumun devrede oldugunu saniyor.
systemctl enable girginospanel >/dev/null 2>&1
systemctl restart girginospanel >/dev/null 2>&1; sleep 3
systemctl enable --now nginx >/dev/null 2>&1; systemctl restart nginx >/dev/null 2>&1
if ! systemctl is-active --quiet girginospanel; then
  journalctl -u girginospanel --no-pager -n 20; die "panel başlamadı"
fi
# "ayakta" YETMEZ — CALISAN SURECIN kurdugumuz binary olduğunu kanitla.
# Binary degistirilip restart edilmediginde /proc/PID/exe ESKI inode'u
# (cogu zaman "(deleted)") gosterir. Servis yine "active" der.
_pid=$(systemctl show -p MainPID --value girginospanel 2>/dev/null)
_kurulu=/opt/girginospanel/bin/girginospanel-server
if [ -n "$_pid" ] && [ "$_pid" != "0" ] && [ -r "/proc/$_pid/exe" ]; then
  # ICERIK karsilastirmasi — inode DEGIL. /proc/PID/exe procfs aygitinda
  # gorunur, kurulu dosya gercek fs'te; inode'lar asla esitlenmez ve kontrol
  # her zaman "bayat" derdi (ilk surumde bu hata yapildi).
  _link=$(readlink "/proc/$_pid/exe" 2>/dev/null)
  _ch=$(sha256sum "/proc/$_pid/exe" 2>/dev/null | awk '{print $1}')
  _kh=$(sha256sum "$_kurulu" 2>/dev/null | awk '{print $1}')
  case "$_link" in
    *" (deleted)") die "panel ayakta ama calisan dosya SILINMIS — restart basarisiz" ;;
  esac
  if [ -z "$_ch" ] || [ -z "$_kh" ]; then
    warn "girginospanel ACTIVE ama calisan binary DOGRULANAMADI (sha256 alinamadi)"
  elif [ "$_ch" = "$_kh" ]; then
    ok "girginospanel ACTIVE (calisan surec = kurulan binary)"
  else
    die "panel ayakta ama ESKI binary ile calisiyor (${_link:-bilinmiyor}) — restart basarisiz"
  fi
else
  # Olculemiyorsa GECTI sayma.
  warn "girginospanel ACTIVE ama calisan binary DOGRULANAMADI (MainPID=$_pid)"
fi

# ---- FTP setup (Pure-FTPd) — ŞİMDİ çalışır: migration ftp_accounts tablosunu oluşturdu ----
# (step 11'de değil çünkü GRANT SELECT ON panel.ftp_accounts tablo yokken patlıyordu)
sleep 2
if [ ! -x "/usr/local/bin/girginospanel-ftp-setup" ]; then
  die "girginospanel-ftp-setup pakette YOK — paketleme hatasi"
elif ! command -v girginospanel-ftp-setup >/dev/null 2>&1; then
  die "girginospanel-ftp-setup kurulu ama PATH ten cagrilamiyor (PATH=$PATH)"
elif girginospanel-ftp-setup >/dev/null 2>&1; then
  ok "girginospanel-ftp-setup (Pure-FTPd, MySQL backend)"
else
  warn "girginospanel-ftp-setup calisti ama HATA dondu — sonra elle: girginospanel-ftp-setup"
fi

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

# ============ 13b) Antivirüs imzaları + WAF (opt-in) ============
# 🔴 SIRA ONEMLI: bu bloklar eskiden 'kurulum tamamlandi' bannerinin ALTINDAYDI.
# Oradaki `warn` satirlari basari mesajinin ardinda kaliyor ve operator temiz
# kurulum sandigi icin fark etmiyordu. Artik banner'dan ONCE kosuyorlar ve
# adim 15 dogrulamasi bunlarin sonucunu da olcuyor.
# === FRESHCLAM: imza veritabani ===
# 🔴 Bu adim OLMADAN antivirus PLACEBO: clamscan imza dizini bosken
# "No supported database files found" der ama EXIT 0 doner; panel bunu
# "temiz" olarak raporlar. Enfekte site temiz gorunur.
mkdir -p /var/lib/clamav && chown clamupdate:clamupdate /var/lib/clamav 2>/dev/null || true
sed -i 's/^Example/#Example/' /etc/freshclam.conf 2>/dev/null || true
freshclam --quiet >/dev/null 2>&1 || freshclam >/dev/null 2>&1 || true
if ls /var/lib/clamav/*.c[vl]d >/dev/null 2>&1; then
  ok "clamav imza veritabani indirildi ($(ls /var/lib/clamav/*.c[vl]d | wc -l) dosya)"
else
  warn "clamav imza veritabani indirilemedi — antivirus taramasi imzasiz calisir"
fi
systemctl enable --now clamav-freshclam >/dev/null 2>&1 \
  && ok "clamav-freshclam (otomatik imza guncelleme) aktif" \
  || warn "clamav-freshclam servisi baslatilamadi"

# ============ WAF (opt-in) ============
if [ "$KUR_WAF" = "1" ]; then
  echo
  echo "== ModSecurity + OWASP CRS kuruluyor (kaynaktan derleme, birkac dakika) =="
  if girginospanel-waf-setup; then
    ok "WAF kuruldu — panelde domain bazinda acilabilir"
  else
    warn "WAF kurulumu basarisiz — panel calismaya devam eder, WAF kapali"
  fi
else
  echo
  echo "  NOT: WAF (ModSecurity) KURULMADI."
  echo "       Paneldeki WAF anahtari, modul kurulmadan etkisizdir."
  echo "       Kurmak icin:  girginospanel-waf-setup"
fi

# ============ 14) İzin onarımı ============
step "14) İzin/SELinux onarımı"
if [ ! -x /usr/local/bin/girginospanel-repair ]; then
  die "girginospanel-repair pakette YOK — paketleme hatasi"
elif ! command -v girginospanel-repair >/dev/null 2>&1; then
  die "girginospanel-repair PATH ten cagrilamiyor (PATH=$PATH)"
elif girginospanel-repair --quiet >/dev/null 2>&1; then
  ok "girginospanel-repair"
else
  warn "girginospanel-repair hata dondu — elle: girginospanel-repair"
fi

# ============ 15) DOĞRULAMA ============
step "15) Doğrulama"
IP=$(hostname -I 2>/dev/null | awk '{print $1}')
CODE=$(curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:8443/ 2>/dev/null)
API=$(curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:8443/api/v1/domains 2>/dev/null)
echo -e "  servisler: $(systemctl is-active mariadb nginx valkey php-fpm named pure-ftpd girginospanel crond | tr '\n' ' ')"
echo -e "  panel :8443 → HTTP $CODE   ·   API (auth) → HTTP $API   ·   DNS :53 → $(systemctl is-active named)   ·   FTP :21 → $(systemctl is-active pure-ftpd)"
echo -e "  araçlar: SSL/acme.sh $([ -x /root/.acme.sh/acme.sh ] && echo ✓ || echo ✗)   ·   firewall/nft $(command -v nft >/dev/null && echo ✓ || echo ✗)   ·   unzip/zip $(command -v unzip >/dev/null && command -v zip >/dev/null && echo ✓ || echo ✗)   ·   composer $(command -v composer >/dev/null && echo ✓ || echo ✗)   ·   apache/httpd $(systemctl is-active httpd)"
# Bugun eklenen guvenlik bilesenleri BASARI BANNERINDAN once ozetlenir:
# eksik biri varsa operator "kurulum tamamlandi" yazisini gormeden fark etsin.
AV_N=$(ls /var/lib/clamav/*.c[vl]d 2>/dev/null | wc -l)
if [ "$AV_N" -gt 0 ]; then AV_D="OK ($AV_N imza)"; else AV_D="YOK - tarama imzasiz calisir"; fi
if [ "$KUR_WAF" = "1" ]; then WAF_D="kuruldu"; else WAF_D="kurulmadi (girginospanel-waf-setup)"; fi
JAIL_D=X; [ -e /opt/girginospanel/src/scripts/50-gosp-jail.conf ] && [ -e /opt/girginospanel/src/scripts/girginospanel-jail ] && JAIL_D=OK
PSW_D=X; [ -x /usr/local/sbin/girginospanel-port-swap.sh ] && PSW_D=OK
echo -e "  antivirüs: $AV_D   ·   freshclam $(systemctl is-active clamav-freshclam 2>/dev/null)   ·   WAF: $WAF_D"
echo -e "  ssh izolasyon(jail): $JAIL_D   ·   backend port helper: $PSW_D"
echo -e "  wp-cli (WordPress): ${WPCLI_DURUM:-?}"
echo -e "  izolasyon: plan-driven kaynak limitleri (cgroup slice) + per-tenant PHP-FPM (CageFS eşdeğeri) HAZIR   ·   bubblewrap $(command -v bwrap >/dev/null && echo ✓ || echo ✗)"

# ══════════════════════════════════════════════════════════════════════
# 🔴 GERCEK DOGRULAMA KAPISI
# Yukaridaki ozet satirlari BILGILENDIRICIDIR, kapi degildir. Eskiden kurulum
# "wp-cli (WordPress): YOK" yazip hemen ardindan "✓ kurulum tamamlandi"
# basiyordu; ayni kosuda SELinux etiketi yok, valkey kapali, FTP kurulmamis ve
# port 80 her Host icin HTTP 500 doner haldeydi. Operator "tamamlandi" gordu.
#
# girginospanel-dogrula URETIM DAVRANISINI olcer (HTTP kodlari, gercek komut
# calistirma, uretim kullanicisiyla dosya erisimi) ve kritik bir sey duserse
# 1 doner. Bu durumda basari banneri BASILMAZ.
# ══════════════════════════════════════════════════════════════════════
DOGRULAMA_KODU=0
if command -v girginospanel-dogrula >/dev/null 2>&1; then
  echo
  girginospanel-dogrula || DOGRULAMA_KODU=$?
else
  warn "girginospanel-dogrula bulunamadi — kurulum DOGRULANAMADI"
  DOGRULAMA_KODU=1
fi

if [ "$DOGRULAMA_KODU" -ne 0 ]; then
  echo
  echo -e "${c_r}═══════════════════════════════════════════════${c_0}"
  echo -e "${c_r} ✗ Kurulum TAMAMLANMADI — kritik dogrulama basarisiz${c_0}"
  echo -e "   Yukaridaki ${c_r}✗${c_0} satirlarini duzeltin, sonra:"
  echo -e "     ${c_b}girginospanel-dogrula${c_0}          (yeniden olc)"
  echo -e "     ${c_b}girginospanel-repair${c_0}           (bilinen sorunlari onar)"
  echo -e "   Kurulumu bastan calistirmak guvenlidir (idempotent)."
  echo -e "${c_r}═══════════════════════════════════════════════${c_0}"
  exit 1
fi

echo
echo -e "${c_g}═══════════════════════════════════════════════${c_0}"
echo -e "${c_g} ✓ GirginOSPanel kurulumu tamamlandı${c_0}"
echo -e "   Panel:  ${c_b}https://${IP:-SUNUCU_IP}:8443${c_0}"
echo -e "   Kullanıcı: ${c_b}root${c_0}   Parola: ${c_b}bu sunucunun root parolası${c_0}"
echo -e "   (panel admin girişi sunucu root parolasını /etc/shadow hash'i ile doğrular)"
if [ "$(findmnt -no FSTYPE / 2>/dev/null)" = "xfs" ] && ! findmnt -no OPTIONS / 2>/dev/null | grep -qwE 'usrquota|uquota|quota'; then
  echo -e "   ${c_y}Disk kotası: GRUB'a rootflags=uquota yazıldı — TEK SEFERLİK reboot sonrası aktif olur.${c_0}"
fi
echo -e "${c_g}═══════════════════════════════════════════════${c_0}"

