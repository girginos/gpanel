#!/bin/bash
# sitekopya-kalinti-temizle.sh — KALDIRILAN "Site kopyası" özelliğinin kalıntısı olan
# /home/c_*/kopyalar dizinlerini temizler. Kurulum yeri: /root/sitekopya-kalinti-temizle.sh
#
#   (argümansız)  DRY-RUN: her hesap için boyut / dosya sayısı / yaş listelenir, hiçbir şey silinmez.
#   --uygula      Güvenli koşulları sağlayan dizinleri siler.
#
# Güvenlik: dizin ADI tam "kopyalar" olmalı ve gerçek dizin (symlink DEĞİL); realpath
# /home/c_<ad>/kopyalar olmalı; ilk seviye girdilerin HEPSİ kopya_YYYYMMDD_HHMMSS olmalı ve
# gerçek dizin olmalı (symlink/başka ad → hesap ATLANIR ve raporlanır). Silme: find -xdev
# (dosya sistemi dışına çıkmaz, symlink izlemez) ile; rm -rf kullanılmaz.
set -u
export LC_ALL=C
UYGULA=0
[ "${1:-}" = "--uygula" ] && UYGULA=1
[ "$(id -u)" -eq 0 ] || { echo "root gerekli" >&2; exit 2; }
DESEN='^kopya_[0-9]{8}_[0-9]{6}$'
simdi=$(date +%s)
atlanan=0; temiz=0; toplam_b=0
for d in /home/c_*/kopyalar; do
  [ -e "$d" ] || [ -L "$d" ] || continue
  hesap=$(basename "$(dirname "$d")")
  if [ -L "$d" ] || [ ! -d "$d" ]; then echo "ATLA $hesap: kopyalar symlink/dizin degil"; atlanan=$((atlanan+1)); continue; fi
  gercek=$(realpath -e -- "$d") || { echo "ATLA $hesap: realpath hatasi"; atlanan=$((atlanan+1)); continue; }
  home=$(realpath -e -- "/home/$hesap") || { atlanan=$((atlanan+1)); continue; }
  if [ "$gercek" != "$home/kopyalar" ] || [ "$(dirname "$gercek")" != "$home" ] || [ "$(dirname "$home")" != "/home" ]; then
    echo "ATLA $hesap: realpath ev disina cikiyor ($gercek)"; atlanan=$((atlanan+1)); continue
  fi
  uygun=1; n=0; enyeni=0
  while IFS= read -r -d '' e; do
    ad=${e##*/}; n=$((n+1))
    if [ -L "$e" ] || [ ! -d "$e" ] || ! [[ $ad =~ $DESEN ]]; then
      echo "ATLA $hesap: beklenmeyen girdi '$ad'"; uygun=0; break
    fi
    m=$(stat -c %Y -- "$e"); [ "$m" -gt "$enyeni" ] && enyeni=$m
  done < <(find "$gercek" -mindepth 1 -maxdepth 1 -print0)
  [ "$uygun" -eq 1 ] || { atlanan=$((atlanan+1)); continue; }
  boyut=$(du -sxb -- "$gercek" 2>/dev/null | cut -f1); boyut=${boyut:-0}
  dosya=$(find "$gercek" -xdev -type f 2>/dev/null | wc -l)
  yas="-"; [ "$enyeni" -gt 0 ] && yas="$(( (simdi-enyeni)/86400 )) gun"
  printf '%-6s %s: %d kopya, %d dosya, %d MB, en yeni %s\n' "$([ $UYGULA -eq 1 ] && echo SIL || echo KALINTI)" "$hesap" "$n" "$dosya" "$((boyut/1048576))" "$yas"
  toplam_b=$((toplam_b+boyut)); temiz=$((temiz+1))
  if [ "$UYGULA" -eq 1 ]; then
    find "$gercek" -xdev -depth -mindepth 1 \( -type f -o -type l -o -type d \) -delete 2>&1 | head -5
    rmdir -- "$gercek" 2>&1 | head -2
    [ -e "$gercek" ] && echo "UYARI $hesap: tam silinemedi"
  fi
done
echo "--- ozet: temizlenebilir=$temiz atlanan=$atlanan toplam=$((toplam_b/1048576)) MB mod=$([ $UYGULA -eq 1 ] && echo UYGULA || echo DRY-RUN)"
