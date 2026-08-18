#!/usr/bin/env bash
# GirginOSPanel — güncelleme önyükleyicisi
#
#   curl -fsSL https://get.girginos.io/guncelle | sudo bash
#   curl -fsSL https://get.girginos.io/guncelle | sudo bash -s -- --dry-run
#
# NEDEN VAR: `girginospanel-update` kendi kendini günceller. Kurulu sürüm
# bozuk ya da bayatsa kendini onaramaz — 2026-08-17'de tam olarak bu oldu:
# eski updater GitHub'dan çekiyordu, GitHub hız sınırı (429) uyguladı ve
# updater kendini düzelten yeni sürümü indiremediği için sunucular kilitli
# kaldı. Bu önyükleyici araçları HER ÇALIŞMADA sunucudan taze indirir,
# dolayısıyla kurulu araçların durumundan bağımsızdır.
#
# 🔴 sha256 doğrulaması ZORUNLU: bu betik `curl | bash` ile root olarak
# çalışıyor ve indirdiği şeyleri /usr/local/bin'e kuruyor. Sağlama
# tutmazsa hiçbir şey kurulmaz.
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
c_g=$'\e[32m'; c_r=$'\e[31m'; c_y=$'\e[33m'; c_0=$'\e[0m'
bilgi(){ echo "  $*"; }
ok(){    echo -e "  ${c_g}✓${c_0} $*"; }
uyar(){  echo -e "  ${c_y}!${c_0} $*"; }
dur(){   echo -e "${c_r}✗ $*${c_0}" >&2; exit 1; }

[ "$(id -u)" = "0" ] || dur "root gerekli:  curl -fsSL https://get.girginos.io/guncelle | sudo bash"
for k in curl tar sha256sum install; do
  command -v "$k" >/dev/null || dur "'$k' bulunamadı — önce kurun"
done
[ -d /opt/girginospanel ] || dur "/opt/girginospanel yok — bu sunucuda GirginOSPanel kurulu değil.
     Kurulum için:  curl -fsSL https://get.girginos.io | sudo bash"

echo
echo "  GirginOSPanel güncelleme önyükleyicisi"
echo "  ──────────────────────────────────────"

# ── 1) Sürüm bilgisi ────────────────────────────────────────────────
LATEST=$(curl -fsSL --max-time 60 "$TABAN/latest.txt") \
  || dur "sürüm bilgisi alınamadı ($TABAN/latest.txt) — ağ/DNS kontrol edin"
PAKET=$(printf '%s' "$LATEST"  | awk -F': ' '/^paket:/{print $2}')
BEK=$(printf '%s'   "$LATEST"  | awk -F': ' '/^sha256:/{print $2}')
SURUM=$(printf '%s' "$LATEST"  | awk -F': ' '/^surum:/{print $2}')
[ -n "$PAKET" ] || dur "latest.txt içinde 'paket:' yok"
[ -n "$BEK" ]   || dur "latest.txt içinde 'sha256:' yok — doğrulanamayan paket kurulmaz"
bilgi "yayındaki sürüm: ${SURUM:-?}"

# ── 2) İndir + DOĞRULA ──────────────────────────────────────────────
GEC=$(mktemp -d /tmp/gosp-guncelle.XXXXXX) || dur "geçici dizin açılamadı"
trap 'rm -rf "$GEC"' EXIT
bilgi "indiriliyor…"
curl -fsSL --max-time 900 -o "$GEC/paket.tar.gz" "$TABAN/$PAKET" || dur "indirme başarısız"
GER=$(sha256sum "$GEC/paket.tar.gz" | cut -d' ' -f1)
[ "$GER" = "$BEK" ] || dur "sha256 TUTMUYOR — hiçbir şey kurulmadı
     beklenen: $BEK
     gelen   : $GER"
ok "sha256 doğrulandı"

tar xzf "$GEC/paket.tar.gz" -C "$GEC" || dur "arşiv açılamadı"
OPS=$(find "$GEC" -maxdepth 3 -type d -name ops -path '*/assets/*' | head -1)
[ -n "$OPS" ] || dur "pakette assets/ops yok (yapı beklenenden farklı)"

# ── 3) Araçları TAZE kur ────────────────────────────────────────────
# Kurulu sürümün durumu önemsiz: her koşuda paketten geleni yazıyoruz.
# Bu, "araç kendini güncelleyemiyor" kilidini kalıcı olarak kırar.
KURULAN=0
for f in "$OPS"/*; do
  [ -f "$f" ] || continue
  bn=$(basename "$f")
  # 🔴 Artik/yedek dosyalar KURULMAZ. Bunlar BAYAT kod olur ve root
  # yetkisiyle /usr/local/bin e 0755 kurulursa yanlislikla calistirilabilir
  # (pakette girginospanel-repair.bak.<zaman> bulundu ve kuruluyordu).
  case "$bn" in *.bak|*.bak.*|*.orig|*.rej|*~) continue ;; esac
  case "$bn" in
    *.conf|*.service|*.timer) continue ;;   # yapılandırma dosyaları buraya değil
  esac
  hedef="/usr/local/bin/${bn%.sh}"
  if ! cmp -s "$f" "$hedef" 2>/dev/null; then
    install -m 0755 "$f" "$hedef" || dur "kurulamadı: $hedef"
    KURULAN=$((KURULAN+1))
  fi
done
# Panel helper'ı TAM olarak bu yolda arıyor (bkz. internal/portyonetim).
if [ -f "$OPS/girginospanel-port-swap.sh" ]; then
  install -D -m 0700 "$OPS/girginospanel-port-swap.sh" \
    /usr/local/sbin/girginospanel-port-swap.sh || dur "port-swap kurulamadı"
fi
ok "ops araçları tazelendi ($KURULAN dosya güncellendi)"

command -v girginospanel-update >/dev/null || dur "girginospanel-update kurulamadı — pakette yok?"

# ── 4) Asıl güncellemeyi çalıştır ───────────────────────────────────
echo
bilgi "güncelleme başlıyor…"
echo
exec girginospanel-update "$@"
