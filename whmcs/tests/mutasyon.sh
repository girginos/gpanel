#!/usr/bin/env bash
# Elle mutasyon testi: modulun kopyasinda tek tek bozma yapar, testlerin DUSMESINI bekler.
#   bash tests/mutasyon.sh      -> her mutant icin "OLDURULDU" ya da "HAYATTA" yazar
set -u
KOK="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
OLDU=0; HAYATTA=0

mutant() { # ad dosya eski yeni
  local ad="$1" dosya="$2" eski="$3" yeni="$4"
  rm -rf "$TMP/m"; mkdir -p "$TMP/m"; cp -r "$KOK/modules" "$TMP/m/modules"
  python3 - "$TMP/m/modules/servers/gpanel/$dosya" "$eski" "$yeni" <<'PY'
import sys
f, eski, yeni = sys.argv[1:4]
s = open(f, encoding='utf-8').read()
if eski not in s:
    print("MUTANT UYGULANAMADI (eski metin yok)"); sys.exit(3)
open(f, 'w', encoding='utf-8').write(s.replace(eski, yeni, 1))
PY
  [ $? -eq 3 ] && { echo "?? $ad: uygulanamadi"; return; }
  if GPANEL_MODUL="$TMP/m/modules/servers/gpanel" php "$KOK/tests/run.php" >"$TMP/cikti" 2>&1; then
    echo "HAYATTA   $ad"; HAYATTA=$((HAYATTA+1))
  else
    echo "OLDURULDU $ad  ($(grep -c FAIL "$TMP/cikti") test dustu)"; OLDU=$((OLDU+1))
  fi
}

G=lib/ApiIstemci.php; M=lib/Modul.php; Y=lib/Yardimci.php
mutant "M01 yanit gunlugu maskesiz"            $G '$yanitMetni = $ham !== '"''"' ? Yardimci::maskeleJson($ham, $gizliler) : '"''"';' '$yanitMetni = $ham;'
mutant "M02 gizli alan adi maskesi kapali"     $Y "'password', 'parola', 'authorization', 'anahtar'," "'x1', 'x2', 'x3', 'x4',"
mutant "M03 409 esleme bozuk"                  $G 'case $durum === 409:' 'case $durum === 4090:'
mutant "M04 403 izin grubu mesaji kayip"       $G "if (\$izinGrubu !== '') {" 'if (false) {'
mutant "M05 CreateAccount idempotent degil"    $M "if (\$k !== null && \$k['tur'] === Kayit::TUR_HOSTING && \$k['panel_id'] > 0) {" 'if (false) {'
mutant "M06 SSO adres/host dogrulamasi yok"    $M "if (!in_array(strtolower(trim(\$u['host'], '[]')), \$izinli, true)) {" 'if (false) {'
mutant "M07 Terminate 404 hata sayilir"        $M 'if ($e->durum !== 404) {' 'if (true) {'
mutant "M08 ChangePackage bos plan -> null"    $M '$c->planDegistir($id, self::planId($c, $o));' '$c->planDegistir($id, null);'
mutant "M09 TLS dogrulamasi varsayilan kapali" $G '$dogrula = !($dogrulamaKapali || trim((string) ($params['"'serveraccesshash'"'] ?? '"''"')) === '"'tls-dogrulama-kapali'"');' '$dogrula = false;'
mutant "M10 baska hizmete bagli kontrolu yok"  $M "if (Kayit::baskaHizmetteMi(\$id, Kayit::TUR_HOSTING, \$sid)) {" 'if (false) {'
mutant "M11 hesap devralma engeli yok"         $M "if (!\$niyet && !\$o['mevcutBagla']) {
            throw new ModulHatasi(\"'\" . (\$mevcut['alan_adi']" 'if (false) {
            throw new ModulHatasi("'"'"'" . ($mevcut['"'alan_adi'"']'
mutant "M12 UsageUpdate MB yukari yuvarlar"    $Y 'floor(max(0, (int) $kb) / 1024)' 'ceil(max(0, (int) $kb) / 1024)'
mutant "M13 parola min uzunluk 1"              $Y 'if ($uz < 12) {' 'if ($uz < 1) {'
mutant "M14 SSO askida (409) duz baglantiya duser" $M '$ucYok = $e->durum === 404 ||' '$ucYok = $e->durum === 409 || $e->durum === 404 ||'
mutant "M15 kimlik basligi Bearer"             $G "'Authorization: gosp ' . \$this->kullanici" "'Authorization: Bearer ' . \$this->kullanici"
mutant "M16 silmede onay parametresi yok"      $G "['onay' => \$alanAdi]" '[]'
mutant "M17 serversecure kapaliyken HTTP'ye izin" $G 'if (!Yardimci::acik($params['"'serversecure'"'] ?? '"''"')) {' 'if (false) {'
mutant "M18 SSO url fragment maskesi yok"      $Y "\$veri = preg_replace('/(https?:\\/\\/[^\\s\"\\'#]*)#[^\\s\"\\']+/i', '\$1#***', \$veri) ?? \$veri;" ''
echo; echo "oldurulen=$OLDU hayatta=$HAYATTA"
