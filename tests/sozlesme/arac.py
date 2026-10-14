#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""arac.py — sözleşme kütüphanesinin tek-adım komut satırı ucu (MCP vps_* araçları bunu çağırır).

Keşif yapan bir ajan ham ssh/curl çıktısı yerine KISA JSON alır; sırlar (anahtar, JWT, parola,
tek kullanımlık kod) hiçbir zaman çıktıya yazılmaz, adla anılır. Durum (oluşturulan anahtarlar,
bayiler, hostingler — yalnız test önekiyle) /root/.sozlesme-durum.json (0600) dosyasında tutulur.

Komutlar (hepsi JSON yazar):
  durum
  anahtar  <ad> --sahip kok|bayi:<etiket> --izin a:yaz,b:oku [--ip CIDR,..] [--hiz N]
  bayi     <etiket>                         # paketli test bayisi, parolası bilinen
  hosting  <etiket> [--sahip <bayi-etiketi>] [--govde JSON]
  api      --kim KIM --kapi admin|bayi|musteri --metod M --yol /yol [--govde JSON] [--neg] [--uzun N]
           KIM: kok | anahtar:<ad> | bayi:<etiket> (parolalı oturum) | sso:<etiket> | musteri:<etiket> | jwt-admin | yok
           yolda {h:<etiket>} / {b:<etiket>} → kimlik
  giris    --kim bayi:<etiket>|musteri:<etiket> [--parola-yanlis] [--neg]
  site     <alan|h:etiket>
  sso      --hedef h:<etiket>|b:<etiket> [--tuket] [--tuketim-kapisi admin|bayi|musteri] [--neg]
  temizle
  kos      [--modul a,b]                    # bütün takımı arka planda başlatır
  kos-durum
"""
import argparse
import json
import os
import re
import subprocess
import sys
import urllib.parse

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import gosp  # noqa: E402

DURUM = "/root/.sozlesme-durum.json"
KOS_LOG = "/root/sozlesme-kos.log"
KOS_JSON = "/root/sozlesme-son.json"
# Keşif öneki takımınkinden (sozl) AYRI: takım koşusu başlarken kendi önekini temizler, ajanın
# yarım keşfini silmez.
ONEK = os.environ.get("GOSP_ARAC_ONEK", "sozk")


def yukle():
    try:
        d = json.load(open(DURUM))
    except (OSError, ValueError):
        d = {}
    for k in ("anahtar", "bayi", "hosting", "sso"):
        d.setdefault(k, {})
    d.setdefault("kosu", gosp.rastgele(4))
    for v in d["anahtar"].values():
        gosp.sir_kaydet(v.split(":", 1)[-1])
    for b in d["bayi"].values():
        gosp.sir_kaydet(b.get("parola"))
    for h in d["hosting"].values():
        gosp.sir_kaydet(h.get("parola"))
    for t in d["sso"].values():
        gosp.sir_kaydet(t)
    return d


def kaydet(d):
    fd = os.open(DURUM, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(d, f)


def cik(o, kod=0):
    print(gosp.maskele(json.dumps(o, ensure_ascii=False)))
    sys.exit(kod)


def kok(d, p):
    if "kok" not in d["anahtar"]:
        d["anahtar"]["kok"] = gosp.Anahtarlar(ONEK).ac("arac-kok", "root", "admin", gosp.TUM_KOK)
        kaydet(d)
    return d["anahtar"]["kok"]


def yol_coz(d, yol):
    def f(m):
        tur, et = m.group(1), m.group(2)
        kay = d["hosting" if tur == "h" else "bayi"].get(et)
        if not kay:
            cik({"hata": "bilinmeyen %s etiketi: %s" % (tur, et)}, 2)
        return str(kay["id"])
    return re.sub(r"\{(h|b):([a-z0-9]+)\}", f, yol)


def bayi_jwt(d, p, et, parola=None, kaynak="127.0.0.1"):
    b = d["bayi"][et]
    y = p.istek("bayi", "POST", "/auth/login", {"kullanici": b["kul"], "parola": parola or b["parola"]}, kaynak=kaynak)
    tok = y.alan("token", varsayilan="")
    gosp.sir_kaydet(tok)
    return y, ("Bearer " + tok if tok else None)


def musteri_jwt(d, p, et, parola=None, kaynak="127.0.0.1"):
    h = d["hosting"][et]
    y = p.istek("musteri", "POST", "/musteri/login", {"kullanici": h["sk"], "parola": parola or h["parola"]}, kaynak=kaynak)
    tok = y.alan("token", varsayilan="")
    gosp.sir_kaydet(tok)
    return y, ("Bearer " + tok if tok else None)


def kim_coz(d, p, kim):
    if kim == "yok":
        return None
    if kim == "kok":
        return kok(d, p)
    if kim == "jwt-admin":
        return gosp.admin_jwt()
    tur, _, ad = kim.partition(":")
    if tur == "anahtar":
        return d["anahtar"].get(ad) or cik({"hata": "bilinmeyen anahtar: " + ad}, 2)
    if tur == "bayi":
        return bayi_jwt(d, p, ad)[1] or cik({"hata": "bayi oturumu açılamadı: " + ad}, 2)
    if tur == "musteri":
        return musteri_jwt(d, p, ad)[1] or cik({"hata": "müşteri oturumu açılamadı: " + ad}, 2)
    if tur == "sso":
        t = d["sso"].get(ad)
        return ("Bearer " + t) if t else cik({"hata": "SSO oturumu yok: " + ad}, 2)
    cik({"hata": "bilinmeyen kim: " + kim}, 2)


def yanit_ozet(y, uzun=400):
    o = {"kod": y.kod}
    if y.hata:
        o["hata"] = y.hata
    if y.hata_kodu:
        o["hata_kodu"] = y.hata_kodu
    if y.json is None and y.govde:
        o["duz_metin"] = True
    o["govde"] = gosp.kisalt(y.govde, uzun)
    for b in ("retry-after", "cache-control", "x-ratelimit-remaining"):
        if b in y.basliklar:
            o[b] = y.basliklar[b]
    return o


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("komut")
    ap.add_argument("ad", nargs="?")
    ap.add_argument("--sahip", default="")
    ap.add_argument("--izin", default="")
    ap.add_argument("--ip", default="127.0.0.0/8,::1")
    ap.add_argument("--hiz", type=int, default=6000)
    ap.add_argument("--govde", default="")
    ap.add_argument("--kim", default="kok")
    ap.add_argument("--kapi", default="admin")
    ap.add_argument("--metod", default="GET")
    ap.add_argument("--yol", default="")
    ap.add_argument("--neg", action="store_true")
    ap.add_argument("--uzun", type=int, default=400)
    ap.add_argument("--parola-yanlis", action="store_true")
    ap.add_argument("--hedef", default="")
    ap.add_argument("--tuket", action="store_true")
    ap.add_argument("--tuketim-kapisi", default="")
    ap.add_argument("--modul", default="")
    a = ap.parse_args()
    if os.geteuid() != 0:
        cik({"hata": "root gerekli"}, 2)
    d = yukle()
    p = gosp.Panel()
    kaynak = p.neg if a.neg else "127.0.0.1"
    govde = json.loads(a.govde) if a.govde else None

    if a.komut == "durum":
        o = gosp.ortam()
        o.update({"kapilar": p.port, "anahtarlar": sorted(d["anahtar"]), "bayiler": {k: v["id"] for k, v in d["bayi"].items()},
                  "hostingler": {k: {"id": v["id"], "alan": v["alan"]} for k, v in d["hosting"].items()},
                  "sso_oturumlari": sorted(d["sso"])})
        o["servis"] = subprocess.run(["systemctl", "is-active", "girginospanel"], stdout=subprocess.PIPE,
                                     universal_newlines=True).stdout.strip()
        cik(o)

    if a.komut == "anahtar":
        izin = [x for x in a.izin.split(",") if x] or gosp.TUM_KOK
        if a.sahip in ("", "kok"):
            sahip, rol = "root", "admin"
        else:
            et = a.sahip.split(":", 1)[1]
            sahip, rol = d["bayi"][et]["kul"], "reseller"
        d["anahtar"][a.ad] = gosp.Anahtarlar(ONEK).ac("arac-" + a.ad, sahip, rol, izin, ipler=a.ip, hiz=a.hiz)
        kaydet(d)
        cik({"ok": True, "anahtar": a.ad, "sahip": sahip, "izinler": izin})

    if a.komut == "bayi":
        ctx_kok = kok(d, p)
        if "paket" not in d:
            y = p.istek("admin", "POST", "/reseller-plans", {"ad": "%s-%s-arac" % (ONEK, d["kosu"]), "max_domain": 10,
                                                              "max_disk_mb": 20000, "max_trafik_mb": 200000, "cpu_cekirdek": 2,
                                                              "ram_mb": 2048, "fazla_satis": True}, yetki=ctx_kok)
            if y.kod not in (200, 201):
                cik({"hata": "paket açılamadı", "yanit": yanit_ozet(y)}, 1)
            d["paket"] = int(y.alan("id"))
        kul = ("%s%sb%s" % (ONEK, d["kosu"], a.ad))[:32]
        pw = gosp.guclu_parola()
        gosp.sir_kaydet(pw)
        y = p.istek("admin", "POST", "/resellers", {"kullanici": kul, "parola": pw, "paket_id": d["paket"]}, yetki=ctx_kok)
        if y.kod not in (200, 201):
            cik({"hata": "bayi açılamadı", "yanit": yanit_ozet(y)}, 1)
        d["bayi"][a.ad] = {"id": int(y.alan("id")), "kul": kul, "parola": pw}
        kaydet(d)
        cik({"ok": True, "bayi": a.ad, "id": d["bayi"][a.ad]["id"], "kullanici": kul, "paket_id": d["paket"]})

    if a.komut == "hosting":
        alan = "%s-%s-%s.test" % (ONEK, d["kosu"], a.ad)
        g = {"alan_adi": alan}
        g.update(govde or {})
        if a.sahip:
            yetki = kim_coz(d, p, "bayi:" + a.sahip)
            y = p.istek("bayi", "POST", "/domains", g, yetki=yetki, zaman=240)
        else:
            y = p.istek("admin", "POST", "/domains", g, yetki=kok(d, p), zaman=240)
        if y.kod != 201:
            cik({"hata": "hosting açılamadı", "yanit": yanit_ozet(y)}, 1)
        pw = gosp.guclu_parola()
        gosp.sir_kaydet(pw)
        k = p.istek("admin", "PUT", "/domains/%s/ftp/password" % y.alan("id"), {"password": pw}, yetki=kok(d, p))
        d["hosting"][a.ad] = {"id": int(y.alan("id")), "alan": alan, "sk": y.alan("sistem_kullanici", varsayilan=""),
                              "parola": pw if k.kod == 200 else ""}
        kaydet(d)
        cik({"ok": True, "hosting": a.ad, "id": d["hosting"][a.ad]["id"], "alan": alan, "sistem_kullanici": d["hosting"][a.ad]["sk"],
             "parola_ayarlandi": k.kod == 200})

    if a.komut == "api":
        yetki = kim_coz(d, p, a.kim)
        y = p.istek(a.kapi, a.metod.upper(), yol_coz(d, a.yol), govde, yetki=yetki, kaynak=kaynak, zaman=240)
        cik(yanit_ozet(y, a.uzun))

    if a.komut == "giris":
        tur, _, et = a.kim.partition(":")
        parola = gosp.guclu_parola() if a.parola_yanlis else None  # rastgele = kesin yanlış
        fn = bayi_jwt if tur == "bayi" else musteri_jwt
        y, tok = fn(d, p, et, parola, kaynak)
        cik({"kod": y.kod, "hata": y.hata, "hata_kodu": y.hata_kodu, "token_alindi": bool(tok)})

    if a.komut == "site":
        alan = a.ad
        if alan.startswith("h:"):
            alan = d["hosting"][alan[2:]]["alan"]
        cik({"alan": alan, "kod": p.site_kodu(alan)})

    if a.komut == "sso":
        tur, _, et = a.hedef.partition(":")
        yol = "/domains/%d/sso" % d["hosting"][et]["id"] if tur == "h" else "/resellers/%d/sso" % d["bayi"][et]["id"]
        y = p.istek("admin", "POST", yol, {}, yetki=kok(d, p))
        url = y.alan("url", varsayilan="")
        u = urllib.parse.urlparse(url)
        kod = urllib.parse.parse_qs(u.fragment).get("kod", [""])[0]
        gosp.sir_kaydet(kod)
        o = {"uretim": yanit_ozet(y, 200), "url": {"sema": u.scheme, "port": u.port, "yol": u.path, "kod_var": bool(kod)}}
        if a.tuket and kod:
            kapi = a.tuketim_kapisi or ("musteri" if tur == "h" else "bayi")
            tyol = "/musteri/adina-giris" if tur == "h" else "/auth/sso-giris"
            g = p.istek(kapi, "POST", tyol, {"kod": kod}, kaynak=kaynak)
            tok = g.alan("token", varsayilan="")
            if tok:
                gosp.sir_kaydet(tok)
                d["sso"][et] = tok
                kaydet(d)
            o["tuketim"] = yanit_ozet(g, 300)
            o["sso_oturumu"] = ("sso:" + et) if tok else None
        cik(o)

    if a.komut == "temizle":
        t = gosp.temizle(ONEK)
        try:
            os.remove(DURUM)
        except OSError:
            pass
        cik(t, 0 if not t["hatalar"] and not t["kalinti"] else 1)

    if a.komut == "kos":
        kos = os.path.join(os.path.dirname(os.path.abspath(__file__)), "kos.py")
        arg = [sys.executable, kos, "--json", KOS_JSON] + (["--modul", a.modul] if a.modul else [])
        with open(KOS_LOG, "w") as lg:
            subprocess.Popen(arg, stdout=lg, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL, start_new_session=True)
        try:
            os.remove(KOS_JSON)
        except OSError:
            pass
        cik({"ok": True, "basladi": True, "log": KOS_LOG, "json": KOS_JSON})

    if a.komut == "kos-durum":
        calisiyor = subprocess.run(["pgrep", "-f", "sozlesme/kos.py"], stdout=subprocess.PIPE).returncode == 0
        o = {"calisiyor": calisiyor}
        try:
            satirlar = open(KOS_LOG).read().splitlines()
        except OSError:
            satirlar = []
        o["son_satirlar"] = [gosp.maskele(s) for s in satirlar[-8:]]
        if not calisiyor and os.path.exists(KOS_JSON):
            j = json.load(open(KOS_JSON))
            o["ozet"] = j["ozet"]
            o["dusenler"] = [{"alan": t["alan"], "ad": t["ad"], "kanit": t["kanit"]} for t in j["testler"] if t["sonuc"] == "dustu"]
            o["atlananlar"] = [{"alan": t["alan"], "ad": t["ad"]} for t in j["testler"] if t["sonuc"] == "atlandi"]
            o["bulgular"] = j["bulgular"]
            o["kalinti"] = (j.get("temizlik") or {}).get("kalinti")
        cik(o)

    cik({"hata": "bilinmeyen komut: " + a.komut}, 2)


if __name__ == "__main__":
    main()
