# -*- coding: utf-8 -*-
"""moduller.py — sözleşme test modülleri. Her modül `def m(ctx)` (ctx: gosp.Baglam).

Kaynak: 2026-10-13 çok ajanlı doğrulama raporlarının (hosting/SSO/anahtar ve bayi/bayi anahtarı/kapı)
test satırları + o gün düzeltilen bulgular. Bir bulgu düzeltildiğinde buraya kalıcı kontrol eklenir.
"""
import os
import re
import time
import urllib.parse

import gosp
import governor as _governor
from gosp import TUM_BAYI

PHP_SURUMLER = ["7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5", "8.6"]


def php_kurulu(s):
    yol = "/usr/sbin/php-fpm" if s == "8.3" else "/opt/remi/php%s/root/usr/sbin/php-fpm" % s.replace(".", "")
    return os.path.exists(yol)


def planlar(ctx):
    y = ctx.kok_api("GET", "/plans")
    return {str(p.get("ad")): p for p in (y.liste) if isinstance(p, dict)}


def detay(ctx, hid):
    return ctx.kok_api("GET", "/domains/%d" % hid).json or {}


# ───────────────────────── kapı ─────────────────────────

def kapi(ctx):
    r, p = ctx.r, ctx.p
    r.bolum("kapi")
    y = p.istek("admin", "GET", "/kapi/portlar", yetki=ctx.kok, desen="GET /kapi/portlar")
    r.kontrol("GET /kapi/portlar: müşteri ve bayi portu doğru, yönetici portu YOK",
              y.kod == 200 and y.alan("musteri") == p.port["musteri"] and y.alan("bayi") == p.port["bayi"]
              and "admin" not in (y.json or {}), y.ozet())
    r.kod("müşteri kapısında /auth/login yok (404)", p.istek("musteri", "POST", "/auth/login",
                                                            {"kullanici": "x", "parola": "y"}), 404)
    r.kod("yönetici kapısında /musteri/login yok (404)", p.istek("admin", "POST", "/musteri/login",
                                                                {"kullanici": "x", "parola": "y"}), 404)
    r.kod("müşteri kapısında /auth/sso-giris yok (404)", p.istek("musteri", "POST", "/auth/sso-giris", {"kod": "x" * 43}), 404)
    r.kod("yönetici kapısında /auth/sso-giris yok (404)", p.istek("admin", "POST", "/auth/sso-giris", {"kod": "x" * 43}), 404)
    r.kod("bayi kapısında /musteri/adina-giris yok (404)", p.istek("bayi", "POST", "/musteri/adina-giris", {"kod": "x" * 43}), 404)
    r.kod("kök anahtar bayi kapısında geçmez (401)", p.istek("bayi", "GET", "/domains", yetki=ctx.kok), 401)
    r.kod("kök anahtar müşteri kapısında geçmez (401)", p.istek("musteri", "GET", "/domains", yetki=ctx.kok), 401)


# ───────────────────────── anahtar / izin ─────────────────────────

def anahtar(ctx):
    r, p, an = ctx.r, ctx.p, ctx.an
    r.bolum("anahtar")
    r.kod("kimliksiz istek 401", p.istek("admin", "GET", "/domains", desen="GET /domains"), 401)
    tam = ctx.kok.split(":", 1)[1]
    r.kod("'Bearer gosp_…' biçimi reddedilir (401)", p.istek("admin", "GET", "/domains", yetki="Bearer " + tam), 401)
    r.kod("anahtar başka kullanıcı adıyla geçmez (401)", p.istek("admin", "GET", "/domains", yetki="gosp admin:" + tam), 401)
    ben = p.istek("admin", "GET", "/api-anahtari/ben", yetki=ctx.kok)
    r.kontrol("GET /api-anahtari/ben: meta döner, gizli/özet DÖNMEZ",
              ben.kod == 200 and "ozet" not in (ben.json or {}) and tam[-10:] not in ben.govde, ben.ozet())
    oku = an.ac("oku", "root", "admin", ["hesap:oku", "plan:oku"])
    r.kod("yalnız okuma izniyle POST /domains 403", p.istek("admin", "POST", "/domains", {"alan_adi": ctx.alan_adi("yasak")},
                                                           yetki=oku, desen="POST /domains"), 403, "izni yok")
    bos = an.ac("bos", "root", "admin", ["denetim:oku"])
    r.kod("hesap izni olmadan GET /domains 403", p.istek("admin", "GET", "/domains", yetki=bos), 403)
    for yol, metod in (("/api-anahtarlari", "GET"), ("/me", "GET"), ("/me/parola", "POST"), ("/me/2fa/setup", "GET")):
        r.kod("kimlik ucu anahtara kapalı: %s %s (403)" % (metod, yol),
              p.istek("admin", metod, yol, {} if metod == "POST" else None, yetki=ctx.kok), 403)
    r.kod("tarayıcıdan (Origin başlığı) kullanım reddi (403)",
          p.istek("admin", "GET", "/domains", yetki=ctx.kok, basliklar={"Origin": "https://evil.example"}), 403)
    ip = an.ac("ip", "root", "admin", ["hesap:oku"], ipler="192.0.2.0/24")
    r.kod("izinli IP listesi dışı 403", p.istek("admin", "GET", "/domains", yetki=ip), 403)
    hiz = an.ac("hiz", "root", "admin", ["hesap:oku"], hiz=3)
    son = None
    for _ in range(4):
        son = p.istek("admin", "GET", "/api-anahtari/ben", yetki=hiz)
    r.kontrol("anahtar hız sınırı: 4. istek 429 + Retry-After", son.kod == 429 and "retry-after" in son.basliklar, son.ozet())


# ───────────────────────── hosting ─────────────────────────

def hosting(ctx):
    r, p = ctx.r, ctx.p
    r.bolum("hosting")
    pl = planlar(ctx)
    h = ctx.hosting("h1")
    d = detay(ctx, h["id"])
    r.kontrol("hosting açıldı (kök anahtar) ve ayrıntı okunuyor", d.get("alan_adi") == h["alan"], str(d.get("alan_adi")))
    r.kod("müşteri paneline giriş (200)", ctx.musteri_giris(h), 200)
    r.kontrol("site açık (200)", p.site_bekle(h["alan"], 200) == 200, "HTTP %d" % p.site_kodu(h["alan"]))

    # askı
    yol = "/domains/%d" % h["id"]
    r.kod("askıya al (200)", ctx.kok_api("POST", yol + "/askiya-al", desen="POST /domains/{id}/askiya-al"), 200)
    r.kod("askıya al tekrar (200, idempotent)", ctx.kok_api("POST", yol + "/askiya-al"), 200)
    r.kontrol("askıda: ayrıntıda askida=true", detay(ctx, h["id"]).get("askida") is True)
    r.kontrol("askıda: site 503", p.site_bekle(h["alan"], 503) == 503, "HTTP %d" % p.site_kodu(h["alan"]))
    r.kod("askıda: müşteri girişi reddedilir (403)", ctx.musteri_giris(h), (401, 403))
    r.kontrol("askıda: FTP girişi kapalı", not gosp.ftp_giris(h["sk"], h["parola"]))
    r.kod("askıda: müşteri SSO kodu üretilmez (409)", ctx.kok_api("POST", yol + "/sso", desen="POST /domains/{id}/sso"), 409)
    r.kod("askıda: PHP sürümü değişmez (409)", ctx.kok_api("PUT", yol + "/php", {"php_surum": "8.3"}, desen="PUT /domains/{id}/php"), 409)
    r.kod("askıdan al (200)", ctx.kok_api("POST", yol + "/askidan-al", desen="POST /domains/{id}/askidan-al"), 200)
    r.kontrol("askıdan çıkış: site 200", p.site_bekle(h["alan"], 200) == 200, "HTTP %d" % p.site_kodu(h["alan"]))
    r.kod("askıdan çıkış: müşteri girişi (200)", ctx.musteri_giris(h), 200)
    r.kontrol("askıdan çıkış: FTP girişi açık", gosp.ftp_giris(h["sk"], h["parola"]))

    # plan (yükselt / düşür / zorunluluk)
    if "Başlangıç" in pl and "Standart" in pl:
        bas, std = pl["Başlangıç"], pl["Standart"]
        r.kod("plan yükselt → Standart (200)", ctx.kok_api("PUT", yol + "/plan", {"plan_id": std["id"]}, desen="PUT /domains/{id}/plan"), 200)
        k_std = int(detay(ctx, h["id"]).get("disk_kota_mb") or 0)
        r.kontrol("Standart planında ve kotası planla aynı", detay(ctx, h["id"]).get("plan_ad") == "Standart"
                  and k_std == int(std.get("disk_kota_mb") or 0), "kota %d" % k_std)
        r.kod("plan düşür → Başlangıç (200)", ctx.kok_api("PUT", yol + "/plan", {"plan_id": bas["id"]}), 200)
        k_bas = int(detay(ctx, h["id"]).get("disk_kota_mb") or 0)
        r.kontrol("düşürmede kota gerçekten düştü", 0 < k_bas < k_std, "%d → %d" % (k_std, k_bas))
        for g in ({}, {"planid": std["id"]}, {"plan": std["id"]}):
            r.kod("PUT plan gövdesi %s → 400 plan_id zorunlu" % g, ctx.kok_api("PUT", yol + "/plan", g), 400, "plan_id zorunlu")
        r.kontrol("zorunluluk reddinden sonra plan değişmedi", detay(ctx, h["id"]).get("plan_ad") == "Başlangıç")
        r.kod("PUT plan metin plan_id 400", ctx.kok_api("PUT", yol + "/plan", {"plan_id": str(std["id"])}), 400)
        r.kod("PUT plan olmayan plan 400", ctx.kok_api("PUT", yol + "/plan", {"plan_id": 99999999}), 400, "plan bulunamadı")
    else:
        r.atla("plan yükselt/düşür", "Başlangıç ve Standart planları yok")

    # POST /domains kök plan doğrulaması (yan etkisiz ret)
    for pid in (99999999, 0, -1):
        alan = ctx.alan_adi("plan%s" % str(pid).replace("-", "m"))
        y = ctx.kok_api("POST", "/domains", {"alan_adi": alan, "plan_id": pid}, desen="POST /domains")
        r.kod("kök POST /domains plan_id=%d → 400 plan bulunamadı" % pid, y, 400, "plan bulunamadı")
        if y.kod == 201:
            r.bulgu("yuksek", "olmayan plan_id ile hosting açıldı", alan)

    # kurulu olmayan PHP → 409 ve yetim kullanıcı/home YOK
    eksik = [s for s in PHP_SURUMLER if not php_kurulu(s)]
    if eksik:
        s = eksik[-1]
        once = gosp.kalinti(ctx.onek).get("sistem_kullanicisi", 0)
        alan = ctx.alan_adi("php" + s.replace(".", ""))
        y = ctx.kok_api("POST", "/domains", {"alan_adi": alan, "php_surum": s}, desen="POST /domains", zaman=240)
        r.kod("kurulu olmayan PHP %s → 409" % s, y, 409, "kurulu değil")
        sonra = gosp.kalinti(ctx.onek).get("sistem_kullanicisi", 0)
        r.kontrol("kurulu olmayan PHP: yetim sistem kullanıcısı kalmadı", sonra == once, "%d → %d" % (once, sonra))
    else:
        r.atla("kurulu olmayan PHP → 409", "sunucuda desteklenen bütün sürümler kurulu")

    # PHP yazımları kanonik
    kurulu = [s for s in PHP_SURUMLER if php_kurulu(s) and s != "8.3"]
    if kurulu:
        s = kurulu[0]
        y = ctx.kok_api("PUT", yol + "/php", {"php_surum": "php" + s.replace(".", "")}, zaman=240)
        r.kontrol("PHP yazımı php%s → %s (kanonik)" % (s.replace(".", ""), s), y.kod == 200 and y.alan("php_surum") == s, y.ozet())
        r.kontrol("site PHP değişiminden sonra açık", p.site_bekle(h["alan"], 200) == 200)
    r.kod("geçersiz PHP yazımı 400", ctx.kok_api("PUT", yol + "/php", {"php_surum": "8.30"}), 400)

    r.kod("silme: yanlış onay 400", ctx.kok_api("DELETE", yol + "?onay=yanlis.test", desen="DELETE /domains/{id}"), 400)

    # bayi_id: kök, hostingi bayi ADINA açar (WHMCS bayi ürünü)
    b = ctx.bayi("H")
    hesap_ok = ctx.an.ac("hbid", "root", "admin", ["hesap:yaz", "plan:oku"])
    r.kod("bayi_id: anahtarda bayi:yaz yoksa 403", p.istek("admin", "POST", "/domains", {"alan_adi": ctx.alan_adi("bid0"), "bayi_id": b["id"]},
                                                            yetki=hesap_ok, desen="POST /domains"), 403, "bayi")
    r.kod("bayi_id: olmayan bayi 400", ctx.kok_api("POST", "/domains", {"alan_adi": ctx.alan_adi("bid1"), "bayi_id": 99999999}), 400, "bayi bulunamadı")
    r.kod("bayi_id: bayi oturumu kullanamaz (403)", p.istek("bayi", "POST", "/domains", {"alan_adi": ctx.alan_adi("bid2"), "bayi_id": b["id"]},
                                                             yetki=b["jwt"]), 403)
    hb = ctx.hosting("bid3", govde_ek={"bayi_id": b["id"]})
    d = detay(ctx, hb["id"])
    r.kontrol("bayi_id: hosting bayiye ait (reseller_id)", int(d.get("reseller_id") or 0) == b["id"], str(d.get("reseller_id")))
    lst = p.istek("bayi", "GET", "/domains", yetki=b["jwt"]).liste
    r.kontrol("bayi_id: hosting bayinin listesinde", any(x.get("alan_adi") == hb["alan"] for x in lst))

    # plan değişimi + hemen silme: yetim vhost kalmamalı (ajan C bulgusu)
    hy = ctx.hosting("yaris")
    if "Standart" in pl:
        ctx.kok_api("PUT", "/domains/%d/plan" % hy["id"], {"plan_id": pl["Standart"]["id"]})
    s = ctx.kok_api("DELETE", "/domains/%d?onay=%s" % (hy["id"], hy["alan"]), zaman=240)
    ctx.hostingler.pop("yaris", None)
    r.kod("plan değişiminin hemen ardından silme 200", s, 200)
    time.sleep(12)
    yetim = [f for f in os.listdir("/etc/nginx/conf.d") if hy["sk"] and hy["sk"] in f]
    r.kontrol("plan+silme yarışı: yetim vhost yok", not yetim, ", ".join(yetim))


# ───────────────────────── parola ─────────────────────────

def parola(ctx):
    r, p = ctx.r, ctx.p
    r.bolum("parola")
    h = ctx.hosting("h1")
    yol = "/domains/%d/ftp/password" % h["id"]
    for ad, pw in (("kısa", "Ab1"), ("rakamsız", "Abcdefghijklmn"), ("satır sonu", "Abcdef123456\nx")):
        r.kod("hosting parola kuralı: %s → 400" % ad, ctx.kok_api("PUT", yol, {"password": pw}, desen="PUT /domains/{id}/ftp/password"), 400)
    r.kod("password/parola uyuşmazlığı 400", ctx.kok_api("PUT", yol, {"password": gosp.guclu_parola(), "parola": gosp.guclu_parola()}), 400)
    for ad, pw in (("TAB", "Abcdef123456\tx"), ("U+0001", "Abcdef123456\u0001x"), ("U+007F", "Abcdef123456\u007fx")):
        r.kod("hosting parolası denetim karakteri %s → 400" % ad, ctx.kok_api("PUT", yol, {"password": pw}), 400)
    yeni = gosp.guclu_parola()
    gosp.sir_kaydet(yeni)
    y = ctx.kok_api("PUT", yol, {"password": yeni})
    r.kontrol("hosting parolası değişti, parola yanıtta yok", y.kod == 200 and yeni not in y.govde, y.ozet())
    r.kod("yeni parolayla müşteri girişi (200)", ctx.musteri_giris(h, yeni), 200)
    r.kod("eski parola artık geçmez (401)", ctx.musteri_giris(h, h["parola"], kaynak=p.neg), 401)
    h["parola"] = yeni

    # bayi parola sıfırlama (bayi.parola)
    b = ctx.bayi("P")
    pyol = "/resellers/%d/password" % b["id"]
    an = ctx.an
    yalniz_bayi = an.ac("pbayi", "root", "admin", ["bayi:yaz"])
    bayi_anahtari = an.ac("pbayikey", b["kul"], "reseller", TUM_BAYI)
    r.kod("bayi parolası: yalnız bayi:yaz yetmez (403)",
          p.istek("admin", "PUT", pyol, {"parola": "Yeni-Parola-123"}, yetki=yalniz_bayi, desen="PUT /resellers/{id}/password"), 403)
    r.kod("bayi parolası: bayi anahtarı kullanamaz (403)",
          p.istek("bayi", "PUT", pyol, {"parola": "Yeni-Parola-123"}, yetki=bayi_anahtari), 403)
    r.kod("bayi parolası: kısa 400", ctx.kok_api("PUT", pyol, {"parola": "kisa"}, desen="PUT /resellers/{id}/password"), 400)
    r.kod("bayi parolası: 73 bayt → 400 (bcrypt sınırı, 500 değil)", ctx.kok_api("PUT", pyol, {"parola": "Ab1" + "x" * 70}), 400, "72 bayt")
    r.kod("bayi parolası: 37 Türkçe karakter (74 bayt) → 400", ctx.kok_api("PUT", pyol, {"parola": "ğ" * 37}), 400, "72 bayt")
    r.kod("bayi parolası: parola ≠ password 400", ctx.kok_api("PUT", pyol, {"parola": "Aaaa11111111", "password": "Bbbb22222222"}), 400)
    r.kod("bayi parolası: olmayan bayi 404", ctx.kok_api("PUT", "/resellers/99999999/password", {"parola": "Aaaa11111111"}), 404)
    r.kod("PUT /resellers/{id} parola anahtarla hâlâ 403 ve yeni uca yönlendirir",
          ctx.kok_api("PUT", "/resellers/%d" % b["id"], {"parola": "Aaaa11111111"}, desen="PUT /resellers/{id}"), 403, "/password")
    r.kod("bayi anahtarı sıfırlamadan önce çalışıyor", p.istek("bayi", "GET", "/domains", yetki=bayi_anahtari), 200)
    eski_jwt = b["jwt"]
    yeni = gosp.guclu_parola()
    gosp.sir_kaydet(yeni)
    y = ctx.kok_api("PUT", pyol, {"password": yeni})
    r.kontrol("bayi parolası sıfırlandı ('password' takma adı), parola yanıtta yok", y.kod == 200 and yeni not in y.govde, y.ozet())
    r.kontrol("yeni parolayla bayi girişi (200)", ctx.bayi_giris(b, yeni) is not None)
    r.kontrol("eski bayi parolası geçmez", ctx.bayi_giris(b, b["parola"], kaynak=p.neg) is None)
    r.kod("sıfırlama öncesi bayi oturumu düştü (401)", p.istek("bayi", "GET", "/reseller/ozet", yetki=eski_jwt), (401, 403))
    r.kod("sıfırlamada bayinin API anahtarı iptal edildi (401)", p.istek("bayi", "GET", "/domains", yetki=bayi_anahtari), 401)
    b["parola"] = yeni
    b["jwt"] = ctx.bayi_giris(b)


# ───────────────────────── SSO ─────────────────────────

def _kod_url(url):
    u = urllib.parse.urlparse(url)
    return u, urllib.parse.parse_qs(u.fragment).get("kod", [""])[0]


def sso(ctx):
    r, p, an = ctx.r, ctx.p, ctx.an
    r.bolum("sso")
    h = ctx.hosting("h1")
    syol = "/domains/%d/sso" % h["id"]
    y = ctx.kok_api("POST", syol, {}, desen="POST /domains/{id}/sso")
    u, kod = _kod_url(y.alan("url", varsayilan=""))
    r.kontrol("müşteri SSO: url müşteri kapısında /adina#kod=, 60 sn, no-store",
              y.kod == 200 and u.scheme == "https" and u.port == p.port["musteri"] and u.path == "/adina" and len(kod) >= 32
              and y.alan("gecerlilik_sn") == 60 and y.basliklar.get("cache-control") == "no-store", y.ozet())
    gosp.sir_kaydet(kod)
    r.kod("müşteri kodu bayi kapısında tüketilmez (404)", p.istek("bayi", "POST", "/musteri/adina-giris", {"kod": kod}), 404)
    g = p.istek("musteri", "POST", "/musteri/adina-giris", {"kod": kod}, desen="POST /musteri/adina-giris")
    r.kontrol("müşteri kodu müşteri kapısında oturum açar (sso=true)", g.kod == 200 and g.alan("sso") is True, g.ozet())
    gosp.sir_kaydet(g.alan("token", varsayilan=""))
    r.kod("müşteri kodu tek kullanımlık (401)", p.istek("musteri", "POST", "/musteri/adina-giris", {"kod": kod}, kaynak=p.neg), 401)
    ssosuz = an.ac("ssosuz", "root", "admin", ["hesap:yaz"])
    r.kod("sso izni olmadan 403", p.istek("admin", "POST", syol, {}, yetki=ssosuz), 403)
    jwt = gosp.admin_jwt()
    if jwt:
        r.kod("SSO yalnız API anahtarıyla (yönetici JWT 403)", p.istek("admin", "POST", syol, {}, yetki=jwt), 403)
    else:
        r.atla("SSO yönetici JWT ile 403", "jeton betiği yok")
    r.kod("müşteri SSO olmayan hesap 404", ctx.kok_api("POST", "/domains/99999999/sso", {}), 404)

    # bayi SSO
    b = ctx.bayi("S")
    byol = "/resellers/%d/sso" % b["id"]
    for ad, izin in (("yalnız sso", ["sso:yaz"]), ("yalnız bayi:yaz", ["bayi:yaz"]), ("sso + bayi:oku", ["sso:yaz", "bayi:oku"])):
        k = an.ac("bs" + str(len(izin)) + ad[:3].replace(" ", ""), "root", "admin", izin)
        r.kod("bayi SSO: %s yetmez (403)" % ad, p.istek("admin", "POST", byol, {}, yetki=k, desen="POST /resellers/{id}/sso"), 403)
    bk = an.ac("bssobayi", b["kul"], "reseller", TUM_BAYI)
    r.kod("bayi SSO: bayi anahtarı kullanamaz (403)", p.istek("bayi", "POST", byol, {}, yetki=bk), 403)
    if jwt:
        r.kod("bayi SSO: yönetici JWT 403", p.istek("admin", "POST", byol, {}, yetki=jwt), 403)
    r.kod("bayi SSO: olmayan bayi 404", ctx.kok_api("POST", "/resellers/99999999/sso", {}), 404)
    r.kod("bayi SSO: kök kullanıcı (bayi değil) 404", ctx.kok_api("POST", "/resellers/1/sso", {}), 404)
    y = ctx.kok_api("POST", byol, {}, desen="POST /resellers/{id}/sso")
    u, kod = _kod_url(y.alan("url", varsayilan=""))
    gosp.sir_kaydet(kod)
    r.kontrol("bayi SSO: url bayi kapısında /sso#kod=, 60 sn, no-store",
              y.kod == 200 and u.port == p.port["bayi"] and u.path == "/sso" and len(kod) >= 32
              and y.alan("gecerlilik_sn") == 60 and y.basliklar.get("cache-control") == "no-store", y.ozet())
    r.kod("bayi kodu müşteri kapısında tüketilmez (404)", p.istek("musteri", "POST", "/auth/sso-giris", {"kod": kod}), 404)
    r.kod("bayi kodu yönetici kapısında tüketilmez (404)", p.istek("admin", "POST", "/auth/sso-giris", {"kod": kod}), 404)
    g = p.istek("bayi", "POST", "/auth/sso-giris", {"kod": kod}, desen="POST /auth/sso-giris")
    tok = g.alan("token", varsayilan="")
    gosp.sir_kaydet(tok)
    r.kontrol("bayi kodu bayi oturumu açar (rol reseller, doğru bayi, sso=true)",
              g.kod == 200 and g.alan("kullanici", "rol") == "reseller" and g.alan("kullanici", "adi") == b["kul"]
              and g.alan("sso") is True, g.ozet())
    r.kod("bayi kodu tek kullanımlık (401)", p.istek("bayi", "POST", "/auth/sso-giris", {"kod": kod}, kaynak=p.neg), 401)
    st = "Bearer " + tok
    r.kod("SSO bayi oturumu: bayi özeti 200", p.istek("bayi", "GET", "/reseller/ozet", yetki=st), 200)
    r.kod("SSO bayi oturumu: API anahtarı yönetimi 403", p.istek("bayi", "GET", "/api-anahtarlari", yetki=st), 403)
    r.kod("SSO bayi oturumu: 2FA kurulumu 403", p.istek("bayi", "GET", "/me/2fa/setup", yetki=st), 403)
    r.kod("parolalı bayi oturumu: API anahtarı yönetimi açık (200)", p.istek("bayi", "GET", "/api-anahtarlari", yetki=b["jwt"]), 200)
    hz = an.ac("bsshiz", "root", "admin", ["sso:yaz", "bayi:yaz"])
    son = None
    for _ in range(31):
        son = p.istek("admin", "POST", byol, {}, yetki=hz)
    r.kontrol("bayi SSO hız sınırı: 31. istek 429 + Retry-After", son.kod == 429 and "retry-after" in son.basliklar, son.ozet())


# ───────────────────────── bayi ─────────────────────────

def bayi(ctx):
    r, p, an = ctx.r, ctx.p, ctx.an
    r.bolum("bayi")
    b = ctx.bayi("A")
    lst = {int(x["id"]): x for x in (ctx.kok_api("GET", "/resellers").liste)}
    x = lst.get(b["id"], {})
    r.kontrol("bayi listede; havuz paketten kopyalandı (cpu 2, ram 2048)",
              float(x.get("cpu_cekirdek") or 0) == 2 and int(x.get("ram_mb") or 0) == 2048 and int(x.get("paket_id") or 0) == ctx.paket(),
              str({k: x.get(k) for k in ("cpu_cekirdek", "ram_mb", "paket_id")}))
    y = ctx.kok_api("POST", "/resellers", {"kullanici": ctx.kul_adi("uzun"), "parola": "Aaaa11111111", "paket_id": ctx.paket(),
                                            "ad_soyad": "ş" * 121}, desen="POST /resellers")
    r.kod("POST /resellers ad_soyad 121 → 400 (ham SQL yok)", y, 400, "120")
    r.kod("POST /resellers bilinmeyen alan → 400", ctx.kok_api("POST", "/resellers", {"kullanici": ctx.kul_adi("bil"), "parola": "Aaaa11111111",
                                                                                        "paket_id": ctx.paket(), "eposta": "x"}), 400)
    r.kod("POST /resellers aynı kullanıcı adı → 409", ctx.kok_api("POST", "/resellers", {"kullanici": b["kul"], "parola": "Aaaa11111111",
                                                                                           "paket_id": ctx.paket()}), 409)
    r.kod("POST /resellers paketsiz → 400", ctx.kok_api("POST", "/resellers", {"kullanici": ctx.kul_adi("pk"), "parola": "Aaaa11111111"}), 400)
    r.kod("POST /resellers 73 baytlık parola → 400", ctx.kok_api("POST", "/resellers", {"kullanici": ctx.kul_adi("p73"), "parola": "Ab1" + "x" * 70,
                                                                                     "paket_id": ctx.paket()}), 400, "72 bayt")
    for ad, g in (("açıklama 256", {"aciklama": "ş" * 256}), ("max_domain taşma", {"max_domain": 2147483648}),
                  ("mail_max_email taşma", {"mail_max_email": 2147483648}), ("mail_kutu_kota_mb negatif", {"mail_kutu_kota_mb": -1})):
        govde = {"ad": "%s-%s-sinir" % (ctx.onek, ctx.kosu), "max_domain": 1, "fazla_satis": True}
        govde.update(g)
        y = ctx.kok_api("POST", "/reseller-plans", govde)
        r.kontrol("bayi paketi %s → 400 (ham SQL yok)" % ad, y.kod == 400 and "Error 1" not in y.govde, y.ozet())

    k = ctx.kok_api("GET", "/bayi-kaynak/%d" % b["id"], desen="GET /bayi-kaynak/{id}")
    r.kontrol("GET /bayi-kaynak/{id} kök anahtarla 200, havuz cpu 2", k.kod == 200 and float(k.alan("havuz", "cpu_cekirdek", varsayilan=0)) == 2, k.ozet())
    bkey = an.ac("akey", b["kul"], "reseller", TUM_BAYI)
    r.kod("GET /bayi-kaynak/{id} bayi anahtarına kapalı", p.istek("bayi", "GET", "/bayi-kaynak/%d" % b["id"], yetki=bkey), (401, 403, 404))
    r.kod("GET /bayi-kaynak/{id} bayi izni olmadan 403", p.istek("admin", "GET", "/bayi-kaynak/%d" % b["id"],
                                                               yetki=an.ac("kaynaksiz", "root", "admin", ["hesap:oku"])), 403)

    # paket uçları: olmayan 404 (varsayılan korunur), ad çakışması 409
    paketler = ctx.kok_api("GET", "/reseller-plans").liste
    varsayilan_once = sorted(int(q["id"]) for q in paketler if q.get("varsayilan"))
    r.kod("PUT /reseller-plans/{olmayan} varsayilan:true → 404",
          ctx.kok_api("PUT", "/reseller-plans/99999999", {"ad": "x", "max_domain": 1, "varsayilan": True}, desen="PUT /reseller-plans/{id}"), 404)
    varsayilan_sonra = sorted(int(q["id"]) for q in (ctx.kok_api("GET", "/reseller-plans").liste) if q.get("varsayilan"))
    r.kontrol("olmayan pakete PUT varsayılanı değiştirmedi", varsayilan_once == varsayilan_sonra, "%s → %s" % (varsayilan_once, varsayilan_sonra))
    r.kod("PUT /reseller-plans/abc → 404", ctx.kok_api("PUT", "/reseller-plans/abc", {"ad": "x", "max_domain": 1}), 404)
    r.kod("DELETE /reseller-plans/{olmayan} → 404", ctx.kok_api("DELETE", "/reseller-plans/99999999", desen="DELETE /reseller-plans/{id}"), 404)
    ana_ad = "%s-%s-paket" % (ctx.onek, ctx.kosu)
    k2 = ctx.kok_api("POST", "/reseller-plans", {"ad": "%s-%s-kucuk" % (ctx.onek, ctx.kosu), "max_domain": 1, "max_disk_mb": 5000,
                                                 "max_trafik_mb": 50000, "fazla_satis": True})
    kucuk = int(k2.alan("id", varsayilan=0))
    c = ctx.kok_api("PUT", "/reseller-plans/%d" % kucuk, {"ad": ana_ad, "max_domain": 1, "fazla_satis": True})
    r.kod("paket ad çakışması 409 (ham SQL yok)", c, 409)
    r.kontrol("çakışma yanıtında SQL ayrıntısı yok", "Duplicate" not in c.govde and "1062" not in c.govde, c.ozet())

    # paket değişimi: yükselt/düşür, kullanımın altına düşürme
    h1 = ctx.hosting("ba1", sahip="A")
    ctx.hosting("ba2", sahip="A")
    r.kod("paket düşür (2 hosting varken max_domain 1)", ctx.kok_api("PUT", "/resellers/%d" % b["id"], {"paket_id": kucuk},
                                                                      desen="PUT /resellers/{id}"), 200)
    x = {int(q["id"]): q for q in (ctx.kok_api("GET", "/resellers").liste)}.get(b["id"], {})
    r.kontrol("bayinin limiti yeni paketten (1 alan adı)", int(x.get("max_domain") or 0) == 1 and int(x.get("paket_id") or 0) == kucuk, str(x.get("max_domain")))
    r.kontrol("düşürme sonrası mevcut hosting çalışıyor (200)", p.site_bekle(h1["alan"], 200) == 200)
    y = p.istek("bayi", "POST", "/domains", {"alan_adi": ctx.alan_adi("ba3")}, yetki=b["jwt"], desen="POST /domains", zaman=240)
    r.kod("düşürme sonrası yeni hosting limitten reddedilir (403)", y, 403, "limit")
    r.kod("paket geri yükselt", ctx.kok_api("PUT", "/resellers/%d" % b["id"], {"paket_id": ctx.paket()}), 200)
    if kucuk:
        ctx.kok_api("DELETE", "/reseller-plans/%d" % kucuk)


# ───────────────────────── bayi askısı ─────────────────────────

def aski(ctx):
    r, p, an = ctx.r, ctx.p, ctx.an
    r.bolum("aski")
    b = ctx.bayi("A")
    h1 = ctx.hosting("ba1", sahip="A")
    h2 = ctx.hosting("ba2", sahip="A")
    bkey = an.ac("askikey", b["kul"], "reseller", TUM_BAYI)
    r.kod("bayi ikinci hostingi kendisi askıya aldı", p.istek("bayi", "POST", "/domains/%d/askiya-al" % h2["id"], yetki=b["jwt"]), 200)
    y = ctx.kok_api("PUT", "/resellers/%d" % b["id"], {"durum": "suspended"}, desen="PUT /resellers/{id}", zaman=240)
    r.kontrol("bayi askıya alındı (etkilenen hosting ≥1)", y.kod == 200 and int(y.alan("etkilenen_hosting", varsayilan=0)) >= 1, y.ozet())
    for h in (h1, h2):
        r.kontrol("bayi askısı → hosting askıda (%s)" % h["alan"], detay(ctx, h["id"]).get("askida") is True)
        r.kontrol("bayi askısı → site 503 (%s)" % h["alan"], p.site_bekle(h["alan"], 503) == 503, "HTTP %d" % p.site_kodu(h["alan"]))
    r.kod("bayi askısı → müşteri girişi reddedilir", ctx.musteri_giris(h1), (401, 403))
    g = p.istek("bayi", "POST", "/auth/login", {"kullanici": b["kul"], "parola": b["parola"]}, desen="POST /auth/login")
    r.kod("askıdaki bayi DOĞRU parolayla giremez (403 hesap_askida)", g, 403, hata_kodu="hesap_askida")
    r.kod("askı öncesi bayi oturumu düştü", p.istek("bayi", "GET", "/reseller/ozet", yetki=b["jwt"]), (401, 403))
    r.kod("askıdaki bayinin anahtarı 403 bayi_askida", p.istek("bayi", "GET", "/domains", yetki=bkey), 403, hata_kodu="bayi_askida")
    r.kod("askıdaki bayiye tek tık giriş yok (409)", ctx.kok_api("POST", "/resellers/%d/sso" % b["id"], {}), 409)
    r.kod("askıdaki bayinin hostingine müşteri SSO yok (409)", ctx.kok_api("POST", "/domains/%d/sso" % h1["id"], {}), 409)
    y = ctx.kok_api("PUT", "/resellers/%d" % b["id"], {"durum": "active"}, zaman=240)
    r.kod("bayi askıdan alındı", y, 200)
    r.kontrol("askıdan çıkış → bayi askısının kapattığı hosting açıldı", detay(ctx, h1["id"]).get("askida") is False)
    r.kontrol("askıdan çıkış → site 200", p.site_bekle(h1["alan"], 200) == 200, "HTTP %d" % p.site_kodu(h1["alan"]))
    r.kod("askıdan çıkış → müşteri girişi 200", ctx.musteri_giris(h1), 200)
    r.kontrol("bayinin KENDİ askıya aldığı hosting askıda KALDI", detay(ctx, h2["id"]).get("askida") is True)
    r.kod("askıdan çıkış → bayi anahtarı yeniden çalışıyor", p.istek("bayi", "GET", "/domains", yetki=bkey), 200)
    yeni = ctx.bayi_giris(b)
    r.kontrol("askıdan çıkış → bayi girişi 200", yeni is not None)
    if yeni:
        b["jwt"] = yeni
    p.istek("bayi", "POST", "/domains/%d/askidan-al" % h2["id"], yetki=b["jwt"])


# ───────────────────────── IDOR ─────────────────────────

def idor(ctx):
    r, p, an = ctx.r, ctx.p, ctx.an
    r.bolum("idor")
    a = ctx.bayi("A")
    ctx.bayi("B")
    hb = ctx.hosting("bb1", sahip="B")
    hk = ctx.hosting("h1")
    akey = an.ac("idora", a["kul"], "reseller", TUM_BAYI)
    hedefler = (("B bayisinin", hb["id"]), ("kökün", hk["id"]), ("olmayan", 99999999))
    uclar = (("GET", "/domains/{id}", None), ("POST", "/domains/{id}/askiya-al", None), ("PUT", "/domains/{id}/plan", {"plan_id": 1}),
             ("PUT", "/domains/{id}/php", {"php_surum": "8.3"}), ("PUT", "/domains/{id}/ftp/password", {"password": gosp.guclu_parola()}),
             ("POST", "/domains/{id}/sso", {}), ("GET", "/domains/{id}/dns", None), ("GET", "/domains/{id}/databases", None),
             ("DELETE", "/domains/{id}?onay=x.test", None))
    once = {hb["id"]: detay(ctx, hb["id"]).get("askida"), hk["id"]: detay(ctx, hk["id"]).get("askida")}
    for etiket, hid in hedefler:
        reddedildi = []
        for metod, desen, govde in uclar:
            y = p.istek("bayi", metod, desen.replace("{id}", str(hid)), govde, yetki=akey)
            if y.kod not in (403, 404):
                reddedildi.append("%s %s → %d" % (metod, desen, y.kod))
        r.kontrol("bayi A anahtarı %s hostinginde 9 uçta reddedildi" % etiket, not reddedildi, "; ".join(reddedildi))
    sonra = {hb["id"]: detay(ctx, hb["id"]).get("askida"), hk["id"]: detay(ctx, hk["id"]).get("askida")}
    r.kontrol("IDOR denemelerinden sonra hedeflerin durumu değişmedi", once == sonra, "%s → %s" % (once, sonra))
    lst = p.istek("bayi", "GET", "/domains", yetki=akey).liste
    r.kontrol("bayi A listesi yalnız kendi hostinglerini içeriyor",
              all(str(d.get("alan_adi", "")).startswith(ctx.onek + "-" + ctx.kosu + "-ba") for d in lst), str([d.get("alan_adi") for d in lst])[:200])
    r.kod("bayi anahtarı bayi uçlarına (kök) erişemez", p.istek("bayi", "GET", "/resellers", yetki=akey), 403)
    r.kod("bayi anahtarı başka bayiye SSO alamaz", p.istek("bayi", "POST", "/resellers/%d/sso" % ctx.bayi("B")["id"], {}, yetki=akey), 403)


# ───────────────────────── çapraz: sızıntı ─────────────────────────

SIZINTI = [re.compile(x) for x in (r"Error \d{4} \(\w{5}\)", r"Duplicate entry", r"Data too long", r"goroutine \d+",
                                   r"\bpanic:", r"sql: ", r"database/sql")]


def sizinti(ctx):
    r = ctx.r
    r.bolum("sizinti")
    bulunan = []
    for g in ctx.p.gozlemler:
        for d in SIZINTI:
            if d.search(g["hata"] or g["govde"]):
                bulunan.append("%s %d: %s" % (g["desen"], g["kod"], g["hata"][:120]))
                break
    r.kontrol("hata yanıtlarında ham SQL/iç ayrıntı yok (%d hata yanıtı tarandı)" % len(ctx.p.gozlemler), not bulunan, "; ".join(bulunan))
    duz = sorted({"%s %d" % (g["desen"], g["kod"]) for g in ctx.p.gozlemler if g["duz"]})
    r.kontrol("kayıtlı uçların hata gövdeleri JSON", not duz, ", ".join(duz))


MODULLER = [("kapi", kapi), ("anahtar", anahtar), ("hosting", hosting), ("parola", parola), ("sso", sso),
            ("bayi", bayi), ("aski", aski), ("idor", idor), ("sizinti", sizinti),
            ("governor", _governor.governor)]

# Varsayılan koşuya girmeyen AĞIR modüller (yalnız --modul ile): MariaDB/panel yeniden başlatır, ayar değiştirir, ~13 dk sürer.
AGIR = {"governor"}
