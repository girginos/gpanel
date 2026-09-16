#!/usr/bin/env bash
# ════════════════════════════════════════════════════════════════════
# gPanel E2E — HEDEF SUNUCUDA calisir.
# Iddiasi "paket duzgun" DEGIL, "KURULAN URUN CALISIYOR".
#
# 🔴 TEMEL KURAL: HTTP 200'e / DB satirina / "ok:true"ya GUVENME.
#    Her test ETKI uzerinden dogrulanir: zone dosyasi + dig, systemd
#    cgroup degeri, diskteki havuz dosyasi, gercek yedek dosyasi.
#    Bugune kadar kacan hatalarin HEPSI "basari sinyali dogru, etki
#    yanlis" seklindeydi (DNS silme ok:true derken BIND cevapliyordu;
#    plan 2048 yazarken cgroup 256M kaliyordu; sayac sorgusu hata yutuyordu).
#
# Cikis: 0 = hepsi gecti, 1 = en az bir test dustu.
# Tum test varliklari "e2e" onekli; basta ve sonda temizlenir.
# ════════════════════════════════════════════════════════════════════
set -uo pipefail

API="https://127.0.0.1:8443/api/v1"
DOM="e2ekontrol.com"
SK="c_e2ekontrol"
ZONE="/var/named/${DOM}.zone"
PAKET_AD="E2E-kucuk-paket"

gecti=0; kaldi=0; atlandi=0
declare -a DUSENLER=()
ok(){   printf '  \033[32m+\033[0m %s\n' "$*"; gecti=$((gecti+1)); }
no(){   printf '  \033[31mX\033[0m %s\n' "$*"; kaldi=$((kaldi+1)); DUSENLER+=("$*"); }
atla(){ printf '  \033[33m~\033[0m %s (atlandi)\n' "$*"; atlandi=$((atlandi+1)); }
baslik(){ printf '\n\033[1;34m-- %s\033[0m\n' "$*"; }

JETON=$(python3 /root/.e2e-jeton.py 2>/dev/null)
if [ -z "$JETON" ]; then
  echo "HATA: jeton uretilemedi - panel kurulu/calisir durumda mi?"; exit 1
fi
H=(-sk --max-time 120 -H "Authorization: Bearer $JETON" -H "Content-Type: application/json")

alan(){ python3 -c "import json,sys
try: d=json.load(sys.stdin)
except Exception: sys.exit(1)
print(d.get('$1',''))" 2>/dev/null; }

temizle() {
  local d
  d=$(mysql -N -B panel -e "SELECT id FROM domains WHERE alan_adi='$DOM';" 2>/dev/null | head -1)
  if [ -n "$d" ]; then
    curl "${H[@]}" -o /dev/null -X DELETE "$API/domains/$d" >/dev/null 2>&1
    sleep 3
  fi
  mysql panel -e "DELETE FROM reseller_plans WHERE ad='$PAKET_AD';"       >/dev/null 2>&1
  mysql panel -e "DELETE FROM customers WHERE eposta='e2e@kontrol.test';" >/dev/null 2>&1
  rm -rf "/var/backups/girginospanel/$SK" 2>/dev/null
}

echo "===================================================="
echo " gPanel E2E - $(hostname) - $(date '+%F %T')"
echo "===================================================="
temizle

baslik "A · Kurulum sagligi (taze kurulum regresyonlari)"

systemctl is-active --quiet girginospanel && ok "panel servisi active" || no "panel servisi calismiyor"

KOD=$(curl -sk -o /dev/null -w '%{http_code}' --max-time 20 https://127.0.0.1:8443/giris)
[ "$KOD" = "200" ] && ok "panel :8443 giris sayfasi 200" || no "panel :8443 -> HTTP $KOD"

if [ -s /var/log/girginospanel-install.log ]; then
  ok "kurulum ayrinti logu var ($(wc -l < /var/log/girginospanel-install.log) satir)"
else
  atla "kurulum ayrinti logu yok (panel bu makinede kurulmamis olabilir)"
fi

if systemctl is-active --quiet chronyd; then
  ok "chronyd active (NTP: $(timedatectl show -p NTPSynchronized --value 2>/dev/null))"
else
  no "chronyd calismiyor - SSL/JWT/yedek zamani kayabilir"
fi

[ -x /usr/local/bin/composer ] && ok "composer kurulu" || no "composer kurulmamis (HOME tuzagi?)"
if [ -x /root/.acme.sh/acme.sh ] && [ ! -e /.acme.sh ]; then
  ok "acme.sh /root/.acme.sh altinda (yanlis /.acme.sh yok)"
else
  no "acme.sh yanlis yerde - HOME bos mu kaldi? (/.acme.sh: $([ -e /.acme.sh ] && echo VAR || echo yok))"
fi

NGT=$(nginx -t 2>&1)
if echo "$NGT" | grep -qi "ssl_stapling"; then
  no "nginx -t ssl_stapling uyariyor: $(echo "$NGT" | grep -i ssl_stapling | head -1)"
else
  ok "nginx -t ssl_stapling uyarisi yok"
fi

baslik "B · Domain yasam dongusu (diskteki ETKI)"

YANIT=$(curl "${H[@]}" -X POST "$API/domains" -d "{\"alan_adi\":\"$DOM\",\"php_surum\":\"8.3\"}")
DID=$(echo "$YANIT" | alan id)
if [ -z "$DID" ]; then
  no "domain olusturulamadi: $(echo "$YANIT" | head -c 200)"
  DID=""
else
  ok "domain olusturuldu (id=$DID, sk=$SK)"
  sleep 2
  id -u "$SK" >/dev/null 2>&1            && ok "sistem kullanicisi olustu"       || no "sistem kullanicisi YOK"
  [ -d "/home/$SK/public_html" ]         && ok "belge koku olustu (public_html)" || no "public_html YOK"
  ls /etc/nginx/conf.d/ | grep -q "$SK"  && ok "nginx vhost yazildi"             || no "nginx vhost YOK"
  [ -f "$ZONE" ]                         && ok "DNS zone dosyasi yazildi"        || no "zone dosyasi YOK"
  POOL="/etc/php-fpm-tenant/$SK/pool.conf"
  [ -f "$POOL" ]                         && ok "per-tenant FPM havuzu yazildi"   || no "FPM havuzu YOK"

  if [ -f "$POOL" ]; then
    PLAN=$(mysql -N -B panel -e "SELECT COALESCE(p.ram_mb,0), COALESCE(p.disk_kota_mb,0) FROM domains d LEFT JOIN service_plans p ON p.id=d.plan_id WHERE d.id=$DID;" 2>/dev/null)
    PRAM=$(echo "$PLAN" | awk '{print $1}'); PDISK=$(echo "$PLAN" | awk '{print $2}')
    EXEC=$(grep -oP 'max_execution_time\] = \K[0-9]+' "$POOL")
    INP=$(grep -oP 'max_input_time\] = \K[0-9]+'      "$POOL")
    UPL=$(grep -oP 'upload_max_filesize\] = \K[0-9]+' "$POOL")
    PST=$(grep -oP 'post_max_size\] = \K[0-9]+'       "$POOL")
    [ "${EXEC:-99999}" -le 300 ] && ok "max_execution_time tavanli ($EXEC <= 300)" || no "max_execution_time=$EXEC tavansiz"
    [ "${INP:-99999}"  -le 600 ] && ok "max_input_time tavanli ($INP <= 600)"      || no "max_input_time=$INP tavansiz"
    if [ -n "$UPL" ] && [ -n "$PST" ] && [ "${PDISK:-0}" -gt 0 ]; then
      if [ "$UPL" -le "$PDISK" ] && [ "$PST" -le "$PDISK" ]; then
        ok "upload/post disk kotasinin altinda (${UPL}M/${PST}M <= ${PDISK}M)"
      else
        no "upload/post disk kotasini ASIYOR (${UPL}M/${PST}M > ${PDISK}M) - yuklenemez dosya vaadi"
      fi
      [ "$PST" -ge "$UPL" ] && ok "post_max_size >= upload_max_filesize (PHP sarti)" || no "post($PST) < upload($UPL) - PHP hicbir dosya kabul etmez"
    fi
    MM=$(systemctl show "girginos-$SK.slice" -p MemoryMax --value 2>/dev/null)
    if [ -n "$MM" ] && [ "$MM" != "infinity" ] && [ "${PRAM:-0}" -gt 0 ]; then
      MMMB=$((MM/1024/1024))
      if [ "$MMMB" = "$PRAM" ]; then
        ok "cgroup MemoryMax plan ile AYNI (${MMMB}MB = plan ${PRAM}MB)"
      else
        no "cgroup MemoryMax=${MMMB}MB ama plan ${PRAM}MB - plan degisikligi yansimamis"
      fi
    else
      atla "cgroup MemoryMax okunamadi"
    fi
  fi
fi

baslik "C · DNS kaydi silme (tespit #31 regresyonu)"

if [ -n "$DID" ]; then
  RID=$(curl "${H[@]}" -X POST "$API/domains/$DID/dns" \
        -d '{"ad":"e2e","tip":"TXT","deger":"e2e-kanit-degeri","ttl":300,"aktif":true}' | alan id)
  if [ -z "$RID" ]; then
    no "DNS kaydi eklenemedi"
  else
    S1=$(grep -oE '[0-9]{9,10}' "$ZONE" 2>/dev/null | head -1)
    D1=$(dig +short +time=3 +tries=1 @127.0.0.1 "e2e.$DOM" TXT 2>/dev/null)
    [ -n "$D1" ] && ok "kayit CANLI DNS'te cevapliyor ($D1)" || no "eklenen kayit dig ile gorunmuyor"
    curl "${H[@]}" -o /dev/null -X DELETE "$API/domains/$DID/dns/$RID" >/dev/null 2>&1
    sleep 2
    S2=$(grep -oE '[0-9]{9,10}' "$ZONE" 2>/dev/null | head -1)
    N=$(grep -c "e2e-kanit-degeri" "$ZONE" 2>/dev/null)
    D2=$(dig +short +time=3 +tries=1 @127.0.0.1 "e2e.$DOM" TXT 2>/dev/null)
    [ "${N:-1}" = "0" ] && ok "silinen kayit zone DOSYASINDAN dustu" || no "kayit zone dosyasinda DURUYOR (panel sildi der, DNS yayinda)"
    [ -z "$D2" ]        && ok "canli DNS artik cevap VERMIYOR"       || no "canli DNS hala cevapliyor: $D2"
    [ "$S1" != "$S2" ]  && ok "zone serial artti ($S1 -> $S2)"       || no "zone serial DEGISMEDI ($S1) - zone yeniden uretilmemis"
  fi
else
  atla "C blogu (domain yok)"
fi

baslik "D · Yedek + pano sayaci (tespit #25 regresyonu)"

if [ -n "$DID" ]; then
  curl "${H[@]}" -o /dev/null -X POST "$API/domains/$DID/backups" -d '{"tip":"tam"}' >/dev/null 2>&1
  for i in $(seq 1 40); do
    curl "${H[@]}" "$API/domains/$DID/backups/ilerleme" 2>/dev/null | grep -q '"bitti":true' && break
    sleep 3
  done
  DOSYA=$(ls -1 "/var/backups/girginospanel/$SK/" 2>/dev/null | wc -l)
  DBSAT=$(mysql -N -B panel -e "SELECT COUNT(*) FROM backups WHERE domain_id=$DID;" 2>/dev/null)
  SAYAC=$(curl "${H[@]}" "$API/domains/$DID/kaynak" | alan yedek_sayisi)
  [ "${DOSYA:-0}" -gt 0 ] && ok "yedek dosyasi DISKTE ($DOSYA adet)" || no "diskte yedek dosyasi yok"
  [ "${DBSAT:-0}" -gt 0 ] && ok "yedek DB kaydi var ($DBSAT)"        || no "backups tablosunda kayit yok"
  if [ "${SAYAC:-0}" -gt 0 ]; then
    ok "pano sayaci yedegi GORUYOR (yedek_sayisi=$SAYAC)"
  else
    no "pano sayaci 0 - disk/DB dolu ama sayac bos (yutulan sorgu hatasi?)"
  fi
else
  atla "D blogu (domain yok)"
fi

baslik "E · WordPress plan DB kotasi (tespit #7 regresyonu)"

if [ -n "$DID" ]; then
  MAXDB=$(mysql -N -B panel -e "SELECT COALESCE(max_db,0) FROM service_plans WHERE id=1;" 2>/dev/null)
  mysql panel -e "INSERT INTO customers (ad, eposta, plan_id) VALUES ('E2E Kontrol','e2e@kontrol.test',1);" >/dev/null 2>&1
  CID=$(mysql -N -B panel -e "SELECT id FROM customers WHERE eposta='e2e@kontrol.test' LIMIT 1;" 2>/dev/null)
  if [ -n "$CID" ] && [ "${MAXDB:-0}" -gt 0 ]; then
    mysql panel -e "UPDATE domains SET customer_id=$CID, plan_id=1 WHERE id=$DID;" >/dev/null 2>&1
    for n in $(seq 1 "$MAXDB"); do
      mysql panel -e "INSERT IGNORE INTO db_accounts (domain_id, db_name, db_user) VALUES ($DID,'e2e_dolu_$n','e2e_u_$n');" >/dev/null 2>&1
    done
    KOD=$(curl "${H[@]}" -o /tmp/e2e-wp.json -w '%{http_code}' -X POST "$API/domains/$DID/wordpress" \
          -d '{"dizin":"","admin_kullanici":"e2eadmin","admin_email":"e2e@kontrol.test","site_basligi":"E2E"}')
    if [ "$KOD" = "403" ]; then
      ok "kota dolu iken WP kurulumu REDDEDILDI (403)"
    else
      no "WP kurulumu kotayi deldi (HTTP $KOD) - plan max_db=$MAXDB iken fazladan DB acildi"
    fi
    mysql panel -e "DELETE FROM db_accounts WHERE domain_id=$DID AND db_name LIKE 'e2e_dolu_%';" >/dev/null 2>&1
  else
    atla "E blogu (musteri olusturulamadi ya da plan limitsiz)"
  fi
else
  atla "E blogu (domain yok)"
fi

baslik "F · API sozlesmeleri"

SUR=$(curl "${H[@]}" "$API/php/versions")
eol_durum(){ echo "$SUR" | python3 -c "
import json,sys
d=json.load(sys.stdin)
l=d if isinstance(d,list) else (d.get('surumler') or d.get('versions') or [])
m=[x for x in l if str(x.get('surum'))=='$1']
print('YOK' if not m else ('EVET' if m[0].get('eol') else 'HAYIR'))" 2>/dev/null; }
for s in 7.4 8.0 8.1; do
  [ "$(eol_durum $s)" = "EVET" ] && ok "PHP $s eol=true (rozet cikar)" || no "PHP $s eol isaretli DEGIL"
done
[ "$(eol_durum 8.3)" = "HAYIR" ] && ok "PHP 8.3 eol=false (dogru)" || no "PHP 8.3 yanlis EOL isaretli"

ENK=$(mysql -N -B panel -e "SELECT MIN(disk_kota_mb) FROM service_plans WHERE reseller_id=0 AND domain_id IS NULL AND disk_kota_mb>0;" 2>/dev/null)
KUCUK=$(( ${ENK:-1024} / 2 ))
KOD=$(curl "${H[@]}" -o /tmp/e2e-p.json -w '%{http_code}' -X POST "$API/reseller-plans" \
      -d "{\"ad\":\"$PAKET_AD\",\"max_domain\":5,\"max_disk_mb\":$KUCUK,\"max_trafik_mb\":$KUCUK,\"fazla_satis\":false}")
if [ "$KOD" = "400" ]; then
  ok "tek hosting bile acamayacak bayi paketi REDDEDILDI (400)"
else
  no "kullanilamaz bayi paketi kabul edildi (HTTP $KOD) - bayi hosting acamayacak"
fi

YAN=$(curl "${H[@]}" -X POST "$API/resellers" \
      -d '{"kullanici":"e2ebayi","parola":"Aa1!aaaaaaaa","ad_soyad":"E2E","paket_id":1,"reseller_plan_id":0}')
if echo "$YAN" | grep -qi "bilinmeyen alan"; then
  ok "bilinmeyen JSON alani REDDEDILDI (sessizce yutulmuyor)"
else
  no "bilinmeyen alan sessizce yutuldu: $(echo "$YAN" | head -c 140)"
fi

if [ -n "$DID" ]; then
  SIZ=0
  for yol in "../../../etc/passwd" "yok/yok.txt"; do
    C=$(curl "${H[@]}" --get --data-urlencode "yol=$yol" "$API/domains/$DID/files")
    echo "$C" | grep -q "/home/" && SIZ=1
  done
  [ "$SIZ" = "0" ] && ok "dosya yoneticisi mutlak yol sizdirmiyor" || no "dosya yoneticisi /home/... yolunu sizdiriyor"
fi

baslik "G · Servis edilen arayuz paketi"

FE=/opt/girginospanel/frontend-dist/assets
if [ -d "$FE" ]; then
  grep -rqF "guncelleme almiyor" "$FE"/*.js 2>/dev/null || grep -rqF "güncelleme almıyor" "$FE"/*.js 2>/dev/null \
    && ok "EOL rozeti arayuzde" || no "EOL rozeti arayuzde YOK"
  grep -rqF "0.2.0-f1" "$FE"/*.js 2>/dev/null \
    && no "hardcode surum arayuzde duruyor" || ok "hardcode surum yok (surum /healthz'den)"
  grep -rqF "MySQL veritabani ve DNS zone" "$FE"/*.js 2>/dev/null || grep -rqF "MySQL veritabanı ve DNS zone" "$FE"/*.js 2>/dev/null \
    && no "celisen domain modal metni duruyor" || ok "domain modal metni davranisla uyumlu"
  grep -rqF "kritik bir servise ait" "$FE"/*.js 2>/dev/null \
    && ok "korumali port onizlemesi var" || no "korumali port onizlemesi yok"
else
  atla "G blogu (frontend-dist yok)"
fi


baslik "H · Sifreli teslim + lisans zorlamasi"

# --- dagitim: ucretli eklenti pakette/diskte DUZ olmamali
_duz_bulundu=0
for _u in mail calistirici; do
  D="/opt/girginospanel/src/eklentiler/$_u/girginospanel-eklenti-$_u"
  [ -f "$D" ] && { no "$_u DUZ ikilisi diskte: $D"; _duz_bulundu=1; }
done
[ "$_duz_bulundu" = "0" ] && ok "ucretli eklentilerin DUZ ikilisi diskte YOK"

_gosp_var=0
for _u in mail calistirici; do
  G="/opt/girginospanel/src/eklentiler/$_u/girginospanel-eklenti-$_u.gosp"
  if [ -f "$G" ]; then
    _gosp_var=1
    [ "$(head -c 8 "$G")" = "GOSPKT01" ] && ok "$_u sifreli paket (GOSPKT01)" || no "$_u .gosp sihri yanlis"
    head -c 4 "$G" | grep -q ELF && no "$_u .gosp icinde ELF sihri" || ok "$_u .gosp ELF DEGIL (opak)"
  fi
done
[ "$_gosp_var" = "1" ] || atla "H: sifreli paket yok (eski paketten kurulmus olabilir)"

# --- kurulu ucretli eklenti BELLEKTEN mi calisiyor
for _u in mail calistirici; do
  if systemctl is-active --quiet "girginospanel-eklenti-$_u" 2>/dev/null; then
    P=$(systemctl show "girginospanel-eklenti-$_u" -p MainPID --value 2>/dev/null)
    E=$(readlink "/proc/$P/exe" 2>/dev/null)
    case "$E" in
      *memfd*) ok "$_u BELLEKTEN calisiyor ($E)" ;;
      "")      atla "$_u exe okunamadi" ;;
      *)       no "$_u DISKTEN calisiyor: $E (memfd bekleniyordu)" ;;
    esac
    B="/opt/girginospanel/eklentiler/$_u/girginospanel-eklenti-$_u"
    [ -f "$B" ] && no "$_u duz ikilisi calisma dizininde DURUYOR: $B" || ok "$_u calisma dizininde duz ikili yok"
  fi
done

# --- eklenti-kur ucretliyi REDDETMELI
EK=""
for c in /opt/girginospanel/bin/eklenti-kur /usr/local/bin/eklenti-kur; do [ -x "$c" ] && EK="$c"; done
if [ -n "$EK" ]; then
  if "$EK" mail 2>&1 | grep -qi "ÜCRETLİ\|UCRETLI"; then
    ok "eklenti-kur ucretli eklentiyi REDDEDIYOR"
  else
    no "eklenti-kur ucretli eklentiyi kurmaya calisiyor (lisans kapisi yok)"
  fi
else
  atla "eklenti-kur diskte yok"
fi

# --- katalog disi ad ile denetim atlatilamamali
mysql panel -e "INSERT IGNORE INTO cp_eklentiler (ad, etiket, soket, aktif) VALUES ('e2esahte','E2E Sahte','/run/girginospanel/e2e.sock',1);" >/dev/null 2>&1
KOD=$(curl "${H[@]}" -o /tmp/e2e-sahte.json -w '%{http_code}' "$API/eklenti/e2esahte/")
if [ "$KOD" = "402" ] && grep -qi "tanınmayan\|taninmayan" /tmp/e2e-sahte.json; then
  ok "katalog disi eklenti adi REDDEDILDI (fail-closed)"
else
  no "katalog disi ad ile denetim atlatilabiliyor (HTTP $KOD)"
fi
mysql panel -e "DELETE FROM cp_eklentiler WHERE ad='e2esahte';" >/dev/null 2>&1
temizle
echo
echo "===================================================="
printf " SONUC: %d gecti - %d dustu - %d atlandi\n" "$gecti" "$kaldi" "$atlandi"
if [ "$kaldi" -gt 0 ]; then
  echo "----------------------------------------------------"
  for f in "${DUSENLER[@]}"; do echo "  X $f"; done
fi
echo "===================================================="
[ "$kaldi" -eq 0 ]
