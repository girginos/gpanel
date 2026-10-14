# -*- coding: utf-8 -*-
"""gosp.py — GirginOSPanel API sözleşme testlerinin ortak kütüphanesi.

TEST panelinin KENDİ ÜZERİNDE root olarak çalışır (gerçek müşteri sunucusunda ÇALIŞTIRMAYIN):
kapı portlarını /etc/girginospanel/kapi.json'dan okur, geçici API anahtarlarını panelin
veritabanına yalnız SHA-256 özetiyle ekler, istekleri 127.0.0.1 üzerinden kapılara yapar.

Kurallar (bu kütüphane uygular):
  - Sır (anahtar, JWT, parola, tek kullanımlık kod) hiçbir çıktıya yazılmaz; `maskele` her
    kanıt metnini geçirir.
  - Oluşturulan her nesnenin adı ÖNEK ile başlar (varsayılan "sozl"); `Temizlik` yalnız o
    önekli nesneleri siler ve kalıntı arar.
  - Bilerek başarısız girişler (401) rastgele bir 127.0.0.x kaynağından yapılır: panelin
    giriş kilidi (15 dk'da 5 adet 401 → 30 dk) olumlu denemeleri etkilemez.
Yalnız standart kütüphane (Python 3.6+).
"""
import base64
import hashlib
import http.client
import json
import os
import random
import re
import secrets
import ssl
import string
import subprocess
import sys
import time

KAPI_JSON = os.environ.get("GOSP_KAPI_JSON", "/etc/girginospanel/kapi.json")
DB_ADI = os.environ.get("GOSP_DB", "panel")
JETON_BETIGI = os.environ.get("GOSP_JETON_BETIGI", "/root/.e2e-jeton.py")
ONEK = os.environ.get("GOSP_ONEK", "sozl")
API = "/api/v1"


# ───────────────────────── yardımcılar ─────────────────────────

def sqlq(s):
    """MySQL dizge değişmezi (yalnız bizim ürettiğimiz değerler için; yine de kaçışlı)."""
    return "'" + str(s).replace("\\", "\\\\").replace("'", "\\'") + "'"


def mysql(q):
    r = subprocess.run(["mysql", DB_ADI, "-N", "-B", "-e", q], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                       universal_newlines=True)
    if r.returncode != 0:
        raise RuntimeError("mysql: " + r.stderr.strip()[:300])
    return r.stdout.strip()


def rastgele(n=6):
    return "".join(random.choice(string.ascii_lowercase + string.digits) for _ in range(n))


def guclu_parola():
    # harf + rakam + işaret; panel ve WHMCS kurallarının hepsini karşılar (16 karakter)
    return "Sz" + secrets.token_urlsafe(9).replace("-", "a").replace("_", "b") + "7q"


_SIRLAR = set()


def sir_kaydet(*degerler):
    for d in degerler:
        if d and len(str(d)) >= 6:
            _SIRLAR.add(str(d))


_DESENLER = [
    (re.compile(r"gosp_[a-z2-7]{12}_[A-Za-z0-9_\-]{20,}"), "gosp_***"),
    (re.compile(r"eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+"), "JWT***"),
    (re.compile(r"(#kod=|\"kod\":\s*\")[A-Za-z0-9_\-%]{20,}"), r"\1***"),
]


def maskele(s):
    s = "" if s is None else str(s)
    for d in sorted(_SIRLAR, key=len, reverse=True):
        s = s.replace(d, "***")
    for r, y in _DESENLER:
        s = r.sub(y, s)
    return s


def kisalt(s, n=220):
    s = maskele(s).replace("\n", " ")
    return s if len(s) <= n else s[:n] + "…"


# ───────────────────────── HTTP ─────────────────────────

class Yanit(object):
    def __init__(self, kod, govde, basliklar):
        self.kod = kod
        self.govde = govde
        self.basliklar = basliklar
        try:
            self.json = json.loads(govde) if govde else None
        except ValueError:
            self.json = None

    @property
    def liste(self):
        """JSON dizi yanıtı; hata gövdesi (nesne) ya da düz metinse boş liste."""
        return self.json if isinstance(self.json, list) else []

    @property
    def hata(self):
        return self.json.get("hata", "") if isinstance(self.json, dict) else ""

    @property
    def hata_kodu(self):
        return self.json.get("kod", "") if isinstance(self.json, dict) else ""

    def alan(self, *yol, varsayilan=None):
        v = self.json
        for k in yol:
            if isinstance(v, dict):
                v = v.get(k)
            elif isinstance(v, list) and isinstance(k, int) and k < len(v):
                v = v[k]
            else:
                return varsayilan
        return varsayilan if v is None else v

    def ozet(self):
        return "HTTP %d %s" % (self.kod, kisalt(self.hata or self.govde, 160))


class Panel(object):
    """Kapılara (admin/bayi/musteri) HTTPS istek; gözlemleri (hata yanıtları) kaydeder."""

    def __init__(self):
        k = json.load(open(KAPI_JSON))
        self.port = {"admin": int(k["admin"]), "bayi": int(k["bayi"]), "musteri": int(k["musteri"])}
        self.ctx = ssl._create_unverified_context()
        # Giriş kilidi IP başınadır (15 dk'da 5 adet 401 → 30 dk). 127.0.0.1 başka bir testin kilidinde
        # olabilir: olumlu (poz) ve bilerek başarısız (neg) denemeler AYRI, rastgele loopback adreslerinden.
        self.poz = "127.%d.%d.%d" % (random.randint(1, 250), random.randint(1, 250), random.randint(2, 250))
        self.neg = "127.%d.%d.%d" % (random.randint(1, 250), random.randint(1, 250), random.randint(2, 250))
        self.gozlemler = []  # {"desen","kod","hata","kapi"} — sızıntı taraması + belge karşılaştırması

    def istek(self, kapi, metod, yol, govde=None, yetki=None, kaynak=None, zaman=120, desen=None,
              ham=None, basliklar=None):
        h = {"Accept": "application/json"}
        if yetki:
            h["Authorization"] = yetki
        veri = None
        if ham is not None:
            veri = ham if isinstance(ham, bytes) else ham.encode()
            h["Content-Type"] = "application/json"
        elif govde is not None:
            veri = json.dumps(govde).encode()
            h["Content-Type"] = "application/json"
        if basliklar:
            h.update(basliklar)
        kaynak = kaynak or self.poz
        yol_tam = yol if yol.startswith("/api/") or yol.startswith("/healthz") else API + yol
        son = None
        for _ in range(2):
            try:
                c = http.client.HTTPSConnection("127.0.0.1", self.port[kapi], context=self.ctx, timeout=zaman,
                                                source_address=(kaynak, 0))
                c.request(metod, yol_tam, body=veri, headers=h)
                r = c.getresponse()
                y = Yanit(r.status, r.read().decode("utf-8", "replace"), {a.lower(): b for a, b in r.getheaders()})
                c.close()
                break
            except (OSError, http.client.HTTPException) as e:
                son = e
                time.sleep(1)
        else:
            y = Yanit(0, "bağlantı hatası: %s" % son, {})
        if y.kod >= 400 and desen:
            self.gozlemler.append({"desen": desen, "kod": y.kod, "hata": y.hata, "kapi": kapi,
                                   "duz": y.json is None, "govde": kisalt(y.govde, 300)})
        return y

    def site_kodu(self, alan, yol="/"):
        try:
            c = http.client.HTTPConnection("127.0.0.1", 80, timeout=20)
            c.request("GET", yol, headers={"Host": alan})
            r = c.getresponse()
            r.read()
            c.close()
            return r.status
        except (OSError, http.client.HTTPException):
            return 0

    def site_bekle(self, alan, beklenen, sure=15):
        k = 0
        for _ in range(sure):
            k = self.site_kodu(alan)
            if k == beklenen:
                return k
            time.sleep(1)
        return k


# ───────────────────────── anahtarlar ve oturumlar ─────────────────────────

class Anahtarlar(object):
    """Geçici API anahtarları: DB'ye yalnız SHA-256 özeti; tam değer yalnız bellekte."""

    def __init__(self, onek):
        self.onek = onek

    @staticmethod
    def _uret():
        onek = base64.b32encode(secrets.token_bytes(8)).decode().lower().rstrip("=")[:12]
        gizli = base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip("=")
        tam = "gosp_" + onek + "_" + gizli
        return tam, onek, hashlib.sha256(tam.encode()).hexdigest(), gizli[-4:]

    def ac(self, ad, sahip_kul, rol, izinler, ipler="127.0.0.0/8,::1", hiz=6000):
        """rol: 'admin' (kök) ya da 'reseller'. Dönen değer Authorization başlığı."""
        uid = mysql("SELECT id FROM users WHERE username=%s" % sqlq(sahip_kul))
        if not uid:
            raise RuntimeError("anahtar sahibi yok: " + sahip_kul)
        tam, onek, ozet, son4 = self._uret()
        mysql("INSERT INTO api_anahtarlari (sahip_id, sahip_rol, ad, onek, ozet, son4, izinler, izinli_ipler, hiz_dk, son_kullanma_at) "
              "VALUES (%d, %s, %s, %s, %s, %s, %s, %s, %d, NOW() + INTERVAL 1 DAY)"
              % (int(uid), sqlq(rol), sqlq(self.onek + "-" + ad), sqlq(onek), sqlq(ozet), sqlq(son4),
                 sqlq(",".join(izinler)), sqlq(ipler), int(hiz)))
        sir_kaydet(tam)
        return "gosp %s:%s" % (sahip_kul, tam)

    def temizle(self):
        mysql("DELETE FROM api_anahtarlari WHERE ad LIKE %s" % sqlq(self.onek + "-%"))


TUM_KOK = ["hesap:yaz", "plan:yaz", "ftp.parola:yaz", "db.parola:yaz", "sso:yaz", "dns:yaz", "veritabani:yaz",
           "ssl:yaz", "yedek:yaz", "bayi:yaz", "bayi.parola:yaz", "denetim:oku", "bildirim:oku"]
TUM_BAYI = ["hesap:yaz", "plan:yaz", "ftp.parola:yaz", "db.parola:yaz", "sso:yaz", "dns:yaz", "veritabani:yaz",
            "ssl:yaz", "yedek:yaz", "denetim:oku", "bildirim:oku"]


def admin_jwt():
    """İsteğe bağlı: test sunucusundaki jeton betiği varsa yönetici JWT'si (yoksa None → o testler atlanır)."""
    if not os.path.exists(JETON_BETIGI):
        return None
    r = subprocess.run([sys.executable, JETON_BETIGI], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                       universal_newlines=True)
    j = r.stdout.strip()
    if r.returncode != 0 or not j:
        return None
    sir_kaydet(j)
    return "Bearer " + j


# ───────────────────────── rapor ─────────────────────────

class Rapor(object):
    def __init__(self):
        self.testler = []
        self.bulgular = []
        self.alan = ""
        self.basla = time.time()

    def bolum(self, alan):
        self.alan = alan
        print("\n== %s" % alan, flush=True)

    def _ekle(self, sonuc, ad, kanit):
        self.testler.append({"alan": self.alan, "ad": ad, "sonuc": sonuc, "kanit": kisalt(kanit, 300)})
        isaret = {"gecti": "  OK  ", "dustu": "  FAIL", "atlandi": "  ATLA"}[sonuc]
        print("%s %s%s" % (isaret, ad, ("" if sonuc == "gecti" else "  — " + kisalt(kanit, 200))), flush=True)
        return sonuc == "gecti"

    def kontrol(self, ad, kosul, kanit=""):
        return self._ekle("gecti" if kosul else "dustu", ad, kanit)

    def atla(self, ad, neden):
        return self._ekle("atlandi", ad, neden)

    def kod(self, ad, y, beklenen, hata_icerir=None, hata_kodu=None):
        """y.kod beklenen (int ya da küme) mi; isteğe bağlı hata metni/kodu."""
        kume = {beklenen} if isinstance(beklenen, int) else set(beklenen)
        ok = y.kod in kume
        if ok and hata_icerir is not None:
            ok = hata_icerir in y.hata
        if ok and hata_kodu is not None:
            ok = y.hata_kodu == hata_kodu
        if y.kod == 429 and 429 not in kume:
            return self.atla(ad, "giriş/hız kilidi 429 — doğrulanamadı")
        return self.kontrol(ad, ok, y.ozet())

    def bulgu(self, onem, ozet, kanit=""):
        self.bulgular.append({"onem": onem, "alan": self.alan, "ozet": ozet, "kanit": kisalt(kanit, 400)})

    def ozet(self):
        say = {"gecti": 0, "dustu": 0, "atlandi": 0}
        for t in self.testler:
            say[t["sonuc"]] += 1
        return say

    def json(self, ortam, gozlemler, temizlik):
        return {"ortam": ortam, "ozet": self.ozet(), "sure_sn": int(time.time() - self.basla),
                "testler": self.testler, "bulgular": self.bulgular, "gozlemler": gozlemler, "temizlik": temizlik}


# ───────────────────────── bağlam (paylaşılan test nesneleri) ─────────────────────────

class Baglam(object):
    """Modüllerin paylaştığı nesneler: kök anahtar, test bayileri, hostingler. Tembel oluşturulur."""

    def __init__(self, onek=ONEK):
        self.p = Panel()
        self.r = Rapor()
        self.onek = onek
        self.kosu = rastgele(4)
        self.an = Anahtarlar(onek)
        self.kok = self.an.ac("kok", "root", "admin", TUM_KOK)
        self._paket = None
        self.bayiler = {}     # etiket -> {"id","kul","parola","jwt"}
        self.hostingler = {}  # etiket -> {"id","alan","sk","parola","sahip"}
        self.parolalar = {}

    # adlar
    def alan_adi(self, etiket):
        return "%s-%s-%s.test" % (self.onek, self.kosu, etiket)

    def kul_adi(self, etiket):
        # panel kullanıcı adını küçük harfe çevirir; karşılaştırmalar da küçük harfle
        return ("%s%s%s" % (self.onek, self.kosu, etiket)).lower()[:32]

    # kök API kısayolu
    def kok_api(self, metod, yol, govde=None, desen=None, zaman=120):
        return self.p.istek("admin", metod, yol, govde=govde, yetki=self.kok, desen=desen, zaman=zaman)

    def paket(self):
        if self._paket is None:
            y = self.kok_api("POST", "/reseller-plans", {"ad": "%s-%s-paket" % (self.onek, self.kosu), "max_domain": 10,
                                                         "max_disk_mb": 20000, "max_trafik_mb": 200000,
                                                         "cpu_cekirdek": 2, "ram_mb": 2048, "fazla_satis": True},
                             desen="POST /reseller-plans")
            if y.kod not in (200, 201):
                raise RuntimeError("test paketi açılamadı: " + y.ozet())
            self._paket = int(y.alan("id", varsayilan=0))
        return self._paket

    def bayi(self, etiket):
        """Test bayisi (paketten), parolası bilinen; bayi JWT'si ile birlikte."""
        if etiket in self.bayiler:
            return self.bayiler[etiket]
        kul = self.kul_adi("b" + etiket)
        pw = guclu_parola()
        sir_kaydet(pw)
        y = self.kok_api("POST", "/resellers", {"kullanici": kul, "parola": pw, "paket_id": self.paket(),
                                                 "ad_soyad": "Sozlesme %s" % etiket}, desen="POST /resellers")
        if y.kod not in (200, 201):
            raise RuntimeError("test bayisi açılamadı: " + y.ozet())
        b = {"id": int(y.alan("id")), "kul": kul, "parola": pw}
        b["jwt"] = self.bayi_giris(b)
        if not b["jwt"]:
            self.bayiler[etiket] = b  # temizlik bulsun
            raise RuntimeError("test bayisi oturum açamadı (%s)" % kul)
        self.bayiler[etiket] = b
        return b

    def bayi_giris(self, b, parola=None, kaynak=None):
        y = self.p.istek("bayi", "POST", "/auth/login", {"kullanici": b["kul"], "parola": parola or b["parola"]},
                         kaynak=kaynak, desen="POST /auth/login")
        tok = y.alan("token", varsayilan="")
        if tok:
            sir_kaydet(tok)
            return "Bearer " + tok
        return None

    def hosting(self, etiket, sahip=None, govde_ek=None):
        """Hosting aç: sahip None → kök anahtar (admin kapısı), yoksa bayi etiketi (bayi JWT, bayi kapısı)."""
        if etiket in self.hostingler:
            return self.hostingler[etiket]
        alan = self.alan_adi(etiket)
        govde = {"alan_adi": alan}
        govde.update(govde_ek or {})
        if sahip is None:
            y = self.kok_api("POST", "/domains", govde, desen="POST /domains", zaman=240)
        else:
            b = self.bayi(sahip)
            y = self.p.istek("bayi", "POST", "/domains", govde, yetki=b["jwt"], desen="POST /domains", zaman=240)
        if y.kod != 201:
            raise RuntimeError("hosting açılamadı (%s): %s" % (alan, y.ozet()))
        pw = guclu_parola()
        sir_kaydet(pw, y.alan("olusturulan_parolalar", "ftp", varsayilan=""), y.alan("olusturulan_parolalar", "db", varsayilan=""))
        h = {"id": int(y.alan("id")), "alan": alan, "sk": y.alan("sistem_kullanici", varsayilan=""), "parola": pw,
             "sahip": sahip}
        # parolayı bilinen bir değere çek (müşteri girişi için)
        yetki = self.kok
        k = self.p.istek("admin", "PUT", "/domains/%d/ftp/password" % h["id"], {"password": pw}, yetki=yetki,
                         desen="PUT /domains/{id}/ftp/password")
        if k.kod != 200:
            raise RuntimeError("hosting parolası ayarlanamadı: " + k.ozet())
        self.hostingler[etiket] = h
        return h

    def musteri_giris(self, h, parola=None, kaynak=None):
        return self.p.istek("musteri", "POST", "/musteri/login", {"kullanici": h["sk"], "parola": parola or h["parola"]},
                            kaynak=kaynak, desen="POST /musteri/login")


# ───────────────────────── temizlik ve kalıntı ─────────────────────────

def temizle(onek=ONEK, yetki=None, p=None, bekle=180):
    """Önekli test nesnelerini siler (hostingler, bayiler — arka plan işi beklenir —, bayi paketleri,
    anahtarlar) ve kalıntı raporu döner. Önceki yarım kalmış koşuları da toplar."""
    p = p or Panel()
    an = Anahtarlar(onek)
    kendi = None
    if yetki is None:
        kendi = an.ac("temizlik", "root", "admin", TUM_KOK)
        yetki = kendi
    sonuc = {"silinen_hosting": 0, "silinen_bayi": 0, "silinen_paket": 0, "hatalar": []}
    y = p.istek("admin", "GET", "/domains", yetki=yetki)
    for d in (y.liste):
        a = str(d.get("alan_adi", ""))
        if a.startswith(onek + "-"):
            s = p.istek("admin", "DELETE", "/domains/%s?onay=%s" % (d["id"], a), yetki=yetki, zaman=240)
            if s.kod == 200:
                sonuc["silinen_hosting"] += 1
            else:
                sonuc["hatalar"].append("hosting %s: %s" % (a, s.ozet()))
    def bayi_kimlikleri():
        j = p.istek("admin", "GET", "/resellers", yetki=yetki).json
        return {int(b["id"]): str(b.get("kullanici", "")) for b in (j if isinstance(j, list) else [])}

    son = time.time() + bekle
    bekleyen = []
    for bid, kul in sorted(bayi_kimlikleri().items()):
        if not kul.startswith(onek):
            continue
        # Panel aynı anda tek toplu iş çalıştırır: bir bayinin silme işi bitmeden ötekine 409 döner.
        while time.time() < son:
            s = p.istek("admin", "DELETE", "/resellers/%d" % bid, yetki=yetki)
            if s.kod == 409:
                time.sleep(3)
                continue
            break
        if s.kod not in (200, 202):
            sonuc["hatalar"].append("bayi %s: %s" % (kul, s.ozet()))
            continue
        while bid in bayi_kimlikleri() and time.time() < son:
            time.sleep(2)
        if bid in bayi_kimlikleri():
            bekleyen.append(bid)
    if bekleyen:
        sonuc["hatalar"].append("silme işi bitmeyen bayiler: %s" % bekleyen)
    y = p.istek("admin", "GET", "/reseller-plans", yetki=yetki)
    for k in (y.liste):
        if str(k.get("ad", "")).startswith(onek + "-"):
            s = p.istek("admin", "DELETE", "/reseller-plans/%s" % k["id"], yetki=yetki)
            if s.kod == 200:
                sonuc["silinen_paket"] += 1
            else:
                sonuc["hatalar"].append("paket %s: %s" % (k.get("ad"), s.ozet()))
    an.temizle()
    sonuc["silinen_bayi"] = "tamam" if not bekleyen else "eksik"
    sonuc["kalinti"] = kalinti(onek)
    return sonuc


def kalinti(onek=ONEK):
    """Önekli nesnelerden geride kalan (DB, sistem kullanıcısı, home, nginx, php-fpm) — boş sözlük = temiz."""
    k = {}
    sorgular = {
        "domains": "SELECT COUNT(*) FROM domains WHERE alan_adi LIKE %s" % sqlq(onek + "-%"),
        "users": "SELECT COUNT(*) FROM users WHERE username LIKE %s" % sqlq(onek + "%"),
        "reseller_plans": "SELECT COUNT(*) FROM reseller_plans WHERE ad LIKE %s" % sqlq(onek + "-%"),
        "api_anahtarlari": "SELECT COUNT(*) FROM api_anahtarlari WHERE ad LIKE %s" % sqlq(onek + "-%"),
    }
    for ad, q in sorgular.items():
        try:
            n = int(mysql(q) or 0)
        except (RuntimeError, ValueError):
            n = -1
        if n:
            k[ad] = n
    sk = "c_" + onek
    try:
        ge = subprocess.run(["getent", "passwd"], stdout=subprocess.PIPE, universal_newlines=True).stdout
        n = sum(1 for s in ge.splitlines() if s.startswith(sk))
        if n:
            k["sistem_kullanicisi"] = n
    except OSError:
        pass
    try:
        n = sum(1 for d in os.listdir("/home") if d.startswith(sk))
        if n:
            k["home"] = n
    except OSError:
        pass
    n = 0
    for dz in ("/etc/nginx/conf.d",):
        try:
            n += sum(1 for f in os.listdir(dz) if onek + "-" in f or ("c_" + onek) in f)
        except OSError:
            pass
    if n:
        k["nginx"] = n
    return k


def ftp_giris(kullanici, parola, host="127.0.0.1"):
    """FTP girişi dener: True = giriş başarılı (açık metin; yalnız yerel test için)."""
    import ftplib
    try:
        f = ftplib.FTP(host, timeout=15)
        f.login(kullanici, parola)
        f.quit()
        return True
    except Exception:  # noqa: BLE001 — reddi/bağlantı hatası ayırt edilmez
        return False


def ortam():
    o = {"surum": "", "ikili_sha16": ""}
    try:
        o["ikili_sha16"] = hashlib.sha256(open("/opt/girginospanel/bin/girginospanel-server", "rb").read()).hexdigest()[:16]
    except OSError:
        pass
    try:
        p = Panel()
        y = p.istek("admin", "GET", "/healthz")
        o["surum"] = y.alan("surum", varsayilan="")
    except Exception:  # noqa: BLE001 — yalnız bilgi
        pass
    return o
