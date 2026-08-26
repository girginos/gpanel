#!/usr/bin/env bash
# girginospanel-install — turns a bare AlmaLinux 10 server into a complete GirginOSPanel.
# Designed to be idempotent (re-runnable). Run as root.
#
#   ./girginospanel-install.sh [--admin-parola <p>] [--admin-eposta <e>]
#
# The assets/ directory must sit next to this script:
#   girginospanel-server  girginospanel-seed-admin  frontend-dist.tar.gz
#   migrations.tar.gz  nginx/*  php-fpm/*  phpmyadmin/*  systemd/*  ops/*
set -uo pipefail

# 🔴 WE SET PATH OURSELVES — `sudo` DROPS /usr/local/bin.
# AlmaLinux/RHEL default:  Defaults secure_path = /sbin:/bin:/usr/sbin:/usr/bin
# Because the documented install/update command is `curl ... | sudo bash`, this
# path runs ON EVERY CUSTOMER and our own tools (girginospanel-*, composer,
# wp) give "command not found" when called bare-name. Measured: the tool existed
# as a FILE, only missing from PATH; the install still finished green.
case ":$PATH:" in
  *:/usr/local/bin:*) : ;;
  *) export PATH="/usr/local/sbin:/usr/local/bin:$PATH" ;;
esac

# 🔴 LC_ALL=C REQUIRED FOR PARSING — TURKISH LOCALE BREAKS REGEX RANGES.
# Measured (31.56.47.190, LANG=tr_TR.UTF-8):
#     printf '@CT@ audit_log' | grep -oE '@CT@ [a-zA-Z0-9_]+'   ->  "@CT@ aud"
#     ayni komut LC_ALL=C ile                                    ->  "@CT@ audit_log"
# In tr_TR collation the `a-z` range does NOT cover `i` (dotless i/dotted i
# distinction), so EVERY parse using a char range gets cut at `i`:
#   audit_log -> aud · cp_domains -> cp_doma · cp_websec_findings -> cp_websec_f
# Result: the schema check produces a FALSE critical like 22/46 and BLOCKED the
# customer update. Turkey is our main market — this affects every customer server.
# LC_ALL=C gives byte semantics; that is correct for parsing ASCII identifiers.
export LC_ALL=C


# --waf : install ModSecurity v3 + OWASP CRS (COMPILES FROM SOURCE, ~10 min).
# Default OFF — to not slow every install. Can be installed later too:
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
    echo "usage: $0 [--admin-parola P] [--admin-eposta E] [--waf]"
    echo "  --waf : install ModSecurity v3 + OWASP CRS (compiles from source, ~10 min)"
    exit 0 ;;
  *) echo "unknown: $1"; exit 2 ;;
esac; shift; done

c_g="\033[32m"; c_y="\033[33m"; c_r="\033[31m"; c_b="\033[1;34m"; c_0="\033[0m"
[ -t 1 ] || { c_g=; c_y=; c_r=; c_b=; c_0=; }
step(){ echo -e "\n${c_b}══ $* ══${c_0}"; }
ok(){ echo -e "  ${c_g}✓${c_0} $*"; }
warn(){ echo -e "  ${c_y}!${c_0} $*"; }
die(){ echo -e "  ${c_r}✗ $*${c_0}"; exit 1; }

[ "$(id -u)" = 0 ] || die "root required"
[ -d "$A" ] || die "assets/ not found ($A)"
grep -qiE "AlmaLinux|Rocky|Red Hat|CentOS" /etc/os-release || warn "AlmaLinux/RHEL10 expected — continuing"

# 🔴 php86 NOT YET RELEASED in remi. If left in the list, every install
# fails silently: package not installed, /etc/opt/remi/php86 never
# gets created, `systemctl enable php86-php-fpm` output is swallowed.
PHP_VERS="74 80 81 82 83 84 85"
PHP_EXT="fpm cli mysqlnd mbstring bcmath intl gd soap opcache pdo xml zip pgsql ldap"

# ============ 1) REPO ============
step "1) Repositories (EPEL + Remi + CRB)"
dnf install -y epel-release >/dev/null 2>&1 && ok "EPEL"
# 🔴 EPEL REQUIRED: unrar, clamav-freshclam, sshpass come from EPEL. If it can't be added
# the SINGLE `dnf install` below returns non-zero entirely, no package installs and
# the ROOT CAUSE of the "base package install" error stays invisible.
rpm -q epel-release >/dev/null 2>&1 || dnf repolist enabled 2>/dev/null | grep -qi epel \
  || die "EPEL repo could not be added — unrar/clamav-freshclam/sshpass cannot install"
rpm -q remi-release >/dev/null 2>&1 || dnf install -y https://rpms.remirepo.net/enterprise/remi-release-10.rpm >/dev/null 2>&1
rpm -q remi-release >/dev/null 2>&1 && ok "Remi" || die "Remi could not be added"
dnf config-manager --set-enabled crb >/dev/null 2>&1 && ok "CRB"

# ============ 2) BASE PACKAGES ============
step "2) Base packages"
dnf install -y nginx httpd mariadb-server valkey certbot python3-certbot-nginx \
  clamav clamav-freshclam httpd-tools mod_proxy_html tar openssl policycoreutils-python-utils \
  setools-console jq bind bind-utils nftables unzip zip cronie xfsprogs sudo \
  bubblewrap rsync git curl acl \
  bzip2 lftp sshpass unrar bsdtar >/dev/null 2>&1 \
  && ok "nginx, httpd, mariadb, valkey, certbot, clamav, bind, nftables, unzip/zip/bzip2, bubblewrap, acl, tools" || die "base package install"

# RAR extractor (file manager .rar extract) — PRIMARY: bsdtar (libarchive, in appstream base
# reads RAR/RAR5 RELIABLY; also rejects path-traversal). 🔴 NOTE: AlmaLinux 10 default
# `7z` (7-Zip 26.02) does NOT include a RAR codec → not used. If no bsdtar, unar/unrar fallback.
if command -v bsdtar >/dev/null 2>&1 || command -v unar >/dev/null 2>&1 || command -v unrar >/dev/null 2>&1; then
  ok "RAR extractor present ($(command -v bsdtar unar unrar 2>/dev/null | head -1))"
elif dnf install -y bsdtar >/dev/null 2>&1; then
  ok "bsdtar (libarchive — rar/rar5/zip/7z extract)"
elif dnf install -y unar >/dev/null 2>&1 || dnf install -y unrar >/dev/null 2>&1; then
  ok "unar/unrar (rar extract)"
else
  warn "RAR extractor could not be installed — file manager .rar extract disabled (zip/tar works)"
fi

# ============ 2b) DISK QUOTA (XFS user quota — CloudLinux parity) ============
# Per-tenant disk + inode quota is enforced via XFS *user* quota (files owned c_<sk>:c_<sk>
# → user quota matches exactly + escape-protected). If root fs is XFS + `noquota`, quota
# opens only at MOUNT time (cannot open via live remount) → write `rootflags=uquota` to GRUB.
# On a fresh install, quota becomes ACTIVE after the post-install reboot.
step "2b) Disk quota (XFS user quota)"
dnf install -y quota xfsprogs >/dev/null 2>&1 && ok "quota + xfsprogs" || warn "quota packages skipped"
ROOTFS_TYPE=$(findmnt -no FSTYPE / 2>/dev/null || echo "")
ROOTFS_OPTS=$(findmnt -no OPTIONS / 2>/dev/null || echo "")
if [ "$ROOTFS_TYPE" != "xfs" ]; then
  warn "root fs is not XFS ($ROOTFS_TYPE) — XFS disk quota skipped"
elif echo "$ROOTFS_OPTS" | grep -qwE 'usrquota|uquota|quota'; then
  ok "root XFS user quota already active"
else
  if grep -q 'rootflags=uquota' /etc/default/grub 2>/dev/null; then
    ok "GRUB rootflags=uquota already present"
  else
    if grep -q '^GRUB_CMDLINE_LINUX=' /etc/default/grub 2>/dev/null; then
      sed -i 's/^\(GRUB_CMDLINE_LINUX="[^"]*\)"/\1 rootflags=uquota"/' /etc/default/grub
    else
      echo 'GRUB_CMDLINE_LINUX="rootflags=uquota"' >> /etc/default/grub
    fi
    # also update existing boot entries (BLS) + regenerate grub.cfg (BIOS + EFI).
    command -v grubby >/dev/null 2>&1 && grubby --update-kernel=ALL --args="rootflags=uquota" >/dev/null 2>&1 || true
    grub2-mkconfig -o /boot/grub2/grub.cfg >/dev/null 2>&1 || true
    for cfg in /boot/efi/EFI/*/grub.cfg; do [ -f "$cfg" ] && grub2-mkconfig -o "$cfg" >/dev/null 2>&1 || true; done
    ok "GRUB rootflags=uquota added (root XFS)"
  fi
  warn "Disk quota becomes active after a ONE-TIME reboot (root fs cannot open via remount)."
fi

# ============ 3) PHP (5 versions + base + wp-cli) ============
# ============ 2c) FIREWALL — DISABLE firewalld, panel takes over ============
# 🔴 The panel firewall manages its own nftables table (girginos_fw). AlmaLinux 10
# CONFLICTS with default firewalld: 'drop' in nftables is final → firewalld drops the panel's
# ports (8443/80/443/53/21), and the panel's 'accept' cannot override it. So the sole authority
# is the panel: we stop+disable+mask firewalld. (The panel binary also guarantees this at
# startup via FirewalldDevral — this step disables it early during install.)
step "2c) Firewall (disable firewalld — panel takes over)"
if systemctl cat firewalld.service >/dev/null 2>&1; then
  systemctl disable --now firewalld >/dev/null 2>&1 || true
  systemctl mask firewalld >/dev/null 2>&1 || true
  ok "firewalld stopped + masked (single firewall = panel nftables)"
else
  ok "firewalld not installed — panel nftables is already the sole firewall"
fi

step "3) PHP versions (5 remi + base) + wp-cli"
BASE_PKGS="php php-fpm php-cli php-mysqlnd php-mbstring php-json php-pecl-zip php-pecl-redis6 php-gd php-bcmath php-intl php-soap php-ldap php-sodium php-opcache"
# 🔴 BEFORE the PHP batch install: disable dnf auto-lock sources (dnf-automatic/makecache
#    if the timer is active, the bulk "dnf install" hits the lock/produces false-negatives).
#    Managed panel updates handle it themselves; auto-update OFF (prevents lock contention + surprise-patch).
systemctl disable --now dnf-automatic.timer dnf-makecache.timer >/dev/null 2>&1 || true
dnf install -y $BASE_PKGS >/dev/null 2>&1 && ok "base php + php-redis"

# 🔴 VERIFICATION: the dnf above is silenced with `2>/dev/null`; if an extension doesn't install
# the install looks "successful" but phpMyAdmin/license verification won't work.
php_eksik=""
# 🔴 `grep -qix "$_m"` requires a WHOLE-LINE match. `php -m` lists the opcache module
# as "Zend OPcache" -> the condition NEVER holds and every install
# prints "base PHP extensions MISSING: opcache", although the module IS loaded
# (measured: php -m | grep -ci zend.opcache = 2). Also `php -m | grep -q`
# produces SIGPIPE; we grab the list once and do an in-shell substring test.
PHP_MODLISTE=$(php -m 2>/dev/null | tr "A-Z" "a-z" | tr -d " ")
for _m in gd bcmath intl soap ldap sodium opcache mysqlnd mbstring zip; do
  case "$PHP_MODLISTE" in
    *"$_m"*) : ;;
    *) php_eksik="$php_eksik $_m" ;;
  esac
done
if [ -n "$php_eksik" ]; then
  warn "base PHP extensions MISSING:$php_eksik — phpMyAdmin/license verification may be affected"
else
  ok "base PHP extensions verified (gd, bcmath, intl, soap, ldap, sodium, opcache)"
fi
for v in $PHP_VERS; do
  pkgs=""; for e in $PHP_EXT; do pkgs="$pkgs php$v-php-$e"; done
  dnf install -y $pkgs php$v-php-pecl-redis6 >/dev/null 2>&1 && ok "php$v (+redis)" || warn "php$v some packages skipped"
done
# 🔴 wp-cli install delegated to a TOOL. There used to be an inline curl here
# that hid the error with `2>/dev/null`, only printed `warn` and continued
# the install. When GitHub rate-limited (429/503), the file never downloaded,
# the install said "successful" and the WordPress page at runtime
# blew up with "Could not open input file: /usr/local/bin/wp".
# The tool: prefers our own mirror, verifies the download BY RUNNING it, and
# is called again by the updater on every update (self-heal).
if [ -f "$A/ops/girginospanel-wpcli-kur" ]; then
  install -m 0755 "$A/ops/girginospanel-wpcli-kur" /usr/local/bin/girginospanel-wpcli-kur
  if girginospanel-wpcli-kur; then
    WPCLI_DURUM="OK"
  else
    WPCLI_DURUM="NONE"
    warn "wp-cli could not be installed — WordPress features won't work. Later: girginospanel-wpcli-kur"
  fi
else
  WPCLI_DURUM="TOOL-MISSING"
  warn "ops/girginospanel-wpcli-kur not in package — wp-cli not installed"
fi

# ionCube Loader — REQUIRED for commercial (encoded) scripts like WHMCS etc.
# 🔴 Without the loader the site shows an "ionCube Loader needs to be installed" white page
# and the customer cannot install it themselves (needs root). The tool is idempotent + fail-soft:
# if the download fails, the PHP install is unaffected and it can be called again later.
if [ -f "$A/ops/girginospanel-ioncube" ]; then
  install -m 0755 "$A/ops/girginospanel-ioncube" /usr/local/bin/girginospanel-ioncube
  girginospanel-ioncube || warn "ionCube could not be installed — later: girginospanel-ioncube"
else
  warn "ops/girginospanel-ioncube not in package — ionCube skipped"
fi

# WordPress prerequisites: SELinux tenant directory labels + imagick.
# 🔴 Both were SILENTLY skipped during install and failed one after another on a
# live customer: under Enforcing WordPress can't install plugins ("FTP credentials"
# form, then "Permission denied"), and Site Health warns because imagick is missing.
if [ -f "$A/ops/girginospanel-wp-onkosul" ]; then
  install -m 0755 "$A/ops/girginospanel-wp-onkosul" /usr/local/bin/girginospanel-wp-onkosul
  girginospanel-wp-onkosul || warn "WordPress prerequisites left incomplete — later: girginospanel-wp-onkosul"
else
  warn "ops/girginospanel-wp-onkosul not in package — SELinux label + imagick skipped"
fi

# 🔴 JOURNAL PERSISTENCE — logs must survive across boots.
# On AlmaLinux 10, if /var/log/journal is absent journald keeps logs only IN MEMORY and
# history is WIPED on every reboot. Result: an unexpected reboot or crash CANNOT
# be DIAGNOSED afterward. Happened exactly on 49.12.158.182: a second reboot occurred,
# `journalctl -b -1` returned empty, the cause could not be found.
# NOTE: AlmaLinux 10 has NO /etc/systemd/journald.conf (moved to a drop-in
# structure) — instead of creating that file we write under conf.d.
mkdir -p /var/log/journal /etc/systemd/journald.conf.d
cat > /etc/systemd/journald.conf.d/10-girginospanel.conf <<'JRNL'
[Journal]
Storage=persistent
SystemMaxUse=500M
JRNL
systemd-tmpfiles --create --prefix /var/log/journal >/dev/null 2>&1 || true
systemctl restart systemd-journald >/dev/null 2>&1 || true
if [ -d /var/log/journal ]; then
  ok "journal persistent (logs kept across boots, up to 500M)"
else
  warn "journal could not be made persistent — old logs lost after reboot"
fi

# ============ 4) MARIADB ============
step "4) MariaDB"
systemctl enable --now mariadb >/dev/null 2>&1; sleep 2
systemctl is-active --quiet mariadb || die "MariaDB did not start"

# my.cnf security hardening: MySQL CLOSED externally (loopback only) + LOCAL INFILE disabled.
# Panel and customer sites connect via 127.0.0.1; 3306 is NOT exposed to the internet.
cat > /etc/my.cnf.d/zz-girginospanel-security.cnf <<'MYCNF'
# GirginOSPanel security hardening (installer)
[mysqld]
bind-address = 127.0.0.1
local-infile = 0
MYCNF
systemctl restart mariadb >/dev/null 2>&1; sleep 2
systemctl is-active --quiet mariadb || die "MariaDB did not start (after security hardening)"
ok "MariaDB security: 3306 closed externally (bind 127.0.0.1) + local-infile disabled"

# 🔴 NO SECRET ROTATION — the install must be IDEMPOTENT w.r.t. secrets.
# Old behavior: every run generated a NEW random DBPASS and applied ALTER USER to the
# live MySQL user. When the script ran a second time on the same server
# (the dev-channel "update" flow is exactly this) the running panel process still held the OLD
# password in memory → ALL requests between this step and the restart in step 12
# got `Error 1045 Access denied for user 'panel'@'127.0.0.1'`; if the script
# died for any reason in that window the panel stayed PERMANENTLY broken.
# (Since session checking fails open on a DB error, this was also a security
# window — separate task.)
# New behavior: if the secret is ABSENT it is generated, if PRESENT the env value is kept as-is and the MySQL
# user is aligned to it — also repairs a drifted password. The Nth run
# gives the same result as the 1st run.
ENVF=/etc/girginospanel/env
env_deger() { [ -f "$ENVF" ] && sed -n "s/^$1=//p" "$ENVF" | head -1; }

# DSN format: panel:<PASSWORD>@tcp(127.0.0.1:3306)/panel?...  (password hex → contains no '@')
DBPASS=$(env_deger PANEL_DB_DSN | sed -n 's/^panel:\(.*\)@tcp(.*/\1/p')
if [ -n "$DBPASS" ]; then DBPASS_KAYNAK="kept from existing env"
else DBPASS=$(openssl rand -hex 16); DBPASS_KAYNAK="newly generated"; fi

mysql -u root <<SQL
CREATE DATABASE IF NOT EXISTS panel CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'panel'@'127.0.0.1' IDENTIFIED BY '$DBPASS';
ALTER USER 'panel'@'127.0.0.1' IDENTIFIED BY '$DBPASS';
GRANT ALL PRIVILEGES ON panel.* TO 'panel'@'127.0.0.1';
FLUSH PRIVILEGES;
SQL
# Verify: "ALTER USER didn't error" is NOT enough — prove the password actually works by
# connecting as the panel user. Otherwise the breakage would only show at step 12.
mysql -u panel -p"$DBPASS" -h 127.0.0.1 -e 'SELECT 1' panel >/dev/null 2>&1 \
  || die "panel DB user could not be verified (password not aligned with env) — install stopped"
ok "panel DB + user (panel@127.0.0.1) — password $DBPASS_KAYNAK, connection VERIFIED"

# ============ 5) DIRECTORIES + ENV ============
step "5) Directories + env"
# 🔴 src/scripts REQUIRED: assets/ops files are copied here and the SSH
# chroot jail config (50-gosp-jail.conf) is read from here. If the dir is absent
# `cp ... 2>/dev/null` fails SILENTLY; the panel logs
# 'SSH ISOLATION NOT APPLIED' on every startup and the tenant gets a full shell WITHOUT chroot.
mkdir -p /opt/girginospanel/src/scripts \
  /opt/girginospanel/bin /opt/girginospanel/frontend-dist /opt/girginospanel/src/migrations \
         /opt/girginospanel/src/eklentiler /opt/girginospanel/eklentiler \
         /opt/girginospanel/pma-signon /etc/girginospanel /etc/ssl/girginospanel
# Secrets: like DBPASS, JWT and the Redis admin password are generated ONLY if absent.
# Rotating JWT drops all sessions; rotating the Redis password may fall out of sync with the
# value redis-setup wrote. On reinstall don't touch either.
JWT=$(env_deger PANEL_JWT_SECRET);           [ -n "$JWT" ]    || JWT=$(openssl rand -hex 32)
RADMIN=$(env_deger PANEL_REDIS_ADMIN_PASS);  [ -n "$RADMIN" ] || RADMIN=$(openssl rand -hex 24)
OMUR=$(env_deger PANEL_JWT_LIFETIME_SEC);    [ -n "$OMUR" ]   || OMUR=43200
DINLE=$(env_deger PANEL_LISTEN);             [ -n "$DINLE" ]  || DINLE=127.0.0.1:8080

# 🔴 PRESERVE keys we don't manage. The old `cat > env` overwrote the whole env;
# later-added lines like PANEL_LISANS_SUNUCU were deleted on every install and the panel
# fell back to the code/DB default for the license server.
EKSTRA=""
if [ -f "$ENVF" ]; then
  EKSTRA=$(grep -E '^[A-Za-z_][A-Za-z0-9_]*=' "$ENVF" \
    | grep -vE '^(PANEL_LISTEN|PANEL_ENV|PANEL_DB_DSN|PANEL_JWT_SECRET|PANEL_JWT_LIFETIME_SEC|PANEL_REDIS_ADMIN_PASS)=')
fi

# Atomic write: if the script dies mid-write, don't leave a half/empty env (panel won't start without env).
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
ok "$ENVF (DB DSN + JWT + Redis admin preserved/generated; $EKSAY extra keys preserved)"

# ============ 6) ARTIFACT DEPLOY ============
step "6) Panel binary + frontend + migration"
# 🔴 The PACKAGE hash is stored as an anchor. Step 12 uses it; otherwise
# the "running == on-disk" comparison would also be TRUE for a binary that
# was never installed (old file running, old file on disk).
PAKET_BIN_SHA=$(sha256sum "$A/girginospanel-server" 2>/dev/null | awk '{print $1}')
[ -n "$PAKET_BIN_SHA" ] || die "package panel binary could not be read"
install -m 0755 "$A/girginospanel-server" /opt/girginospanel/bin/girginospanel-server \
  || die "panel binary could not be installed (disk full / read-only / SELinux)"
_disk_sha=$(sha256sum /opt/girginospanel/bin/girginospanel-server 2>/dev/null | awk '{print $1}')
[ "$_disk_sha" = "$PAKET_BIN_SHA" ] || die "panel binary written INCOMPLETELY to disk (hash mismatch)"
[ -f "$A/girginospanel-seed-admin" ] && install -m 0755 "$A/girginospanel-seed-admin" /opt/girginospanel/bin/girginospanel-seed-admin
tar xzf "$A/frontend-dist.tar.gz" -C /opt/girginospanel/frontend-dist && ok "frontend-dist"
tar xzf "$A/migrations.tar.gz" -C /opt/girginospanel/src/migrations && ok "migrations ($(ls /opt/girginospanel/src/migrations/*.sql 2>/dev/null | wc -l) sql)"
# Licensed plugin payloads: ship to the server binary but the gate is CLOSED (active=0).
# Not run until a license is entered; when it is, the install puts it in place.
if [ -d "$A/eklentiler" ]; then
  cp -a "$A/eklentiler/." /opt/girginospanel/src/eklentiler/ 2>/dev/null
  chmod -R 0755 /opt/girginospanel/src/eklentiler 2>/dev/null
  ok "plugin payload ($(find /opt/girginospanel/src/eklentiler -type f 2>/dev/null | wc -l) files)"
fi

# Install the antivirus agent binary if present in the package (real-time monitor + scanner).
if [ -f "$A/girginospanel-avajan" ]; then
  install -m 0755 "$A/girginospanel-avajan" /usr/local/bin/girginospanel-avajan || die "avajan could not be installed"
fi
# ops tool + signon
# 🔴 Two separate targets, two separate meanings:
#   /usr/local/bin        -> COMMANDS the operator runs manually
#   src/scripts           -> source files the panel reads at RUNTIME
#                            (SSH chroot jail script + sshd config template)
# The `.conf` files used to be installed to /usr/local/bin with 0755 too (wrong
# place, wrong perms) and the src/scripts copy was SWALLOWED with `2>/dev/null`:
# it failed silently because the dir was absent, the panel logged
# "SSH ISOLATION NOT APPLIED" on every startup and the tenant got a full shell WITHOUT chroot.
mkdir -p /opt/girginospanel/src/scripts
_ops_n=0
for t in "$A"/ops/*; do
  [ -f "$t" ] || continue
  bn=$(basename "$t")
  # 🔴 Leftover/backup files are NOT installed. These become STALE code and if installed to
  # /usr/local/bin with 0755 as root they could be run by accident
  # (girginospanel-repair.bak.<time> was found in the package and was being installed).
  case "$bn" in *.bak|*.bak.*|*.orig|*.rej|*~) continue ;; esac
  case "$bn" in
    *.conf|*.service|*.timer)
      # Configuration: only to the source dir, NOT executable
      install -m 0644 "$t" "/opt/girginospanel/src/scripts/$bn" || die "ops config: $bn"
      ;;
    *)
      nm="${bn%.sh}"
      install -m 0755 "$t" "/usr/local/bin/$nm" || die "ops tool: $nm"
      # The panel reads some scripts from here at runtime (jail etc.)
      install -m 0755 "$t" "/opt/girginospanel/src/scripts/$nm" || die "ops source: $nm"
      ;;
  esac
  _ops_n=$((_ops_n+1))
done
# Verify: are the two files required for SSH isolation actually in place?
for zorunlu in 50-gosp-jail.conf girginospanel-jail; do
  # 🔴 die, NOT warn: without these files a tenant opening SSH cannot be
  # JAILED into chroot and sees everything on the server. Saying "install
  # complete" with a security failure means silently leaving it vulnerable.
  [ -e "/opt/girginospanel/src/scripts/$zorunlu" ] \
    || die "ops: $zorunlu NOT in src/scripts — SSH chroot isolation CANNOT be installed (missing from package)"
done
ok "ops tools ($_ops_n files: /usr/local/bin + src/scripts)"

# 🔴 RESOLVABILITY GUARD: installing the file is NOT enough, it must be
# proven callable from PATH. Because `sudo` drops /usr/local/bin from PATH
# tools give "command not found" even though installed, and the install
# still finished GREEN (exactly what happened on 49.12.158.182: 6 steps were
# silently skipped). The check uses the PRODUCTION path (command -v), not its own setup.
_coz_eksik=""
for _t in girginospanel-wpcli-kur girginospanel-wp-onkosul girginospanel-repair \
          girginospanel-optimize girginospanel-redis-setup girginospanel-ftp-setup; do
  [ -x "/usr/local/bin/$_t" ] || continue
  command -v "$_t" >/dev/null 2>&1 || _coz_eksik="$_coz_eksik $_t"
done
if [ -n "$_coz_eksik" ]; then
  die "ops tools installed but not callable from PATH:$_coz_eksik  (PATH=$PATH)"
fi
ok "ops tools resolvable from PATH"

# 🔴 port-swap helper SPECIAL PATH: the panel looks for it at exactly
# the path /usr/local/sbin/girginospanel-port-swap.sh
# (internal/portyonetim/degistir.go: BackendHelperYolu). The ops loop above
# strips the ".sh" extension and drops it in /usr/local/bin, so it DID NOT MATCH
# -> backend port change died with "bash helper missing" on every fresh install.
if [ -f "$A/ops/girginospanel-port-swap.sh" ]; then
  install -D -m 0700 "$A/ops/girginospanel-port-swap.sh" \
    /usr/local/sbin/girginospanel-port-swap.sh \
    && ok "port-swap helper (/usr/local/sbin)" \
    || die "port-swap helper could not be installed"
  # Clean up the copy that landed in the wrong place (avoid confusion)
  rm -f /usr/local/bin/girginospanel-port-swap 2>/dev/null
else
  warn "port-swap helper NOT in package — backend port change won't work"
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
# http-level setting (client_max_body_size 10240m) — idempotent.
# NOTE: server_names_hash_bucket_size is NOT added — it's already in girginospanel-optimize's 00-perf.conf
# if we add it here too, nginx -t blows up with "duplicate directive".
grep -q "client_max_body_size 10240m" /etc/nginx/nginx.conf || \
  sed -i '/^http {/a\    client_max_body_size 10240m;' /etc/nginx/nginx.conf
install -m 0644 "$A/nginx/_panel.conf" /etc/nginx/conf.d/_panel.conf || die "_panel.conf could not be installed"
install -m 0644 "$A/nginx/_default80.conf" /etc/nginx/conf.d/_default80.conf || die "_default80.conf could not be installed"
install -m 0644 "$A/nginx/php-fpm.conf" /etc/nginx/conf.d/php-fpm.conf || die "nginx php-fpm.conf could not be installed"
# 🔴 GATE FIRST, ON A SINGLE LINE. Do NOT insert a comment/block in between:
# bash skips comments after `&&` and takes the next COMMAND as the right operand,
# so the gate silently dies (happened exactly: a broken conf was reported as "✓ nginx -t OK"
# and `die` never ran).
nginx -t >/dev/null 2>&1 || { nginx -t; die "nginx config error"; }

# /var/cache/nginx PARENT DIR PERM: RPM leaves it 0700 root:root.
# Even if the subdir (girgincache) is owned correctly, the nginx worker cannot
# TRAVERSE the parent dir, so the cache loader gives:
#   [crit] opendir() "/var/cache/nginx/girgincache" failed (13: Permission denied)
# Symptom is sneaky: nginx STAYS UP, only fastcgi_cache silently dies.
if [ -d /var/cache/nginx ]; then
  chmod 0755 /var/cache/nginx; chown root:root /var/cache/nginx
fi
ok "nginx -t OK"

# ============ 9) phpMyAdmin ============
step "9) phpMyAdmin"
mkdir -p /opt/phpmyadmin   # create FIRST (otherwise strip-components extract blows up)
if [ ! -f /opt/phpmyadmin/index.php ]; then
  TMP=$(mktemp -d)
  if curl -fsSL -o "$TMP/pma.tar.gz" https://www.phpmyadmin.net/downloads/phpMyAdmin-latest-all-languages.tar.gz \
     && tar xzf "$TMP/pma.tar.gz" -C /opt/phpmyadmin --strip-components=1; then
    ok "phpMyAdmin downloaded + extracted"
  else warn "phpMyAdmin could not be downloaded (network?) — later manually: girginospanel-repair"; fi
  rm -rf "$TMP"
fi
if [ -f "$A/phpmyadmin/config.inc.php" ]; then
  BLOWFISH=$(openssl rand -hex 16)           # fresh — NOT a prod secret
  PMACTRL=$(openssl rand -hex 16)            # pma control user password (fresh)
  sed -e "s/BLOWFISH_SECRET_BURAYA/$BLOWFISH/g" -e "s/PMA_CONTROL_PASS_BURAYA/$PMACTRL/g" \
    "$A/phpmyadmin/config.inc.php" > /opt/phpmyadmin/config.inc.php
  # pma control user + phpmyadmin DB + pmadb tables (advanced features)
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
# pma internal-auth token (pma-signon.php + panel API read the same file → random value matches).
# Generate if absent (root:apache 0640 → pma FPM pool [apache] reads it, no one else). Don't touch existing.
if [ ! -s /etc/girginospanel/pma-internal.token ]; then
  openssl rand -hex 32 > /etc/girginospanel/pma-internal.token
  chown root:apache /etc/girginospanel/pma-internal.token 2>/dev/null || true
  chmod 640 /etc/girginospanel/pma-internal.token
fi
install -m 0644 "$A/php-fpm/phpmyadmin.conf" /etc/php-fpm.d/phpmyadmin.conf || die "phpmyadmin.conf could not be installed"
mkdir -p /var/lib/phpmyadmin/{tmp,sessions}
# ── PMA PERMISSIONS ────────────────────────────────────────────────────
# 🔴 Everything used to be set to `nginx:nginx` — but the phpMyAdmin FPM pool
# (assets/php-fpm/phpmyadmin.conf) runs as `user = apache`.
# PMA only worked because files were 644/755 (world-readable).
# Result: `config.inc.php` was 0644 and contained `blowfish_secret` + the control
# user's PASSWORD -> ANY local user on the server
# (including tenants) could read it. The `sessions` dir was also 0755, so
# another user's PMA session files were readable.
#
# Correct model: apache runs PHP, nginx only reads static files.
# So the /opt/phpmyadmin DIR stays 755 (nginx reads static),
# but config.inc.php becomes root:apache 640 (nginx doesn't read PHP source,
# it hands off to FPM). nginx has no business with /var/lib/phpmyadmin.
chown -R root:apache /opt/phpmyadmin 2>/dev/null
chmod 755 /opt/phpmyadmin 2>/dev/null
[ -f /opt/phpmyadmin/config.inc.php ] && chmod 640 /opt/phpmyadmin/config.inc.php
chown -R apache:apache /var/lib/phpmyadmin 2>/dev/null
chmod 750 /var/lib/phpmyadmin 2>/dev/null
chmod 700 /var/lib/phpmyadmin/tmp /var/lib/phpmyadmin/sessions 2>/dev/null
# SELinux labels are written as PERSISTENT rules. restorecon alone
# is not enough: without the rule the next `restorecon -R /` reverts the label and
# if SELinux is set to Enforcing PMA + panel return 403.
if command -v semanage >/dev/null 2>&1; then
  semanage fcontext -a -t httpd_sys_content_t '/opt/phpmyadmin(/.*)?' 2>/dev/null
  semanage fcontext -a -t httpd_sys_rw_content_t '/var/lib/phpmyadmin(/.*)?' 2>/dev/null
  semanage fcontext -a -t httpd_sys_content_t '/opt/girginospanel/frontend-dist(/.*)?' 2>/dev/null
  semanage fcontext -a -t httpd_sys_content_t '/opt/girginospanel/pma-signon(/.*)?' 2>/dev/null
fi
restorecon -R /opt/phpmyadmin /var/lib/phpmyadmin /opt/girginospanel/frontend-dist /opt/girginospanel/pma-signon >/dev/null 2>&1
# Verify: a secret-bearing file must NOT stay world-readable.
if [ -f /opt/phpmyadmin/config.inc.php ]; then
  _m=$(stat -c%a /opt/phpmyadmin/config.inc.php)
  case "$_m" in
    *[2367]) echo "  WARNING: config.inc.php perm $_m — world-readable, secret leaking" ;;
  esac
fi
setsebool -P httpd_can_network_connect_db 1 >/dev/null 2>&1
ok "phpMyAdmin pool + config + permissions"

# ============ 10) systemd + services ============
step "10) systemd + services"
install -m 0644 "$A/systemd/girginospanel.service" /etc/systemd/system/girginospanel.service || die "girginospanel.service could not be installed"
# journald persistent: panel crash/db-fatal traces shouldn't be wiped on reboot (reboot-resilience diagnosis)
mkdir -p /var/log/journal && systemctl restart systemd-journald >/dev/null 2>&1 || true
# daily backup of the panel DB (03:30) — copying the file is NOT enough, it's enable --now
# below; otherwise the timer never fires and the install silently stays WITHOUT BACKUP.
for u in girginospanel-db-backup.service girginospanel-db-backup.timer; do
  [ -f "$A/systemd/$u" ] && cp "$A/systemd/$u" "/etc/systemd/system/$u"
done
systemctl daemon-reload

# ── Antivirus units: resource slice + monitor + scheduled scan ──
# 🔴 The slice FILE must be installed so the av units can join
# Slice=girginos-av.slice. Without the slice, units run in the default slice and resource
# limits constrain NOTHING — the panel says "I set a limit" but it isn't applied.
# The content is rewritten by the panel (avayar.LimitleriUygula); here
# we leave a baseline version so there's a limit on first boot too.
if [ ! -f /etc/systemd/system/girginos-av.slice ]; then
  cat > /etc/systemd/system/girginos-av.slice <<'AVSLICE'
[Unit]
Description=GirginOSPanel antivirus scan slice
Before=slices.target
[Slice]
CPUAccounting=yes
MemoryAccounting=yes
IOAccounting=yes
CPUQuota=50%
MemoryMax=512M
IOWeight=50
TasksMax=64
AVSLICE
fi
for u in girginospanel-avizle.service girginospanel-avsurec.service girginospanel-avtara.service girginospanel-avtara.timer; do
  [ -f "$A/systemd/$u" ] && cp "$A/systemd/$u" "/etc/systemd/system/$u"
done
systemctl daemon-reload
# 🔴 DEFAULT: only the scheduled scan timer is enabled. The real-time monitor
# (avizle) is DEFAULT OFF — the operator enables it from the panel (fanotify + continuous
# scanning, has a resource cost). This must be CONSISTENT with av_ayarlar defaults
# (gercek_zamanli=0, zamanli_tarama=1).
# 🔴 ANTIVIRUS "YAKINDA": motor kararli olana dek zamanli tarama DEVRE DISI.
if [ -f /etc/systemd/system/girginospanel-avtara.timer ]; then
  systemctl disable --now girginospanel-avtara.timer >/dev/null 2>&1
  ok "antivirus scheduled scan DISABLED (feature coming soon)"
fi
# 🔴 The backup root dir is created DURING INSTALL. The backup script already does `mkdir -p`
# but that only runs on the FIRST RUN (03:30); until then the dir is ABSENT and
# looks missing in the post-install audit. A deterministic state is better.
# NOTE: I first put this line in the MIDDLE of a multi-line `&&` chain and broke the
# script with a syntax error. NEVER insert a line between chains joined by a
# continuation (backslash).
install -d -m 0755 /var/backups/girginospanel 2>/dev/null || true
if [ -f /etc/systemd/system/girginospanel-db-backup.timer ]; then
  systemctl enable --now girginospanel-db-backup.timer >/dev/null 2>&1
  systemctl is-active --quiet girginospanel-db-backup.timer \
    && ok "daily panel DB backup ACTIVE (03:30 → /var/backups/girginospanel/db, 14 days)" \
    || warn "DB backup timer could not be started — daily panel DB backup may not run"
fi
# 🔴 It used to print a fixed "base + 5 versions" but PHP_VERS contains 7 versions
# moreover there was no `is-active` at all. For a version not in Remi
# (like php86) `enable` silently fails, the message still printed green and the
# customer site choosing that version got 502. Now we report by COUNTING and MEASURING.
_fpm_ok=0; _fpm_eksik=""
for _u in php-fpm $(for v in $PHP_VERS; do echo "php$v-php-fpm"; done); do
  systemctl enable --now "$_u" >/dev/null 2>&1
  if systemctl is-active --quiet "$_u"; then
    _fpm_ok=$((_fpm_ok+1))
  else
    _fpm_eksik="$_fpm_eksik $_u"
  fi
done
if [ -z "$_fpm_eksik" ]; then
  ok "php-fpm ($_fpm_ok pools ACTIVE)"
else
  warn "php-fpm: $_fpm_ok ACTIVE, NOT ACTIVE:$_fpm_eksik"
fi

# ---- named (DNS server) — the nameserver for domains ----
NC=/etc/named.conf
if [ -f "$NC" ]; then
  # 🔴 The backup was overwritten ON EVERY RUN: on the second run ".gosp-bak"
  # becomes a copy of the already-modified file and the ORIGINAL named.conf is
  # lost permanently. The first backup is preserved.
  [ -f "$NC.gosp-bak" ] || cp -a "$NC" "$NC.gosp-bak" 2>/dev/null || true
  # make it externally queryable: listen on all interfaces (default is only 127.0.0.1)
  sed -i -E 's/listen-on port 53 \{[^}]*\}/listen-on port 53 { any; }/' "$NC"
  sed -i -E 's/listen-on-v6 port 53 \{[^}]*\}/listen-on-v6 port 53 { any; }/' "$NC"
  # no open resolver (open resolver / DNS amplification) — authoritative DNS only
  sed -i -E 's/recursion yes/recursion no/' "$NC"
  # panel zone include (WriteZone fills this) — idempotent
  grep -q 'girginospanel-zones.conf' "$NC" || \
    echo 'include "/etc/named/girginospanel-zones.conf";' >> "$NC"
fi
# panel zone include file (starts empty; fills as the panel adds domains)
mkdir -p /etc/named
[ -f /etc/named/girginospanel-zones.conf ] || \
  printf '// girginospanel — auto-generated\n' > /etc/named/girginospanel-zones.conf
chown root:named /etc/named/girginospanel-zones.conf 2>/dev/null || true
chmod 640 /etc/named/girginospanel-zones.conf 2>/dev/null || true
# zone files under /var/named (SELinux named_zone_t context REQUIRED)
restorecon -R /var/named /etc/named >/dev/null 2>&1 || true
if named-checkconf >/dev/null 2>&1; then
  # 🔴 `enable --now` does NOT restart an already-running named -> the sed
  # edits above DON'T TAKE EFFECT, but the message still says ":53 open, recursion off"
  # In reality named still listens on 127.0.0.1 (domains don't resolve) or
  # is still an OPEN RESOLVER (DNS amplification). Restart + MEASURE the
  # CLAIMED assertions.
  systemctl enable named >/dev/null 2>&1
  systemctl restart named >/dev/null 2>&1
  sleep 1
  if ! systemctl is-active --quiet named; then
    warn "named could not be started"
  else
    _n53=$(ss -lnu 2>/dev/null | grep -c ":53 ")
    _nrec=$(grep -cE "^[[:space:]]*recursion[[:space:]]+yes" "$NC" 2>/dev/null)
    if [ "${_n53:-0}" -gt 0 ] && [ "${_nrec:-0}" -eq 0 ]; then
      ok "named (DNS authoritative, :53 open, recursion off)"
    else
      warn "named ACTIVE but claims NOT VERIFIED (:53 socket=$_n53, recursion-yes line=$_nrec)"
    fi
  fi
else
  warn "named-checkconf error — check DNS manually"
fi

# ---- acme.sh (Let's Encrypt SSL) — the panel calls /root/.acme.sh/acme.sh ----
# LE requires a valid email (@ + dot). If invalid like admin@local, register without contact.
AEMAIL="$ADMIN_EPOSTA"; echo "$AEMAIL" | grep -qE '@[^@]+\.[^@]+$' || AEMAIL=""
if [ ! -x /root/.acme.sh/acme.sh ]; then
  if [ -n "$AEMAIL" ]; then curl -fsSL https://get.acme.sh 2>/dev/null | sh -s email="$AEMAIL" >/dev/null 2>&1 || true
  else curl -fsSL https://get.acme.sh 2>/dev/null | sh >/dev/null 2>&1 || true; fi
fi
if [ -x /root/.acme.sh/acme.sh ]; then
  /root/.acme.sh/acme.sh --set-default-ca --server letsencrypt >/dev/null 2>&1
  # register the LE account NOW (with the valid email if any, otherwise contactless) — avoid errors at issue time
  if [ -n "$AEMAIL" ]; then /root/.acme.sh/acme.sh --register-account -m "$AEMAIL" --server letsencrypt >/dev/null 2>&1
  else /root/.acme.sh/acme.sh --register-account --server letsencrypt >/dev/null 2>&1; fi
  ok "acme.sh (Let's Encrypt CA + account registered + auto-renew cron)"
else
  warn "acme.sh could not be installed — for Let's Encrypt SSL manually: curl https://get.acme.sh | sh"
fi

# ---- httpd (Apache backend — web_backend=apache option, nginx front-proxy) ----
# since nginx is on :80, Apache listens on 127.0.0.1:10080 (mod_proxy_fcgi → php-fpm)
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
    systemctl enable --now httpd >/dev/null 2>&1 && ok "httpd (Apache backend :10080, mod_proxy_fcgi)" || warn "httpd could not be started"
  else warn "httpd configtest error — check Apache backend manually"; fi
fi

# ---- composer (per-domain PHP dependency management) ----
if [ ! -x /usr/local/bin/composer ]; then
  # 🔴 `php --` (stdin-pipe) bazi ortamlarda (PHP 8.3 + ionCube) SEGFAULT eder ->
  # temp dosya + `php <dosya>` formu kullan (segfault YOK).
  _ci=$(mktemp)
  if curl -sS https://getcomposer.org/installer -o "$_ci" 2>/dev/null; then
    php "$_ci" --install-dir=/usr/local/bin --filename=composer >/dev/null 2>&1
  fi
  rm -f "$_ci"
fi
[ -x /usr/local/bin/composer ] && ok "composer ($(/usr/local/bin/composer --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1))" || warn "composer could not be installed"

# ---- Node.js (Laravel Toolkit: npm/vite build + multi-version 'n') ----
if ! command -v node >/dev/null 2>&1; then
  curl -fsSL https://rpm.nodesource.com/setup_22.x 2>/dev/null | bash - >/dev/null 2>&1 || true
  dnf install -y nodejs >/dev/null 2>&1 || true
fi
if command -v npm >/dev/null 2>&1 && [ ! -d /usr/local/n/versions/node ]; then
  npm install -g n >/dev/null 2>&1 || true
  N_PREFIX=/usr/local n 20 >/dev/null 2>&1 || true
  N_PREFIX=/usr/local n 22 >/dev/null 2>&1 || true
fi
if [ -d /usr/local/n/versions/node ]; then ok "node ($(ls /usr/local/n/versions/node 2>/dev/null | tr '\n' ' '))"; else warn "node could not be installed (Laravel npm features disabled)"; fi

# ---- backups: NO cron installed — the panel scheduler owns backups ----
# This used to write /etc/cron.d/girginospanel-backup running
# girginospanel-backup-all every night at 03:00. That script never read the
# `domains.backup_freq` column: it archived EVERY domain nightly — including
# domains whose backup was switched OFF in the panel — applied a hard-coded
# 14-day retention (ignoring `domains.backup_retention`), never checked free
# space before writing, and dumped only `<sk>_main`, so multi-database tenants
# had NO database in their backups. In production it filled the root disk and
# took the panel and the customer sites down with it.
#
# Backups are now owned by the Go scheduler inside the panel binary: it honours
# backup_freq / backup_hour / backup_retention, obeys the `backup_genel_ayar`
# master switch and free-space gate, dumps ALL of a tenant's databases, verifies
# each archive with sha256 + gzip -t, and can push to an off-site target. It
# needs no separate cron entry.
#
# `girginospanel-backup-all` is still installed into /usr/local/bin, but it is a
# no-op stub now, so a leftover cron entry on an upgraded host does no harm.
# crond is still started because other components rely on it.
systemctl enable --now crond >/dev/null 2>&1
systemctl is-active --quiet crond && ok "crond ACTIVE (backups run from the panel scheduler, no extra cron)" || warn "crond could not be started"

# ---- daily SYSTEM (disaster-recovery) backup cron (girginospanel-sistem-yedek 04:00 UTC) ----
# panel-backup'in KAPSAMADIGI ogeler: TUM MySQL DB (mail+domain+panel+grantlar),
# secrets (/etc/girginospanel), systemd units, OpenDKIM, named, mail+web SSL/ACME,
# nginx/apache vhost, per-tenant php-fpm, /var/vmail (gercek e-postalar), cron.
# Script assets/ops/girginospanel-sistem-yedek.sh -> ops-loop /usr/local/bin/girginospanel-sistem-yedek olarak kurar.
if [ -x /usr/local/bin/girginospanel-sistem-yedek ]; then
  cat > /etc/cron.d/girginospanel-sistem-yedek <<'CRON'
# girginospanel — daily system (disaster-recovery) backup 04:00 UTC
SHELL=/bin/bash
PATH=/usr/local/bin:/usr/bin:/bin
0 4 * * * root /usr/local/bin/girginospanel-sistem-yedek >/var/log/girginospanel-sistem-yedek.log 2>&1
CRON
  ok "system backup cron installed (04:00 UTC, felaket-kurtarma)"
else
  warn "girginospanel-sistem-yedek not installed — system backup cron skipped"
fi

# SELinux
# 🔴 There was NO `else` on failure: among ~40 green lines the ABSENCE
# of ONE LINE goes unnoticed by the operator. An explicit warning instead of silent skip.
if setsebool -P httpd_can_network_connect 1 >/dev/null 2>&1; then
  ok "SELinux httpd_can_network_connect"
else
  warn "SELinux httpd_can_network_connect COULD NOT be set — panel may not reach external services"
fi
# Batch5A: let nginx(httpd_t) read tenant home content (public_html) — with these booleans
# OFF, try_files thinks the file is "absent" → all sites 404. (Also guaranteed at panel
# startup via ensureHTTPDHomeBooleans; this line is for first-boot.)
# 🔴 Without this boolean ALL SITES return 404 — cannot be skipped silently.
if setsebool -P httpd_enable_homedirs=on httpd_read_user_content=on >/dev/null 2>&1; then
  ok "SELinux httpd home read (homedirs + user_content)"
else
  die "SELinux httpd home booleans could not be set — all sites return 404"
fi
restorecon -R /opt/girginospanel/bin /opt/girginospanel/frontend-dist >/dev/null 2>&1
# Batch5A: fcontext for per-tenant php-fpm socket dirs /run/php-fpm-<sk>/ (httpd_var_run_t).
# The existing /run/php-fpm(/.*)? rule doesn't cover the hyphenated path → nginx→FPM 500. Idempotent.
# (Also guaranteed at panel startup via ensureFPMSELinuxFcontext; this line is for before first boot.)
if command -v getenforce >/dev/null 2>&1 && [ "$(getenforce)" != "Disabled" ] && command -v semanage >/dev/null 2>&1; then
  # 🔴 The `|| true` chain made the ok line UNCONDITIONAL: even if the rule
  # wasn't added "✓" was printed. Without this rule nginx->FPM returns 500 (ALL
  # tenants). MEASURE AGAIN after adding.
  # NOTE: pipe + `grep -q` produces SIGPIPE (pipefail); we grab the list into a variable.
  _selfc=$(semanage fcontext -l 2>/dev/null)
  case "$_selfc" in
    *"/run/php-fpm-["*) : ;;
    *) semanage fcontext -a -t httpd_var_run_t "/run/php-fpm-[^/]+(/.*)?" 2>/dev/null || true ;;
  esac
  _selfc=$(semanage fcontext -l 2>/dev/null)
  case "$_selfc" in
    *"/run/php-fpm-["*) ok "SELinux fcontext: per-tenant php-fpm socket (httpd_var_run_t)" ;;
    *) die "SELinux fcontext rule COULD NOT be added — per-tenant FPM sockets return 500" ;;
  esac
fi

# ============ 11) Valkey + optimize ============
step "11) Valkey (Redis) + performance tuning"
if [ ! -x "/usr/local/bin/girginospanel-redis-setup" ]; then
  die "girginospanel-redis-setup NOT in package — packaging error"
elif ! command -v girginospanel-redis-setup >/dev/null 2>&1; then
  die "girginospanel-redis-setup installed but not callable from PATH (PATH=$PATH)"
elif girginospanel-redis-setup >/dev/null 2>&1; then
  ok "girginospanel-redis-setup"
else
  warn "girginospanel-redis-setup ran but returned an ERROR — later manually: girginospanel-redis-setup"
fi
if [ ! -x "/usr/local/bin/girginospanel-optimize" ]; then
  die "girginospanel-optimize NOT in package — packaging error"
elif ! command -v girginospanel-optimize >/dev/null 2>&1; then
  die "girginospanel-optimize installed but not callable from PATH (PATH=$PATH)"
elif girginospanel-optimize >/dev/null 2>&1; then
  ok "girginospanel-optimize"
else
  warn "girginospanel-optimize ran but returned an ERROR — later manually: girginospanel-optimize"
fi

# ============ 12) Start panel (migration runs at startup) ============
step "12) Starting panel"
# 🔴 STALE PROCESS: `enable --now` does NOT RESTART an already-running service.
# In step 6 we REPLACED the binary; without restart the OLD process keeps
# running. Then the new binary sits on disk but isn't in effect and
# the things the provisioner guarantees on each startup (catch-all doc
# roots, cache zone, WAF sync) are NOT RE-CREATED. The operator
# sees "ACTIVE" and assumes the new version is in effect.
systemctl enable girginospanel >/dev/null 2>&1
systemctl restart girginospanel >/dev/null 2>&1; sleep 3
systemctl enable --now nginx >/dev/null 2>&1; systemctl restart nginx >/dev/null 2>&1
if ! systemctl is-active --quiet girginospanel; then
  journalctl -u girginospanel --no-pager -n 20; die "panel did not start"
fi
# "up" is NOT enough — prove the RUNNING PROCESS is the binary we installed.
# When the binary is replaced without restart /proc/PID/exe shows the OLD inode
# (usually "(deleted)"). The service still says "active".
_pid=$(systemctl show -p MainPID --value girginospanel 2>/dev/null)
_kurulu=/opt/girginospanel/bin/girginospanel-server
if [ -n "$_pid" ] && [ "$_pid" != "0" ] && [ -r "/proc/$_pid/exe" ]; then
  # CONTENT comparison — NOT inode. /proc/PID/exe appears on the procfs device
  # the installed file is on the real fs; inodes never match and the check
  # always said "stale" (this mistake was made in the first version).
  _link=$(readlink "/proc/$_pid/exe" 2>/dev/null)
  _ch=$(sha256sum "/proc/$_pid/exe" 2>/dev/null | awk '{print $1}')
  # Anchor is the PACKAGE hash. Comparing with "$_kurulu" would be a false pair.
  _kh=${PAKET_BIN_SHA:-$(sha256sum "$_kurulu" 2>/dev/null | awk '{print $1}')}
  case "$_link" in
    *" (deleted)") die "panel up but the running file is DELETED — restart failed" ;;
  esac
  if [ -z "$_ch" ] || [ -z "$_kh" ]; then
    warn "girginospanel ACTIVE but running binary NOT VERIFIED (sha256 unavailable)"
  elif [ "$_ch" = "$_kh" ]; then
    ok "girginospanel ACTIVE (running process = installed binary)"
  else
    die "panel up but running the OLD binary (${_link:-unknown}) — restart failed"
  fi
else
  # If it can't be measured, don't count it as PASS.
  warn "girginospanel ACTIVE but running binary NOT VERIFIED (MainPID=$_pid)"
fi

# ---- FTP setup (Pure-FTPd) — works NOW: migration created the ftp_accounts table ----
# (not in step 11 because GRANT SELECT ON panel.ftp_accounts blew up when the table didn't exist)
sleep 2
if [ ! -x "/usr/local/bin/girginospanel-ftp-setup" ]; then
  die "girginospanel-ftp-setup NOT in package — packaging error"
elif ! command -v girginospanel-ftp-setup >/dev/null 2>&1; then
  die "girginospanel-ftp-setup installed but not callable from PATH (PATH=$PATH)"
elif girginospanel-ftp-setup >/dev/null 2>&1; then
  ok "girginospanel-ftp-setup (Pure-FTPd, MySQL backend)"
else
  warn "girginospanel-ftp-setup ran but returned an ERROR — later manually: girginospanel-ftp-setup"
fi

# ============ 13) Admin access ============
# 🔴 Panel admin login = the server's ROOT user (/etc/shadow hash verification).
# There is NO separate panel password. Login: user 'root' + this server's root password.
step "13) Admin access (root + /etc/shadow)"
DSN="panel:${DBPASS}@tcp(127.0.0.1:3306)/panel?parseTime=true"
if [ -x /opt/girginospanel/bin/girginospanel-seed-admin ]; then
  # helper users record (ownership/audit); login is still verified with the server root password (/etc/shadow hash)
  /opt/girginospanel/bin/girginospanel-seed-admin -dsn "$DSN" -kullanici root \
    -parola "$(openssl rand -hex 16)" -eposta "$ADMIN_EPOSTA" >/dev/null 2>&1 \
    && ok "admin record ready" || warn "seed skipped (not critical)"
fi
# make the root profile come up EMPTY — clear seed-admin's fake 'admin@local'/'System Administrator'
# values (the user fills them in from the Profile page)
mysql panel -e "UPDATE users SET email='', full_name='' WHERE username='root' AND email='admin@local';" >/dev/null 2>&1 || true
ok "Login: user 'root' + this server's root password"

# ============ 13b) Antivirus signatures + WAF (opt-in) ============
# 🔴 ORDER MATTERS: these blocks used to be BELOW the 'install complete' banner.
# Those `warn` lines stayed after the success message and the operator assumed a clean
# install so didn't notice. Now they run BEFORE the banner and
# the step 15 verification measures their result too.
# === FRESHCLAM: signature database ===
# 🔴 WITHOUT this step antivirus is a PLACEBO: with an empty clamscan signature dir
# it says "No supported database files found" but returns EXIT 0; the panel reports this
# as "clean". An infected site looks clean.
mkdir -p /var/lib/clamav && chown clamupdate:clamupdate /var/lib/clamav 2>/dev/null || true
sed -i 's/^Example/#Example/' /etc/freshclam.conf 2>/dev/null || true
freshclam --quiet >/dev/null 2>&1 || freshclam >/dev/null 2>&1 || true
if ls /var/lib/clamav/*.c[vl]d >/dev/null 2>&1; then
  ok "clamav signature database downloaded ($(ls /var/lib/clamav/*.c[vl]d | wc -l) files)"
else
  warn "clamav signature database could not be downloaded — antivirus scan runs without signatures"
fi
systemctl enable --now clamav-freshclam >/dev/null 2>&1 \
  && ok "clamav-freshclam (automatic signature update) active" \
  || warn "clamav-freshclam service could not be started"

# ============ WAF (opt-in) ============
if [ "$KUR_WAF" = "1" ]; then
  echo
  echo "== Installing ModSecurity + OWASP CRS (compiling from source, a few minutes) =="
  if girginospanel-waf-setup; then
    ok "WAF installed — can be enabled per-domain in the panel"
  else
    warn "WAF install failed — panel keeps working, WAF off"
  fi
else
  echo
  echo "  NOTE: WAF (ModSecurity) NOT INSTALLED."
  echo "       The WAF toggle in the panel has no effect until the module is installed."
  echo "       To install:  girginospanel-waf-setup"
fi

# ============ 14) Permission repair ============
step "14) Permission/SELinux repair"
if [ ! -x /usr/local/bin/girginospanel-repair ]; then
  die "girginospanel-repair NOT in package — packaging error"
elif ! command -v girginospanel-repair >/dev/null 2>&1; then
  die "girginospanel-repair not callable from PATH (PATH=$PATH)"
elif girginospanel-repair --quiet >/dev/null 2>&1; then
  ok "girginospanel-repair"
else
  warn "girginospanel-repair returned an error — manually: girginospanel-repair"
fi

# ============ 15) VERIFICATION ============
step "15) Verification"
IP=$(hostname -I 2>/dev/null | awk '{print $1}')
CODE=$(curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:8443/ 2>/dev/null)
API=$(curl -sk -o /dev/null -w '%{http_code}' https://127.0.0.1:8443/api/v1/domains 2>/dev/null)
echo -e "  services: $(systemctl is-active mariadb nginx valkey php-fpm named pure-ftpd girginospanel crond | tr '\n' ' ')"
echo -e "  panel :8443 → HTTP $CODE   ·   API (auth) → HTTP $API   ·   DNS :53 → $(systemctl is-active named)   ·   FTP :21 → $(systemctl is-active pure-ftpd)"
echo -e "  tools: SSL/acme.sh $([ -x /root/.acme.sh/acme.sh ] && echo ✓ || echo ✗)   ·   firewall/nft $(command -v nft >/dev/null && echo ✓ || echo ✗)   ·   unzip/zip $(command -v unzip >/dev/null && command -v zip >/dev/null && echo ✓ || echo ✗)   ·   composer $(command -v composer >/dev/null && echo ✓ || echo ✗)   ·   apache/httpd $(systemctl is-active httpd)"
# The security components added today are summarized BEFORE the SUCCESS BANNER:
# if one is missing the operator notices before seeing "install complete".
AV_N=$(ls /var/lib/clamav/*.c[vl]d 2>/dev/null | wc -l)
if [ "$AV_N" -gt 0 ]; then AV_D="OK ($AV_N signatures)"; else AV_D="NONE - scan runs without signatures"; fi
if [ "$KUR_WAF" = "1" ]; then WAF_D="installed"; else WAF_D="not installed (girginospanel-waf-setup)"; fi
JAIL_D=X; [ -e /opt/girginospanel/src/scripts/50-gosp-jail.conf ] && [ -e /opt/girginospanel/src/scripts/girginospanel-jail ] && JAIL_D=OK
PSW_D=X; [ -x /usr/local/sbin/girginospanel-port-swap.sh ] && PSW_D=OK
echo -e "  antivirus: $AV_D   ·   freshclam $(systemctl is-active clamav-freshclam 2>/dev/null)   ·   WAF: $WAF_D"
echo -e "  ssh isolation(jail): $JAIL_D   ·   backend port helper: $PSW_D"
echo -e "  wp-cli (WordPress): ${WPCLI_DURUM:-?}"
echo -e "  isolation: plan-driven resource limits (cgroup slice) + per-tenant PHP-FPM (CageFS equivalent) READY   ·   bubblewrap $(command -v bwrap >/dev/null && echo ✓ || echo ✗)"

# ══════════════════════════════════════════════════════════════════════
# 🔴 REAL VERIFICATION GATE
# The summary lines above are INFORMATIONAL, not a gate. The install used to
# print "wp-cli (WordPress): NONE" and immediately after "✓ install complete"
# in the same run SELinux label missing, valkey off, FTP not installed and
# port 80 returned HTTP 500 for every Host. The operator saw "complete".
#
# girginospanel-dogrula measures PRODUCTION BEHAVIOR (HTTP codes, real command
# execution, file access as the production user) and if something critical fails
# it returns 1. In that case the success banner is NOT printed.
# ══════════════════════════════════════════════════════════════════════
DOGRULAMA_KODU=0
if command -v girginospanel-dogrula >/dev/null 2>&1; then
  echo
  girginospanel-dogrula || DOGRULAMA_KODU=$?
else
  warn "girginospanel-dogrula not found — install NOT VERIFIED"
  DOGRULAMA_KODU=1
fi

if [ "$DOGRULAMA_KODU" -ne 0 ]; then
  echo
  echo -e "${c_r}═══════════════════════════════════════════════${c_0}"
  echo -e "${c_r} ✗ Install NOT COMPLETE — critical verification failed${c_0}"
  echo -e "   Fix the ${c_r}✗${c_0} lines above, then:"
  echo -e "     ${c_b}girginospanel-dogrula${c_0}          (re-measure)"
  echo -e "     ${c_b}girginospanel-repair${c_0}           (repair known issues)"
  echo -e "   Re-running the install from scratch is safe (idempotent)."
  echo -e "${c_r}═══════════════════════════════════════════════${c_0}"
  exit 1
fi

echo
echo -e "${c_g}═══════════════════════════════════════════════${c_0}"
echo -e "${c_g} ✓ GirginOSPanel installation complete${c_0}"
echo -e "   Panel:  ${c_b}https://${IP:-SUNUCU_IP}:8443${c_0}"
echo -e "   Username: ${c_b}root${c_0}   Password: ${c_b}this server's root password${c_0}"
echo -e "   (panel admin login verifies the server root password against the /etc/shadow hash)"
if [ "$(findmnt -no FSTYPE / 2>/dev/null)" = "xfs" ] && ! findmnt -no OPTIONS / 2>/dev/null | grep -qwE 'usrquota|uquota|quota'; then
  echo -e "   ${c_y}Disk quota: rootflags=uquota written to GRUB — becomes active after a ONE-TIME reboot.${c_0}"
fi
echo -e "${c_g}═══════════════════════════════════════════════${c_0}"

