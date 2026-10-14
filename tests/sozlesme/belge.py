#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""belge.py — belge ↔ gerçek kayma denetimi.

Sözleşme takımının kaydettiği hata yanıtlarını (kos.py --json → "gozlemler": uç deseni, durum, hata
iletisi) belge sitesinin API sayfalarındaki "Olası hatalar" tablolarıyla karşılaştırır:

  - BELGESİZ: gerçek panelin verdiği (durum, ileti) çifti, o ucun belge bölümünde ve genel hata
    sayfasında (hatalar.md) yok → belge eksik ya da panel iletisi değişmiş.
  - BİLİNMEYEN UÇ: gözlenen uç belge sayfalarında hiç geçmiyor.

    python3 belge.py --json /root/sozlesme-son.json --belge /root/girginos-docs/icerik/tr/api

İletilerdeki sayılar karşılaştırmada '#' sayılır ("(4/4)" ≈ "(2/1)"); belgedeki "…" joker karakterdir.
Çıkış kodu: 0 = kayma yok, 1 = belgesiz yanıt var.
"""
import argparse
import json
import os
import re
import sys

UC = re.compile(r"^`(GET|POST|PUT|DELETE|PATCH) (/api/v1)?(/[^`?]*)`\s*$")
SATIR = re.compile(r"^\|\s*(\d{3})\s*\|(.*)$")
TIRNAK = re.compile(r"`([^`]+)`")
JSON_HATA = re.compile(r'"hata":\s*"([^"]+)"')
# API anahtarı ucu olmayan, panel giriş formunun uçları (belge sitesinde API sayfası yok).
HARIC = {("POST", "/auth/login"), ("POST", "/musteri/login")}


def desen(yol):
    """'/resellers/{id}/sso' → '/resellers/{}/sso' (parametre adları önemsiz)."""
    return re.sub(r"\{[^}]*\}", "{}", yol.rstrip("/"))


def norm(s):
    s = re.sub(r"\d+", "#", s.strip())
    return s


def eslesir(gercek, belgedeki):
    g, b = norm(gercek), norm(belgedeki)
    if g == b:
        return True
    if "…" in b:
        parcalar = [re.escape(p) for p in b.split("…")]
        return re.fullmatch(".*".join(parcalar), g, re.S) is not None
    return False


def belge_oku(dizin):
    """{(METOD, desen): [(durum, ileti)]} ve genel hatalar listesi."""
    uclar, genel = {}, []
    for ad in sorted(os.listdir(dizin)):
        if not ad.endswith(".md"):
            continue
        simdiki = None
        for satir in open(os.path.join(dizin, ad), encoding="utf-8"):
            satir = satir.rstrip("\n")
            m = UC.match(satir)
            if m:
                simdiki = (m.group(1), desen(m.group(3)))
                uclar.setdefault(simdiki, [])
                continue
            if satir.startswith("## "):
                simdiki = None if not ad.startswith(("hatalar", "errors")) else ("*", "*")
            m = SATIR.match(satir)
            if m:
                # satırdaki BÜTÜN `…` iletileri (açıklama sütunundaki "Diğer iletiler: `…`" dahil)
                for ileti in TIRNAK.findall(m.group(2)):
                    cift = (int(m.group(1)), ileti)
                    if ad.startswith(("hatalar", "errors")) or ad == "index.md":
                        genel.append(cift)
                    if simdiki and simdiki != ("*", "*"):
                        uclar[simdiki].append(cift)
            # genel sayfalardaki JSON örnekleri ("hata": "…"): durum kodu belirsiz → her durumda geçerli
            if ad.startswith(("hatalar", "errors")) or ad == "index.md":
                for ileti in JSON_HATA.findall(satir):
                    genel.append((0, ileti))
    return uclar, genel


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--json", required=True)
    ap.add_argument("--belge", required=True, help="belge API sayfaları dizini (ör. icerik/tr/api)")
    a = ap.parse_args()
    j = json.load(open(a.json, encoding="utf-8"))
    uclar, genel = belge_oku(a.belge)
    belgesiz, bilinmeyen, eslesen = [], set(), 0
    gorulen = set()
    for g in j.get("gozlemler", []):
        if not g.get("hata"):
            continue  # düz metin/boş gövde (404/405) — genel kural, belgede ayrıca anlatılır
        metod, _, yol = g["desen"].partition(" ")
        anahtar = (metod, desen(yol))
        if anahtar in HARIC:
            continue
        tekil = (anahtar, g["kod"], g["hata"])
        if tekil in gorulen:
            continue
        gorulen.add(tekil)
        if anahtar not in uclar:
            bilinmeyen.add("%s %s" % anahtar)
            continue
        adaylar = uclar[anahtar] + genel
        if any(d in (g["kod"], 0) and eslesir(g["hata"], i) for d, i in adaylar):
            eslesen += 1
        else:
            belgesiz.append("%s %s → %d %s" % (metod, yol, g["kod"], g["hata"]))
    print("belgeyle eşleşen hata yanıtı: %d" % eslesen)
    for b in belgesiz:
        print("  BELGESİZ  " + b)
    for b in sorted(bilinmeyen):
        print("  BİLİNMEYEN UÇ  " + b)
    print("SONUÇ: %d belgesiz, %d bilinmeyen uç" % (len(belgesiz), len(bilinmeyen)))
    return 1 if belgesiz else 0


if __name__ == "__main__":
    sys.exit(main())
