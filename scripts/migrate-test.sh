#!/usr/bin/env bash
# migrate-test.sh — gPanel SCHEMA-MIGRATION test kapisi
# =====================================================================
# NE YAPAR
#   1) GECICI (scratch) bir DB olusturur: gpanel_migtest_<pid>_<ts>
#   2) migrations/*.sql dosyalarini, uretim runner'i (cmd/server/main.go
#      -> runMigrations) ile AYNI sirada (byte-wise dosya-adi sirasi,
#      Go os.ReadDir ile ozdes) SIRALI uygular.
#   3) GECIS 1 (temiz kurulum): bos scratch DB uzerinde tumunu uygular.
#      Bir "gercek" hata (asagidaki zararsiz kume DISINDA) varsa -> FAIL,
#      hangi migration oldugu bildirilir.
#   4) GECIS 2 (idempotency): AYNI seti TEKRAR uygular. Ikinci gecis
#      "gercek" hata uretmemeli. Uretirse -> FAIL (ledger'siz startup
#      re-run guvenligini bozar; bu bir BUG'dir). Yalniz "zararsiz"
#      (duplicate column/key, already exists, duplicate entry) hatalar
#      -> uretimde runner bunlari yutar + ledger zaten tek-sefer kosar,
#      bu yuzden RAPORLANIR ama BLOKLAMAZ.
#   5) Scratch DB her cikista (basari/hata/sinyal) DUSURULUR.
#
# GUVENLIK — CANLI 'panel' DB'sine ASLA DOKUNULMAZ
#   * Tum ifadeler yalniz scratch DB baglaminda kosar (mysql <scratch>).
#   * 0001_init.sql "CREATE DATABASE panel" + "USE panel" icerir; bunlar
#     scratch testinde canliya yonlendirir -> filtre ile NOTRLESTIRILIR
#     ve filtreden sonra hala USE/CREATE DATABASE varsa HARD-ABORT.
#   * DROP yalniz "gpanel_migtest_" onekli ada uygulanir.
#
# Uretim runner'i ile ozdeslik notlari
#   * Sira: byte-wise (LC_ALL=C sort) == Go string "<" == os.ReadDir sirasi.
#   * Deyim ayirma: migration'larda DELIMITER/trigger/procedure/function ve
#     ; iceren blok-yorum YOK; bu yuzden "mysql < dosya" runner'in naif
#     ";" bolmesiyle ozdes davranir.
#   * Zararsiz hata kumesi runner (cmd/server/main.go) ile BIREBIR ayni.
#
# Baglanti (hem 181 socket-root hem CI TCP calisir):
#   Env: DB_USER(=root) DB_PASS DB_HOST DB_PORT DB_SOCK
#   181  : env yok -> `mysql -u root` (unix_socket auth, sifresiz)
#   CI   : DB_HOST=127.0.0.1 DB_PORT=3306 DB_USER=root DB_PASS=root
#
# Cikis kodlari: 0=basari  1=kapi hatasi  2=ortam hatasi  3=guvenlik durdurma
# =====================================================================
set -uo pipefail

# --- konum / migrations dizini ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGDIR="${1:-${MIGDIR:-$(cd "$SCRIPT_DIR/.." && pwd)/migrations}}"

# --- mysql baglanti argumanlari ---
DB_USER="${DB_USER:-root}"
MYSQL_ARGS=( --default-character-set=utf8mb4 -u "$DB_USER" )
[ -n "${DB_PASS:-}" ] && MYSQL_ARGS+=( "-p${DB_PASS}" )
[ -n "${DB_HOST:-}" ] && MYSQL_ARGS+=( -h "$DB_HOST" --protocol=TCP )
[ -n "${DB_PORT:-}" ] && MYSQL_ARGS+=( -P "$DB_PORT" )
[ -n "${DB_SOCK:-}" ] && MYSQL_ARGS+=( -S "$DB_SOCK" )
msql() { mysql "${MYSQL_ARGS[@]}" "$@"; }

# --- scratch DB adi + guard ---
SCRATCH="gpanel_migtest_$$_$(date +%s)"
case "$SCRATCH" in gpanel_migtest_*) : ;; *)
  echo "GUVENLIK: scratch adi beklenen desende degil: $SCRATCH" >&2; exit 3;; esac
for banned in panel mysql information_schema performance_schema sys mailserver; do
  [ "$SCRATCH" = "$banned" ] && { echo "GUVENLIK: yasak DB adi: $SCRATCH" >&2; exit 3; }
done

# --- cleanup: scratch DB'yi her cikista dusur (yalniz guvenli onekte) ---
cleanup() {
  case "$SCRATCH" in gpanel_migtest_*)
    if msql -e "DROP DATABASE IF EXISTS \`$SCRATCH\`;" 2>/dev/null; then
      echo "temizlik: scratch DB dusuruldu ($SCRATCH)"
    else
      echo "UYARI: scratch DB dusurulemedi -> elle: DROP DATABASE \`$SCRATCH\`;" >&2
    fi ;;
  esac
}
trap cleanup EXIT INT TERM

# --- on kontroller ---
[ -d "$MIGDIR" ] || { echo "HATA: migrations dizini yok: $MIGDIR" >&2; exit 2; }
# DB hazir mi (CI service container icin kisa bekleme)
ready=0
for i in $(seq 1 30); do
  if msql -e "SELECT 1;" >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
[ "$ready" = 1 ] || { echo "HATA: mysql baglantisi kurulamadi (30s)" >&2; exit 2; }

echo "== gPanel migration test =="
echo "migrations : $MIGDIR"
echo "scratch DB : $SCRATCH   (canli 'panel' DB'sine DOKUNULMAZ)"

# --- dosya listesi: uretim os.ReadDir ile AYNI sira (byte-wise) ---
mapfile -t FILES < <(cd "$MIGDIR" && ls -1 ./*.sql 2>/dev/null | sed 's#^\./##' | LC_ALL=C sort)
[ "${#FILES[@]}" -gt 0 ] || { echo "HATA: .sql migration bulunamadi: $MIGDIR" >&2; exit 2; }
echo "toplam migration dosyasi: ${#FILES[@]}"

# --- scratch DB olustur (uretim ile ayni charset/collation) ---
msql -e "DROP DATABASE IF EXISTS \`$SCRATCH\`;
         CREATE DATABASE \`$SCRATCH\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;" \
  || { echo "HATA: scratch DB olusturulamadi" >&2; exit 2; }

# --- filtre: 'USE ...;' ve 'CREATE DATABASE ...;' ifadelerini cikar ---
#     (0001_init.sql'deki 'USE panel' canliya yonlendirmesin diye)
sanitize() {
  awk '
    BEGIN{skip=0}
    {
      if (skip) { if ($0 ~ /;/) skip=0; next }      # coklu satirli ifadeyi atla
      s=$0; sub(/^[ \t]+/,"",s); U=toupper(s)
      if (U ~ /^CREATE[ \t]+DATABASE/ || U ~ /^USE[ \t]/) {
        if ($0 !~ /;/) skip=1                        # ; sonraki satirda ise atlamaya devam
        next
      }
      print
    }'
}

# --- runner ile BIREBIR ayni "zararsiz" siniflandirma ---
is_harmless() { # $1 = kucuk-harfli hata metni
  case "$1" in
    *"duplicate column"*|*"duplicate key"*|*"already exists"*|*"duplicate entry"*) return 0;;
    *) return 1;;
  esac
}

# --- bir gecisi kosar; sonuclari global degiskenlere yazar ---
TOTAL_REAL=0; TOTAL_HARM=0
declare -a REAL_LINES=() NONIDEM_FILES=() REAL_FILES=()
run_pass() {
  TOTAL_REAL=0; TOTAL_HARM=0; REAL_LINES=(); NONIDEM_FILES=(); REAL_FILES=()
  local f filtered err elines line low real harm
  for f in "${FILES[@]}"; do
    filtered="$(sanitize < "$MIGDIR/$f")"
    # GUVENLIK gate: filtreden sonra USE/CREATE DATABASE ASLA kalmamali
    if printf '%s\n' "$filtered" | grep -qiE '^[[:space:]]*(USE[[:space:]]|CREATE[[:space:]]+DATABASE)'; then
      echo "GUVENLIK: $f -> USE/CREATE DATABASE notrlestirilemedi, DURDU" >&2; exit 3
    fi
    # --force: her deyimi dene (runner da her deyimde devam eder); yalniz stderr yakala
    err="$(printf '%s\n' "$filtered" | msql --force "$SCRATCH" 2>&1 >/dev/null)"
    elines="$(printf '%s\n' "$err" | grep -E '^ERROR ' || true)"
    real=0; harm=0
    if [ -n "$elines" ]; then
      while IFS= read -r line; do
        [ -n "$line" ] || continue
        low="$(printf '%s' "$line" | tr 'A-Z' 'a-z')"
        if is_harmless "$low"; then harm=$((harm+1))
        else real=$((real+1)); REAL_LINES+=("$f: $line"); fi
      done <<< "$elines"
    fi
    TOTAL_REAL=$((TOTAL_REAL+real)); TOTAL_HARM=$((TOTAL_HARM+harm))
    [ "$real" -gt 0 ] && REAL_FILES+=("$f")
    [ "$harm" -gt 0 ] && [ "$real" -eq 0 ] && NONIDEM_FILES+=("$f")
    if [ "$real" -gt 0 ] || [ "$harm" -gt 0 ]; then
      printf '  %-44s %d gercek / %d zararsiz\n' "$f" "$real" "$harm"
    fi
  done
}

# ================= GECIS 1: temiz kurulum =================
echo; echo "--- GECIS 1: temiz kurulum (bos scratch DB) ---"
run_pass
P1_REAL=$TOTAL_REAL; P1_HARM=$TOTAL_HARM
declare -a P1_REAL_LINES=(); [ "${#REAL_LINES[@]}" -gt 0 ] && P1_REAL_LINES=("${REAL_LINES[@]}")
[ "$P1_REAL" -eq 0 ] && [ "$P1_HARM" -eq 0 ] && echo "  (tum dosyalar temiz uygulandi)"
echo "GECIS 1: ${#FILES[@]} dosya uygulandi, ${P1_REAL} gercek hata, ${P1_HARM} zararsiz hata"

# ================= GECIS 2: idempotency =================
echo; echo "--- GECIS 2: idempotency (ayni set TEKRAR) ---"
run_pass
P2_REAL=$TOTAL_REAL; P2_HARM=$TOTAL_HARM
declare -a P2_REAL_LINES=(); [ "${#REAL_LINES[@]}" -gt 0 ] && P2_REAL_LINES=("${REAL_LINES[@]}")
declare -a P2_NONIDEM=();   [ "${#NONIDEM_FILES[@]}" -gt 0 ] && P2_NONIDEM=("${NONIDEM_FILES[@]}")
declare -a P2_REALF=();     [ "${#REAL_FILES[@]}" -gt 0 ] && P2_REALF=("${REAL_FILES[@]}")
[ "$P2_REAL" -eq 0 ] && [ "$P2_HARM" -eq 0 ] && echo "  (ikinci gecis TAMAMEN hatasiz -> tum set strict-idempotent)"
echo "GECIS 2: ${P2_REAL} gercek hata, ${P2_HARM} zararsiz hata, ${#P2_NONIDEM[@]} dosya idempotent-degil-ama-runner-guvenli"

# ================= SONUC =================
echo; echo "===== SONUC ====="
FAIL=0

if [ "$P1_REAL" -gt 0 ]; then
  FAIL=1
  echo "[BASARISIZ] Temiz kurulum: ${P1_REAL} GERCEK hata (kirik/sirasiz migration):"
  for l in "${P1_REAL_LINES[@]}"; do echo "    - $l"; done
else
  echo "[TEMIZ] Temiz kurulum: ${#FILES[@]} migration hatasiz uygulandi"
  [ "$P1_HARM" -gt 0 ] && echo "         (not: ilk geciste ${P1_HARM} zararsiz hata -> set-ici mukerrer nesne olabilir)"
fi

if [ "$P2_REAL" -gt 0 ]; then
  FAIL=1
  echo "[BASARISIZ] Idempotency: re-run'da ${P2_REAL} GERCEK hata -> ledger'siz startup re-run'i BOZAR (BUG):"
  for l in "${P2_REAL_LINES[@]}"; do echo "    - $l"; done
else
  echo "[GECER] Idempotency: re-run'da GERCEK hata YOK"
fi

if [ "${#P2_NONIDEM[@]}" -gt 0 ]; then
  echo "[BILGI] Strict-idempotent OLMAYAN ama runner-guvenli dosyalar (${#P2_NONIDEM[@]}) —"
  echo "        uretimde ledger tek-sefer kosar; ledger'siz fallback'te runner bu hatalari yutar:"
  for f in "${P2_NONIDEM[@]}"; do echo "    - $f"; done
fi

echo
if [ "$FAIL" -eq 0 ]; then
  echo "SONUC: BASARILI — temiz kurulum + idempotency (gercek hata yok)."
  exit 0
else
  echo "SONUC: BASARISIZ — yukaridaki GERCEK hatalari giderin."
  exit 1
fi
