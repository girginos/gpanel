#!/usr/bin/env bash
# GirginOSPanel — herkese açık kurulum önyükleyicisi
#
#   curl -fsSL https://get.girginos.io | sudo bash
#   curl -fsSL https://get.girginos.io | sudo bash -s -- --waf
#
# Yaptığı iş: yayınlanan installer paketini indirir, sha256'sını latest.txt ile
# DOĞRULAR, açar ve girginospanel-install.sh'ı verilen argümanlarla çalıştırır.
#
# 🔴 sha256 doğrulaması ZORUNLU: bu betik `curl | bash` ile çalıştırılıyor,
# yani indirilen tarball doğrudan root olarak çalışacak. Sağlama tutmazsa
# hiçbir şey çalıştırmadan çıkarız — "indirdik, herhalde doğrudur" YOK.
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


TABAN=${GOSP_TABAN:-https://surum.girginos.io/panel}
c_g=$'\e[32m'; c_r=$'\e[31m'; c_0=$'\e[0m'
bilgi(){ echo -e "  $*"; }
dur(){ echo -e "${c_r}✗ $*${c_0}" >&2; exit 1; }

[ "$(id -u)" = "0" ] || dur "root gerekli:  curl -fsSL https://get.girginos.io | sudo bash"

for k in curl tar sha256sum; do
  command -v "$k" >/dev/null || dur "'$k' bulunamadı — önce kurun"
done

echo
echo "  GirginOSPanel kurulumu"
echo "  ──────────────────────"

# 1) Sürüm bilgisi
LATEST=$(curl -fsSL --max-time 30 "$TABAN/latest.txt") || dur "sürüm bilgisi alınamadı ($TABAN/latest.txt)"
PAKET=$(echo "$LATEST"  | awk -F': ' '/^paket:/{print $2}')
BEK_SHA=$(echo "$LATEST" | awk -F': ' '/^sha256:/{print $2}')
SURUM=$(echo "$LATEST"  | awk -F': ' '/^surum:/{print $2}')
[ -n "$PAKET" ]   || dur "latest.txt içinde 'paket:' yok"
[ -n "$BEK_SHA" ] || dur "latest.txt içinde 'sha256:' yok — doğrulanamayan paket kurulmaz"
bilgi "sürüm: ${SURUM:-?}   paket: $PAKET"

# 2) İndir
GEC=$(mktemp -d /tmp/gosp-kur.XXXXXX) || dur "geçici dizin açılamadı"
temizle(){ rm -rf "$GEC"; }
trap temizle EXIT
bilgi "indiriliyor…"
curl -fSL --max-time 900 -o "$GEC/paket.tar.gz" "$TABAN/$PAKET" || dur "indirme başarısız"

# 3) DOĞRULA (kurulumdan ÖNCE)
GER_SHA=$(sha256sum "$GEC/paket.tar.gz" | cut -d' ' -f1)
if [ "$GER_SHA" != "$BEK_SHA" ]; then
  dur "sha256 TUTMUYOR — kurulum durduruldu
     beklenen: $BEK_SHA
     gelen   : $GER_SHA"
fi
bilgi "${c_g}✓${c_0} sha256 doğrulandı"

# 4) Aç
tar xzf "$GEC/paket.tar.gz" -C "$GEC" || dur "arşiv açılamadı"
KUR=$(find "$GEC" -maxdepth 2 -name girginospanel-install.sh -type f | head -1)
[ -n "$KUR" ] || dur "pakette girginospanel-install.sh yok"
chmod +x "$KUR"

# 5) Çalıştır — argümanları aynen aktar
bilgi "kurulum başlıyor…"
echo
cd "$(dirname "$KUR")" || dur "dizine girilemedi"
exec "$KUR" "$@"
