#!/usr/bin/env bash
# build-assets.sh — GirginOSPanel release binary'lerini DOĞRU bayraklarla derler.
#
# 🔴 NEDEN GOAMD64=v1 ZORUNLU:
#   AlmaLinux 10 / go1.26+ varsayılan olarak `go env GOAMD64=v3` üretir. v3 ile derlenen
#   binary, v3 mikromimari (AVX2 vb.) desteklemeyen eski/yaygın müşteri CPU'larında
#     "This program can only be run on AMD64 processors with v3 microarchitecture support"
#   verip HİÇ ÇALIŞMAZ. Bu yüzden yayınlanan `assets/girginospanel-server` DAİMA
#   GOAMD64=v1 ile derlenmelidir. Bu script bunu sabitler — elle `go build` YAPMA.
#
# Kullanım:
#   scripts/build-assets.sh          # server (+ varsa seed-admin) derle → assets/'a yaz
#
# Not: frontend-dist.tar.gz / migrations.tar.gz / ops arch-BAĞIMSIZDIR, bu script onlara
#      dokunmaz (npm run build ayrı yapılır). Sadece Go binary'leri derler.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# Eski CPU uyumu için ZORUNLU derleme ortamı.
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64
export GOAMD64=v1

echo "== girginospanel-server derleniyor (GOAMD64=$GOAMD64, CGO_ENABLED=$CGO_ENABLED) =="
# 🔴 Panel surum damgasi: GOSP_SURUM verilirse ldflags ile enjekte edilir
# (uretim yayini girginospanel-paketle GOSP_SURUM=0.3.0-YYYYMMDD gecer).
# Verilmezse surum.go varsayilani (0.3.0-f3) kalir (dev / dev-kanali).
# 🔴 ONCE bu adim ldflags'siz derleyip girginospanel-paketle'nin satir-105
# ldflags binary'sini EZIYORDU -> yayinlanan binary surumu daima f3 dusuyordu.
if [ -n "${GOSP_SURUM:-}" ]; then
  go build -trimpath -ldflags "-X girginospanel/internal/surum.Panel=$GOSP_SURUM" -o assets/girginospanel-server ./cmd/server
else
  go build -o assets/girginospanel-server ./cmd/server
fi

# 🔴 Eklenti başlatıcısı: systemd birimlerinin ExecStart'ı BUDUR.
# Panelle AYNI GOAMD64=v1 bayrağıyla derlenmek ZORUNDA — v3 ile derlenseydi
# eski CPU'lu müşteride eklenti "Exec format" ile hiç açılmazdı ve hata
# panelde değil, eklentinin biriminde görünürdü (teşhisi zor).
echo "== girginospanel-eklenti-baslatici derleniyor (GOAMD64=$GOAMD64) =="
go build -o assets/girginospanel-eklenti-baslatici ./cmd/gosp-baslatici

# seed-admin: scripts/seed_admin.go içinde //go:build ignore var → dosyayı doğrudan derle.
if [ -f scripts/seed_admin.go ]; then
  echo "== girginospanel-seed-admin derleniyor (GOAMD64=$GOAMD64) =="
  go build -o assets/girginospanel-seed-admin scripts/seed_admin.go
fi

# 🔴 LİSANSLI EKLENTİ YÜKÜ DE BURADAN DERLENİR.
#
# 🔴 ÜCRETLİ eklentiler (mail, calistirici) pakete YALNIZ ŞİFRELİ (.gosp) girer;
# düz ELF paketten ÇIKARILIR. Eskiden düz ELF gidiyordu ve kurulum paketi HERKESE
# AÇIK indirilebildiği için ödeme yapmamış biri ikiliyi alıp `calismaHakki()`
# gövdesini yamalayarak ücretli yüzeyi açabiliyordu (paket.go:5-15 bunu ölçülmüş
# açık olarak belgeliyor; 2026-09-13'te canlı doğrulandı).
# whitelabel ÇEKİRDEK/ücretsizdir (katalog.go Cekirdek:true) → düz kalır. Daha önce buraya ELLE kopyalanıyordu ve sessizce eskiyordu:
# mail eklentisinin suite-bileti düzeltmesi 2026-09-02'de yapıldı, pakete konan
# ikili ise 2026-08-07 tarihliydi — düzeltme müşteriye HİÇ ulaşmadı, kurulum
# "Çalışma bileti başka bir ürüne ait (gpanel-suite)" diyerek reddediyordu.
# Panel ikilisiyle AYNI bayraklarla (GOAMD64=v1) derlenmek zorunda: eski CPU'da
# çalışmayan bir eklenti, çalışmayan bir panelden farksızdır.
#
# Depo yoksa SESSİZCE ATLANMAZ. Bayat yük göndermek, derlemeyi durdurmaktan
# daha pahalıdır; bilerek atlamak isteyen GOSP_EKLENTI_ATLA=1 vermelidir.
# 🔴 Paketleyici burada derlenir: cmd/gosp-paketle hicbir yerde derlenmiyordu,
# bu yuzden .gosp uretimi hic calismamisti.
GOSP_PAKETLE="${GOSP_PAKETLE:-$REPO_ROOT/.gosp-paketle.bin}"
go build -trimpath -o "$GOSP_PAKETLE" "$REPO_ROOT/cmd/gosp-paketle"   || { echo "HATA: gosp-paketle derlenemedi" >&2; exit 1; }

# Lisans sunucusuna girilecek (urun, anahtar_kimlik) -> K kayitlari burada toplanir.
GOSP_ANAHTAR_LISTE="$REPO_ROOT/.gosp-anahtarlari.json"
: > "$GOSP_ANAHTAR_LISTE"; chmod 0600 "$GOSP_ANAHTAR_LISTE"
_gosp_ilk=1

for _e in mail whitelabel calistirici; do
  # Ucretli mi + lisans sunucusundaki urun slug'i (katalog.go ile AYNI olmali)
  case "$_e" in
    mail)        _slug="mail-server"; _ucretli=1 ;;
    calistirici) _slug="app-runner";  _ucretli=1 ;;
    *)           _slug="";            _ucretli=0 ;;
  esac
  _repo="${GOSP_EKLENTI_KOK:-/root}/gpanel-eklenti-${_e}"
  _hedef="assets/eklentiler/${_e}/girginospanel-eklenti-${_e}"
  if [ ! -d "$_repo" ]; then
    if [ "${GOSP_EKLENTI_ATLA:-0}" = "1" ]; then
      # 🔴 SESSİZ ATLAMA YOK. Kaçış kullanıldığında paketteki yükün NE
      # OLDUĞU ekrana basılır; aksi halde "atlandı" satırı görülüp aylarca
      # eski bir ikili yayınlanabilir (mail eklentisinde tam olarak bu oldu:
      # kaynak 2026-09-02'de düzeltildi, pakete 2026-08-07 ikilisi gitti).
      echo "!! ${_e} deposu yok ($_repo) — GOSP_EKLENTI_ATLA=1 ile ATLANDI"
      if [ -f "$REPO_ROOT/$_hedef" ]; then
        echo "   pakete GİDECEK yük: $(date -r "$REPO_ROOT/$_hedef" '+%Y-%m-%d %H:%M') — $(go version -m "$REPO_ROOT/$_hedef" 2>/dev/null | head -1)"
      else
        echo "   UYARI: $_hedef hiç YOK — paket bu eklentiyi taşımayacak"
      fi
      continue
    fi
    echo "HATA: ${_e} eklenti deposu yok ($_repo). Paket yükü elle güncellenemez;" >&2
    echo "      bilerek atlamak için GOSP_EKLENTI_ATLA=1 verin." >&2
    exit 1
  fi
  # 🔴 SÜRÜM DAMGASI ZORUNLU. Eklentiler sürümlerini ldflags ile alır
  # (main.go: `var surum = "0.3.0-dev"` + `-X main.surum=<sürüm>`); düz
  # `go build` ile derlenen ikili kendini "0.3.0-dev" diye tanıtır. Ölçüldü:
  # yayındaki paket 0.3.7 derken elle derlenen ikili 0.3.0-dev raporluyordu —
  # paket etiketiyle çalışan ikili birbirini tutmuyor, sürüm denetimi bayat
  # okuyor. Kaynak sırası: SURUM dosyası → GOSP_SURUM_<AD> → (yalnız panelle
  # birlikte çıkan eklentiler için) panel sürümü.
  _surum=""
  [ -f "$_repo/SURUM" ] && _surum=$(head -1 "$_repo/SURUM" | tr -d '[:space:]')
  eval "_ev=\${GOSP_SURUM_$(echo "$_e" | tr 'a-z' 'A-Z'):-}"
  [ -n "$_ev" ] && _surum="$_ev"
  if [ -z "$_surum" ] && [ "$_e" = "whitelabel" ]; then
    # whitelabel panelle BİRLİKTE çıkar; cp_eklentiler kaydına da panel sürümü
    # yazılıyor (kurulum_whitelabel.go: surum.Panel). Tek kaynak orası olsun.
    _surum=$(grep -oE 'Panel = "[^"]+"' "$REPO_ROOT/internal/surum/surum.go" | head -1 | sed 's/.*"\(.*\)"/\1/')
  fi
  if [ -z "$_surum" ]; then
    echo "HATA: ${_e} için sürüm yok. $_repo/SURUM dosyası oluşturun" >&2
    echo "      ya da GOSP_SURUM_$(echo "$_e" | tr 'a-z' 'A-Z')=<sürüm> verin." >&2
    exit 1
  fi
  echo "== eklenti yükü: ${_e} ${_surum} derleniyor (GOAMD64=$GOAMD64, ucretli=$_ucretli) =="
  mkdir -p "$(dirname "$_hedef")"
  # 🔴 Ucretli eklentinin DUZ ikilisi assets'e HIC yazilmaz: gecici yola derlenir,
  # sifrelenir, gecici silinir. Boylece "hem duz hem sifreli gitti" arizasi imkansiz.
  _duz="$REPO_ROOT/$_hedef"
  [ "$_ucretli" = "1" ] && _duz="$(mktemp /tmp/gosp-duz-XXXXXXXX)"
  # 🔴 -s -w (strip): sembol tablosu + debug_info olmadan `main.calismaHakki`
  # gibi lisans kapisi adlari ikilide DUZ OKUNMAZ. Olculdu (2026-09-13):
  # strip'siz ikilide `strings | grep main.calismaHakki` 2 eslesme veriyordu.
  if [ "${GOSP_GARBLE:-0}" = "1" ] && [ "$_ucretli" = "1" ]; then
    # 🔴 garble: sembol + .gopclntab fonksiyon adlari + kontrol akisi obfuscate.
    # Yalniz UCRETLI eklentide (whitelabel duz kalir). garble buildinfo'yu
    # siler -> GOAMD64 damgasi kaybolur; bu yuzden damgayi garble'dan ONCE,
    # ayri normal bir derlemeyle kanitlariz (GOAMD64 env zaten zorlu).
    command -v garble >/dev/null 2>&1 || { echo "HATA: GOSP_GARBLE=1 ama garble kurulu degil (go install mvdan.cc/garble@latest)" >&2; exit 1; }
    _dam="$(mktemp /tmp/gosp-dam-XXXXXXXX)"
    ( cd "$_repo" && go build -trimpath -o "$_dam" . )
    go version -m "$_dam" | grep -q "GOAMD64=v1" || { echo "HATA: ${_e} yükü GOAMD64=v1 DEĞİL" >&2; rm -f "$_dam"; exit 1; }
    rm -f "$_dam"
    echo "   garble ile obfuscate ediliyor (birkac dk surebilir)..."
    ( cd "$_repo" && garble -literals -tiny build -ldflags "-X main.surum=$_surum" -o "$_duz" . )       || { echo "HATA: ${_e} garble derlemesi basarisiz" >&2; exit 1; }
    chmod 0700 "$_duz"
  else
    ( cd "$_repo" && go build -trimpath -ldflags "-s -w -X main.surum=$_surum" -o "$_duz" . )
    chmod 0700 "$_duz"
    # GOAMD64 kapisi DUZ ikili uzerinde olculur (.gosp'a `go version -m` calismaz).
    go version -m "$_duz" | grep -q "GOAMD64=v1" || { echo "HATA: ${_e} yükü GOAMD64=v1 DEĞİL" >&2; exit 1; }
  fi

  if [ "$_ucretli" = "1" ]; then
    _kdosya="/root/.gosp-icerik-${_slug}"
    [ -s "$_kdosya" ] || { echo "HATA: icerik anahtari yok: $_kdosya (openssl rand -hex 32 ile uretin, 0600)" >&2; exit 1; }
    _k=$(tr -d '[:space:]' < "$_kdosya")
    _gosp="$REPO_ROOT/${_hedef}.gosp"
    # 🔴 -k ZORUNLU: -k'siz her kosu YENI K + YENI anahtar_kimlik uretir ve
    # lisans sunucusundaki (urun, kimlik) -> K kaydini SESSIZCE gecersiz kilar.
    _cikti=$("$GOSP_PAKETLE" -giris "$_duz" -cikis "$_gosp" -urun "$_slug"                -eklenti "$_e" -surum "$_surum" -k "$_k"                -imza-anahtari "${GOSP_IMZA_ANAHTARI:-/root/.gosp-paket-imza}")       || { echo "HATA: ${_e} sifreli paketi uretilemedi" >&2; rm -f "$_duz"; exit 1; }
    _kimlik=$(printf '%s
' "$_cikti" | sed -n 's/^anahtar_kimlik: *//p' | head -1)
    [ -n "$_kimlik" ] || { echo "HATA: ${_e} anahtar_kimlik okunamadi" >&2; rm -f "$_duz"; exit 1; }
    rm -f "$_duz"
    # 🔴 Bayat DUZ ikili assets'te kalmasin (onceki surumlerden artakalan).
    rm -f "$REPO_ROOT/$_hedef"
    echo "   + ${_e} SIFRELI: $(basename "$_gosp") (kimlik $_kimlik)"
    [ "$_gosp_ilk" = "1" ] || printf ',
' >> "$GOSP_ANAHTAR_LISTE"
    printf '  {"urun":"%s","kimlik":"%s","k_hex":"%s","gosp":"/opt/girginos-io/packages/%s.gosp"}'       "$_slug" "$_kimlik" "$_k" "$_slug" >> "$GOSP_ANAHTAR_LISTE"
    _gosp_ilk=0
  fi
  # Bazı eklentiler ikiliye ek olarak arayüz paketi de taşır (whitelabel:
  # app.js). Kurulum önkoşulu ikisini birden arar; biri eksikse "Kur" ilk
  # adımda düşer, o yüzden ikisi de aynı kapıdan geçmeli.
  # app.js SIFRELENMEZ: .gosp yalniz ikiliyi tasir, arayuz paketini kurulum
  # ayrica okur (kurulum_calistirici.go). Duz kalmasi TASARIM.
  for _ek in app.js; do
    if [ -f "$_repo/$_ek" ]; then
      cp -f "$_repo/$_ek" "$(dirname "$REPO_ROOT/$_hedef")/$_ek"
      echo "   + ${_e}/${_ek}"
    fi
  done
done

# Anahtar listesini gecerli JSON dizisi yap (lisans sunucusuna bu gider).
if [ -s "$GOSP_ANAHTAR_LISTE" ]; then
  printf '
]
' >> "$GOSP_ANAHTAR_LISTE"
  sed -i '1i [' "$GOSP_ANAHTAR_LISTE"
  echo "== lisans sunucusu anahtar listesi: $GOSP_ANAHTAR_LISTE (0600)"
  echo "   -> 182:/etc/girginos-io/package-keys.json + .gosp'lar /opt/girginos-io/packages/ + systemctl restart girginos-io"
fi

echo "== doğrulama: GOAMD64 damgası v1 olmalı =="
go version -m assets/girginospanel-server | grep -E "GOAMD64" || true

echo "✓ Bitti. 'assets/frontend-dist.tar.gz'i güncellemek için ayrıca: (cd frontend && npm run build) sonra dist'i paketle."
