#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""kos.py — API sözleşme test takımını TEST panelinde koşar (root olarak, panel sunucusunun kendisinde).

    python3 kos.py                      # bütün modüller
    python3 kos.py --modul sso,bayi     # yalnız seçilenler
    python3 kos.py --json /root/sozlesme-son.json
    python3 kos.py --yalniz-temizle     # önceki yarım koşulardan kalan önekli nesneleri sil

Çıktı: satır satır OK/FAIL/ATLA + sonda özet; --json ile ajan raporlarıyla aynı biçimde JSON
(testler, bulgular, gözlemler = hata yanıtları [belge karşılaştırması için], temizlik/kalıntı).
Çıkış kodu: 0 = hepsi geçti, 1 = en az bir FAIL, 2 = koşu kurulamadı.
"""
import argparse
import json
import os
import sys
import traceback

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import gosp  # noqa: E402
import moduller  # noqa: E402


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--modul", default="", help="virgüllü modül listesi: " + ",".join(a for a, _ in moduller.MODULLER) + " (ağır modüller yalnız açıkça seçilince koşar: " + ",".join(sorted(moduller.AGIR)) + ")")
    ap.add_argument("--json", default="", help="sonuç JSON dosyası")
    ap.add_argument("--onek", default=gosp.ONEK)
    ap.add_argument("--yalniz-temizle", action="store_true")
    ap.add_argument("--temizleme", action="store_true", help="sonda temizleme (hata ayıklama için nesneler kalsın)")
    a = ap.parse_args()
    if os.geteuid() != 0:
        print("root olarak çalıştırın (panel DB'sine geçici anahtar eklenir)")
        return 2
    if a.yalniz_temizle:
        t = gosp.temizle(a.onek)
        print(json.dumps(t, ensure_ascii=False, indent=1))
        return 0 if not t["hatalar"] and not t["kalinti"] else 1

    ortam = gosp.ortam()
    print("panel %s (ikili %s)" % (ortam["surum"], ortam["ikili_sha16"]))
    # Önceki yarım koşudan kalanları önce topla: sonuçlar temiz bir başlangıçtan ölçülür.
    on = gosp.temizle(a.onek)
    if on["silinen_hosting"] or on["kalinti"]:
        print("önceki koşudan kalan temizlendi: %s" % json.dumps(on, ensure_ascii=False))
    try:
        ctx = gosp.Baglam(a.onek)
    except Exception as e:  # noqa: BLE001
        print("koşu kurulamadı: %s" % gosp.maskele(e))
        return 2
    secilen = [s for s in a.modul.split(",") if s] or [ad for ad, _ in moduller.MODULLER if ad not in moduller.AGIR]
    for ad, fn in moduller.MODULLER:
        if ad not in secilen:
            continue
        try:
            fn(ctx)
        except Exception as e:  # noqa: BLE001 — modül çökmesi rapora düşer, koşu sürer
            ctx.r.bolum(ad)
            ctx.r.kontrol("%s modülü tamamlandı" % ad, False, "%s: %s" % (type(e).__name__, e))
            ctx.r.bulgu("yuksek", "%s modülü çöktü" % ad, gosp.maskele(traceback.format_exc()[-600:]))
    temizlik = {}
    if not a.temizleme:
        ctx.r.bolum("temizlik")
        temizlik = gosp.temizle(a.onek, p=ctx.p)
        ctx.r.kontrol("test nesneleri silindi (hata yok)", not temizlik["hatalar"], "; ".join(temizlik["hatalar"]))
        ctx.r.kontrol("kalıntı yok (DB, sistem kullanıcısı, home, nginx)", not temizlik["kalinti"], json.dumps(temizlik["kalinti"]))
    oz = ctx.r.ozet()
    print("\nSONUÇ: %d OK, %d FAIL, %d ATLA (%d sn)" % (oz["gecti"], oz["dustu"], oz["atlandi"], ctx.r.json(ortam, [], {})["sure_sn"]))
    if a.json:
        fd = os.open(a.json, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w") as f:
            json.dump(ctx.r.json(ortam, ctx.p.gozlemler, temizlik), f, ensure_ascii=False, indent=1)
    return 1 if oz["dustu"] else 0


if __name__ == "__main__":
    sys.exit(main())
