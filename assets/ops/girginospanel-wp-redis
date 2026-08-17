#!/usr/bin/env bash
# girginospanel-wp-redis — bir domain kullanıcısının WordPress kurulumlarına
# per-tenant izole Redis (Valkey) object cache bağlar/çözer. Idempotent. root ile çalıştır.
#
#   girginospanel-wp-redis <sistem_kullanici>        # BAĞLA
#   girginospanel-wp-redis <sistem_kullanici> off    # ÇÖZ
#
# Panelin "Redis Cache" özelliğiyle aynı reçete:
#   ACL user (~<sk>:* key-prefix, @dangerous kapalı + info/dbsize açık)
#   WP: WP_REDIS_PASSWORD=array(sk,pass) + phpredis + selective flush + drop-in elle kopya
set -uo pipefail

SK="${1:?kullanim: $0 <sistem_kullanici> [off]}"
ACTION="${2:-on}"
HOST=127.0.0.1; PORT=6379
ENV=/etc/girginospanel/env

# --- guardlar ---
if ! [[ "$SK" =~ ^[a-z0-9_]{1,32}$ ]]; then echo "gecersiz kullanici adi: $SK"; exit 1; fi
[ "$(id -u)" = 0 ] || { echo "root gerekli"; exit 1; }
ADMIN=$(grep -oP '^PANEL_REDIS_ADMIN_PASS=\K.*' "$ENV" 2>/dev/null)
[ -z "$ADMIN" ] && { echo "PANEL_REDIS_ADMIN_PASS yok -> once: girginospanel-redis-setup"; exit 1; }
id "$SK" >/dev/null 2>&1 || { echo "sistem kullanicisi yok: $SK"; exit 1; }

vc(){ REDISCLI_AUTH="$ADMIN" valkey-cli "$@"; }
wpc(){ runuser -u "$SK" -- env HOME="/home/$SK" /usr/bin/php -d memory_limit=512M /usr/local/bin/wp "$@"; }
say(){ printf '  %s\n' "$*"; }

# domain_id (panel DB kaydi icin)
DID=$(mysql -u root panel -N -e "SELECT id FROM domains WHERE sistem_kullanici='$SK' LIMIT 1;" 2>/dev/null)

# WP kurulumlari: public_html + bir seviye alt
DIRS=()
for d in "/home/$SK/public_html" "/home/$SK/public_html"/*/; do
  d="${d%/}"; [ -f "$d/wp-config.php" ] && DIRS+=("$d")
done

# ================= OFF =================
if [ "$ACTION" = "off" ]; then
  echo "==== Redis cozuluyor: $SK ===="
  for dir in "${DIRS[@]}"; do
    runuser -u "$SK" -- rm -f "$dir/wp-content/object-cache.php"
    for k in WP_REDIS_HOST WP_REDIS_PORT WP_REDIS_USERNAME WP_REDIS_PASSWORD WP_REDIS_PREFIX \
             WP_REDIS_SELECTIVE_FLUSH WP_REDIS_CLIENT WP_CACHE; do
      wpc config delete "$k" --path="$dir" >/dev/null 2>&1
    done
    say "cozuldu: $dir"
  done
  vc ACL DELUSER "$SK" >/dev/null; vc ACL SAVE >/dev/null
  [ -n "$DID" ] && mysql -u root panel -e "DELETE FROM cp_domain_redis WHERE domain_id=$DID;" 2>/dev/null
  say "ACL + DB kaydi silindi"
  exit 0
fi

# ================= ON =================
echo "==== Redis baglaniyor: $SK ===="
[ ${#DIRS[@]} -eq 0 ] && { echo "  UYARI: /home/$SK/public_html altinda WordPress bulunamadi"; }

# parola: mevcut DB kaydi varsa yeniden kullan, yoksa uret
PASS=$(mysql -u root panel -N -e "SELECT redis_pass FROM cp_domain_redis WHERE domain_id='${DID:-0}' AND aktif=1;" 2>/dev/null)
[ -z "$PASS" ] && PASS=$(openssl rand -hex 18)

# 1) ACL user (izole; @dangerous kapali + eklentinin read-only diagnostigi acik)
vc ACL SETUSER "$SK" on ">$PASS" resetkeys "~$SK:*" resetchannels "&$SK:*" \
   +@all -@dangerous -@admin +info +dbsize +command +ping +echo "+client|no-evict" >/dev/null
vc ACL SAVE >/dev/null
say "ACL user hazir: $SK (~$SK:*)"

# 2) panel DB kaydi (panel 'aktif' gostersin)
if [ -n "$DID" ]; then
  mysql -u root panel -e "INSERT INTO cp_domain_redis (domain_id, sk, redis_pass, aktif) VALUES ($DID,'$SK','$PASS',1)
    ON DUPLICATE KEY UPDATE sk=VALUES(sk), redis_pass=VALUES(redis_pass), aktif=1;" 2>/dev/null && say "panel DB kaydi guncellendi (domain #$DID)"
fi

# 3) her WP kurulumu
BAGLANAN=0
for dir in "${DIRS[@]}"; do
  say "WP: $dir"
  set_(){ local a=(config set "$1" "$2" --type=constant --path="$dir"); [ "${3:-}" = raw ] && a+=(--raw); wpc "${a[@]}" >/dev/null 2>&1; }
  set_ WP_REDIS_HOST "$HOST"
  set_ WP_REDIS_PORT "$PORT" raw
  set_ WP_REDIS_PASSWORD "array('$SK','$PASS')" raw    # ACL auth = dizi [kullanici, parola]
  set_ WP_REDIS_PREFIX "$SK:"
  set_ WP_REDIS_SELECTIVE_FLUSH true raw
  set_ WP_REDIS_CLIENT phpredis
  set_ WP_CACHE true raw
  wpc config delete WP_REDIS_USERNAME --path="$dir" >/dev/null 2>&1  # eski yanlis kalinti

  wpc plugin install redis-cache --activate --path="$dir" >/dev/null 2>&1
  # drop-in ELLE kopyala (wp redis enable flushdb'ye takiliyor)
  runuser -u "$SK" -- cp -f "$dir/wp-content/plugins/redis-cache/includes/object-cache.php" \
                            "$dir/wp-content/object-cache.php" 2>/dev/null

  ST=$(wpc redis status --path="$dir" 2>&1)
  if grep -q "Connected" <<<"$ST"; then
    say "  -> Connected (object cache aktif)"
    BAGLANAN=$((BAGLANAN+1))
  else
    say "  -> BAGLANAMADI:"; grep -iE "status|error" <<<"$ST" | head -2 | sed 's/^/     /'
  fi
done

echo "==== Ozet ===="
say "baglanan WP kurulumu: $BAGLANAN / ${#DIRS[@]}"
say "kullanici=$SK  parola=$PASS  prefix=$SK:  sunucu=$HOST:$PORT"
