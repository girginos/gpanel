#!/usr/bin/env bash
# Dev kanal paketleyici — KAPILAR ZORUNLU.
#
# 🔴 NEDEN BU SCRIPT VAR: bu oturumda basari iddiasi UC KEZ yalan soyledi:
#   1) `go build | head && echo OK`  -> boru exit kodunu yuttu
#   2) `echo "OK"`                   -> hicbir sey olcmedi
#   3) `go build ./internal/xxx/...` -> PARCA derleme gecti, TAM AGAC KIRIKTI
# Bu yuzden asagidaki kapilarin HEPSI, TAM AGAC uzerinde, boru KULLANMADAN
# calisir ve biri duserse paket URETILMEZ.
set -uo pipefail

T=/root/gpanel-temiz
D=/opt/gpanel-dev-channel
cd "$T" || { echo "HATA: $T yok"; exit 1; }

basarisiz() { echo; echo "🔴 KAPI DUSTU: $1"; echo "PAKET URETILMEDI."; exit 1; }

echo "════════ KAPI 1/7: Go tam agac derleme ════════"
go build ./...
[ $? -eq 0 ] || basarisiz "go build ./... (tam agac)"
echo "  ✓ go build ./... exit=0"

echo "════════ KAPI 2/7: go vet tam agac ════════"
go vet ./...
[ $? -eq 0 ] || basarisiz "go vet ./... (tam agac)"
echo "  ✓ go vet ./... exit=0"

echo "════════ KAPI 3/7: frontend derleme (tsc + vite) ════════"
( cd frontend && npm run build >/tmp/fe-build.log 2>&1 )
[ $? -eq 0 ] || { tail -20 /tmp/fe-build.log; basarisiz "npm run build"; }
echo "  ✓ npm run build exit=0"

echo "════════ KAPI 4/7: installer sozdizimi + literal \\n yok ════════"
bash -n girginospanel-install.sh
[ $? -eq 0 ] || basarisiz "bash -n girginospanel-install.sh"
# 🔴 Kirli agacta mkdir satirinda LITERAL \n vardi -> bash'te kacisli 'n' =
# arguman olarak 'n' -> CWD'de cop dizin. Gercek satir devami olmali.
# Tespit ayri bir python dosyasinda: kabuk tirnak zinciri grep -P regex'ini
# bozuyordu (temiz dosyada YANLIS ALARM veriyordu). Dedektor iki kontrolle
# de kanitlandi: bozukta yakalar, temizde yakalamaz.
python3 /root/kapi_literal_n.py girginospanel-install.sh
[ $? -eq 0 ] || basarisiz "installer'da LITERAL \\n bulundu (satir devami bozuk)"
echo "  ✓ bash -n gecti, literal \\n yok"

echo "════════ KAPI 5/7: installer kuru calistirma (cop 'n' dizini olusmamali) ════════"
# mkdir satirini izole edip gecici kokte calistir: 'n' adli dizin OLUSMAMALI.
KURU=$(mktemp -d)
MKSATIR=$(grep -A3 'mkdir -p /opt/girginospanel/src/scripts' girginospanel-install.sh \
          | sed 's|/opt/girginospanel|'"$KURU"'/opt/girginospanel|g; s|/etc/girginospanel|'"$KURU"'/etc/girginospanel|g; s|/etc/ssl/girginospanel|'"$KURU"'/etc/ssl/girginospanel|g')
( cd "$KURU" && eval "$MKSATIR" ) >/dev/null 2>&1
if [ -e "$KURU/n" ]; then
  basarisiz "installer COP 'n' DIZINI olusturuyor (literal \\n hatasi geri gelmis)"
fi
[ -d "$KURU/opt/girginospanel/src/eklentiler" ] || basarisiz "eklenti payload dizini olusmadi"

echo "  ✓ cop 'n' dizini YOK, hedef dizinler olustu"
rm -rf "$KURU"

echo "════════ KAPI 6/7: migration numara cakismasi (yeni eklenenler) ════════"
CAK=$(ls migrations/*.sql | xargs -n1 basename | cut -c1-4 | sort | uniq -d | grep -v '^0011$')
if [ -n "$CAK" ]; then
  echo "$CAK"; basarisiz "migration numara cakismasi (0011 bilinen/eski istisna)"
fi
echo "  ✓ yeni cakisma yok"

echo
echo "════════ TUM KAPILAR GECTI — paket uretiliyor ════════"
TS=$(date +%s)
mkdir -p "$D/yedek.$TS"
cp -a "$D/gpanel-dev.tar.gz" "$D/gpanel-dev.tar.gz.sha256" "$D/install.sh" "$D/yedek.$TS/" 2>/dev/null
echo "  yedek: $D/yedek.$TS"

bash scripts/build-assets.sh >/tmp/assets.log 2>&1
[ $? -eq 0 ] || { tail -10 /tmp/assets.log; basarisiz "build-assets.sh"; }
# 🔴 BAYATLIK KAPISI — build-assets.sh'ten SONRA. Önce buradaydı, yani
# yükü ÜRETEN komuttan önce çalışıyordu: mail kaynağına dokunulan her turda
# paketleme, henüz derlenmemiş yük yüzünden boşuna duruyordu. Sıra artık
# doğru; burada ölçülen şey gerçekten pakete girecek olan dosyadır.
#
# Ölçülen arıza: mail eklentisinin kaynağı 2026-09-02'de düzeltildi, pakete
# konan ikili 2026-08-07 tarihliydi; düzeltme müşteriye hiç ulaşmadı ve
# kurulum çalışma biletini reddetti.
for _e in mail whitelabel calistirici; do
  _repo="${GOSP_EKLENTI_KOK:-/root}/gpanel-eklenti-${_e}"
  _ikili="assets/eklentiler/${_e}/girginospanel-eklenti-${_e}"
  # 🔴 Ucretli eklentiler pakete SIFRELI girer (.gosp); duz ELF hic bulunmaz.
  # whitelabel cekirdek/ucretsizdir, duz kalir (katalog.go Cekirdek:true).
  case "$_e" in mail|calistirici) _ikili="${_ikili}.gosp" ;; esac
  [ -f "$_ikili" ] || basarisiz "eklenti yuku yok: $_ikili (scripts/build-assets.sh)"
  if [ -d "$_repo" ]; then
    _enyeni=$(find "$_repo" -maxdepth 1 -name '*.go' -newer "$_ikili" -print -quit 2>/dev/null)
    [ -z "$_enyeni" ] || basarisiz "eklenti yuku BAYAT: $_ikili, $_enyeni dosyasindan eski"
  fi
done
for _aj in whitelabel calistirici; do
  [ -f "assets/eklentiler/${_aj}/app.js" ] || basarisiz "pakette eksik: assets/eklentiler/${_aj}/app.js"
done
# 🔴 DUZ UCRETLI IKILI KAPISI: sifreli teslimin tek anlami duz ikilinin kutuda
# HIC olmamasi. Bayat bir kopya kalirsa kurulum duz-metin dalina duser.
for _u in mail calistirici; do
  [ ! -f "assets/eklentiler/${_u}/girginospanel-eklenti-${_u}" ]     || basarisiz "assets'te DUZ ucretli ikili duruyor: ${_u} (yalniz .gosp olmali)"
done
echo "  ✓ eklenti yukleri guncel + ucretliler SIFRELI, whitelabel duz"

go version -m assets/girginospanel-server | grep -q "GOAMD64=v1" \
  || basarisiz "ikili GOAMD64=v1 DEGIL (eski CPU'larda hic calismaz)"
echo "  ✓ ikili GOAMD64=v1"

tar czf assets/frontend-dist.tar.gz -C frontend/dist .
tar czf assets/migrations.tar.gz   -C migrations .

rm -rf "$D/build/gpanel-dev"; mkdir -p "$D/build/gpanel-dev"
cp -a assets "$D/build/gpanel-dev/"
cp -a girginospanel-install.sh "$D/build/gpanel-dev/"
chmod +x "$D/build/gpanel-dev/girginospanel-install.sh"
# 🔴 IZOLASYON: Windows ajani AYRI binary + AYRI kanaldir (windows/dev + guncelle).
# Linux paketine .exe SIZMAZ. assets/ icine baska bir build'den kacak bir Windows
# binary'si dusmus olabilir (cp -a hepsini alir) — Linux paketinden AYIKLA ki
# "Windows datasi Linux yayinina karismasin". Asagidaki kapi bunu ayrica kanitlar.
find "$D/build/gpanel-dev" -type f -name '*.exe' -print -delete
tar czf "$D/gpanel-dev.tar.gz" -C "$D/build" gpanel-dev
( cd "$D" && sha256sum gpanel-dev.tar.gz > gpanel-dev.tar.gz.sha256 )

echo
echo "════════ PAKET DOGRULAMA ════════"
( cd "$D" && sha256sum -c gpanel-dev.tar.gz.sha256 ) || basarisiz "sha256 dogrulama"
# 🔴 KAPI, KURUCUNUN OLUMCUL SAYDIGI HER DOSYAYI KAPSAMALI.
# Onceki liste 4 dosyaydi; oysa girginospanel-install.sh asagidakilerin
# yoklugunda `die` ediyor. Bunlardan biri paketten dusse paketle.sh
# "PAKET URETILDI" basiyor, ariza MUSTERI SUNUCUSUNDA, kurulumun ortasinda
# ortaya cikiyordu — MariaDB parolasi donmus, panel ikilisi yazilmis,
# nginx conf'lari degistirilmis halde. Eksik olan seyi urettigi yerde
# yakalamak, musteride yarim kurulum birakmaktan ucuzdur.
# (Bu liste eklenti baslaticisini da kapsar: kurucu onu artik zorunlu kiliyor.)
_ICERIK=$(tar tzf "$D/gpanel-dev.tar.gz")
for f in girginospanel-install.sh          assets/girginospanel-server          assets/girginospanel-eklenti-baslatici          assets/girginospanel-seed-admin          assets/girginospanel-avajan          assets/migrations.tar.gz          assets/frontend-dist.tar.gz          assets/nginx/_panel.conf          assets/nginx/_default80.conf          assets/nginx/php-fpm.conf          assets/php-fpm/phpmyadmin.conf          assets/phpmyadmin/pma-signon.php          assets/systemd/girginospanel.service          assets/systemd/girginospanel-db-backup.service          assets/systemd/girginospanel-db-backup.timer          assets/ops/50-gosp-jail.conf          assets/ops/girginospanel-jail          assets/ops/girginospanel-redis-setup          assets/ops/girginospanel-optimize          assets/ops/girginospanel-ftp-setup          assets/ops/girginospanel-repair          assets/ops/girginospanel-dogrula \
         assets/eklentiler/mail/girginospanel-eklenti-mail.gosp \
         assets/eklentiler/whitelabel/girginospanel-eklenti-whitelabel \
         assets/eklentiler/whitelabel/app.js \
         assets/eklentiler/calistirici/girginospanel-eklenti-calistirici.gosp \
         assets/eklentiler/calistirici/app.js; do
  n=$(printf '%s
' "$_ICERIK" | grep -c "gpanel-dev/$f\$")
  [ "$n" -eq 1 ] || basarisiz "pakette eksik: $f"
done
echo "  ✓ paket icerigi tam (27 zorunlu dosya)"
# 🔴 IZOLASYON KAPISI: Linux paketinde HICBIR Windows (.exe) artefakti olmamali.
EXE=$(tar tzf "$D/gpanel-dev.tar.gz" | grep -ic '\.exe$' || true)
[ "$EXE" -eq 0 ] || basarisiz "Linux paketinde $EXE adet .exe var — Windows sizintisi (izolasyon ihlali)"
echo "  ✓ pakette Windows (.exe) artefakti YOK — Linux/Windows ayrik"
# 🔴 SIFRELI TESLIM KAPISI: tarball'da ucretli eklentinin DUZ ikilisi olmamali.
for _u in mail calistirici; do
  DUZ=$(printf '%s
' "$_ICERIK" | grep -c "gpanel-dev/assets/eklentiler/${_u}/girginospanel-eklenti-${_u}$" || true)
  [ "$DUZ" -eq 0 ] || basarisiz "pakette ${_u} DUZ ikilisi var — sifreli teslim ihlali"
done
echo "  ✓ ucretli eklentiler pakette YALNIZ sifreli (.gosp)"
echo -n "  ikili damgasi lic-eu : "; strings assets/girginospanel-server | grep -c "lic-eu.girginos.io"
echo -n "  ikili damgasi app    : "; strings assets/girginospanel-server | grep -c "app.girginos.io"
# 🔴 Migrationlar IC tarball'da (assets/migrations.tar.gz). Dis tarball'i
# saymak DAIMA 0 verir ve "migration yok" gibi YANLIS okunur.
MIGT=$(tar xzOf "$D/gpanel-dev.tar.gz" gpanel-dev/assets/migrations.tar.gz 2>/dev/null | tar tz 2>/dev/null | grep -c '\.sql$')
echo "  migration sayisi     : $MIGT"
[ "$MIGT" -ge 58 ] || basarisiz "pakette migration sayisi beklenenden az ($MIGT)"
ls -la "$D/gpanel-dev.tar.gz"
echo
echo "✅ PAKET URETILDI"

# ══════════════════════════════════════════════════════════════════════
# 🔴 KAPI 7/7 — CANLI E2E. Kapi 1-6 "PAKET duzgun mu" diye sorar
# (derleniyor mu, dosyalar tam mi, sozdizimi gecerli mi). Hicbiri
# "KURULAN URUN CALISIYOR MU" diye sormaz — ve tam bu boslukta,
# 2026-09-10 tarihli harici test raporunda 21 GERCEK eksik cikti.
# Hicbiri kod incelemesiyle bulunmadi; hepsi urun KULLANILARAK bulundu
# (tar'siz sunucuda kurulum hic baslamiyordu, silinen DNS kaydi yayinda
# kaliyordu, pano yedek sayaci hep 0 idi, plan RAM'i cgroup'a yansimiyordu).
#
# Bu kapinin kurali: HTTP 200'e / DB satirina / "ok:true"ya GUVENME —
# etkiye bak (dig, zone serial, cgroup degeri, diskteki dosya).
# ══════════════════════════════════════════════════════════════════════
echo
echo "════════ KAPI 7/7: canli e2e (kurulan urun calisiyor mu) ════════"
if [ -z "${E2E_HOST:-}" ]; then
  echo "  ⚠️  E2E_HOST tanimsiz — CANLI DOGRULAMA YAPILMADI."
  echo "     Paket yalnizca 'iyi bicimlendirilmis' oldugu icin gecti;"
  echo "     KURULDUGUNDA CALISTIGI KANITLANMADI."
  echo "     Yayin oncesi mutlaka:"
  echo "       E2E_HOST=<tek-kullanimlik-vps> E2E_KUR=1 bash $T/scripts/e2e.sh"
elif E2E_HOST="$E2E_HOST" bash "$T/scripts/e2e.sh"; then
  echo "  ✓ e2e gecti — paket YAYINLANABILIR"
else
  echo
  echo "🔴 E2E DUSTU — paket dosyasi uretildi AMA YAYINLANMAMALI."
  exit 1
fi
