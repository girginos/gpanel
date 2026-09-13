#!/usr/bin/env bash
# gpanel GRANT joker onarimi — MEVCUT kurulumlar (2026-09-13)
# Eski kacissiz "GRANT ALL ON `db`.*" grant'lerinde db adindaki _ ve %
# LIKE-joker gibi davranip cross-tenant erisim aciyordu (canli kanitlandi).
# Kod fix'i (hesaplar.GrantDBKac) yalniz YENI grant'leri kacisliyor; bu
# script MEVCUT eski grant'leri REVOKE edip kacisli haliyle yeniden GRANT
# eder. Guvenli: yalniz c_*_db ve wpu_* kullanicilar; sistem/root'a dokunmaz.
# Idempotent: zaten kacisli (Db'de ters-boluk olan) satirlari atlar.
# Kullanim: bash grant-onarim.sh [--dry]
set -uo pipefail

DRY=0
[ "${1:-}" = "--dry" ] && DRY=1

# Tek ters-boluk karakteri. Kaynakta cift-escape ("\\") YOK — oktal '\134'
# ile uretilir; dosya transferi bunu bozarsa diye CALISMADAN once dogrulanir
# (fail-closed): uretilen deger 1 karakter ve kodu 92 (ters-boluk) degilse cik.
bs=$(printf '\134')
code=$(printf '%d' "'$bs" 2>/dev/null)
if [ "${#bs}" -ne 1 ] || [ "$code" -ne 92 ]; then
  echo "HATA: ters-boluk uretilemedi (dosya transferi bozmus olabilir) — GUVENLI CIKIS"
  exit 1
fi

onarilan=0
atlanan=0
hata=0

while IFS=$'\t' read -r user db; do
  [ -z "${user:-}" ] && continue
  [ -z "${db:-}" ] && continue
  # Zaten kacisli mi? Db degerinde ters-boluk varsa literal (guvenli) — atla.
  case "$db" in
    *"$bs"*) atlanan=$((atlanan+1)); continue ;;
  esac
  db_kacisli="${db//_/${bs}_}"
  db_kacisli="${db_kacisli//%/${bs}%}"
  if [ "$db_kacisli" = "$db" ]; then
    atlanan=$((atlanan+1)); continue   # _ veya % yok -> joker riski yok
  fi
  if [ "$DRY" = "1" ]; then
    printf '  [DRY] %s @ %s  ->  GRANT(%s)\n' "$user" "$db" "$db_kacisli"
    continue
  fi
  if mysql -e "REVOKE ALL PRIVILEGES ON \`$db\`.* FROM '$user'@'localhost';
GRANT ALL PRIVILEGES ON \`$db_kacisli\`.* TO '$user'@'localhost';" 2>/dev/null; then
    printf '  ONARILDI: %s @ %s\n' "$user" "$db"
    onarilan=$((onarilan+1))
  else
    printf '  HATA: %s @ %s\n' "$user" "$db"
    hata=$((hata+1))
  fi
done < <(mysql -N -B -r -e "SELECT User, Db FROM mysql.db
  WHERE (Db LIKE '%|_%' ESCAPE '|' OR Db LIKE '%|%%' ESCAPE '|')
    AND (User LIKE 'c|_%|_db' ESCAPE '|' OR User LIKE 'wpu|_%' ESCAPE '|');" 2>/dev/null)

if [ "$DRY" = "0" ]; then
  mysql -e "FLUSH PRIVILEGES;" 2>/dev/null
fi
printf 'SONUC: onarilan=%d atlanan=%d hata=%d (dry=%d)\n' "$onarilan" "$atlanan" "$hata" "$DRY"
