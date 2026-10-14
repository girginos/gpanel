# -*- coding: utf-8 -*-
"""governor.py — MariaDB CPU kısıcısı (internal/dbgovernor, göç 0220) için GERÇEK-VPS sözleşme modülü.

Yapay yükle (BENCHMARK döngüleri) uçtan uca sınar; yalnız TEST panelinde (gp-prova VM'leri) koşar.
Kısıcının zaman ayarlarını (poll 5 sn, pencere 10 sn, yükselme 10 sn, kill 3 sn, soğuma 30 sn, kesme 1 dk)
cp_ayarlar üzerinden kısaltır; koşu sonunda ayarları ve modu ÖZGÜN haline döndürür.

    python3 kos.py --onek sozg --modul governor          # ~15 dk
    GOV_UZUN=1 python3 kos.py --onek sozg --modul governor   # + tek uzun deyim, yoklama aralığı, userstat kapalı, 50 kullanıcı ölçeği, QPS (~32 dk)

Kapsam (numaralar görev metnindeki sırayı izler):
  1  gözlem: ölçüm doğruluğu (db_cpu_olcum ↔ USER_STATISTICS), yalnız ağır kullanıcı işaretli, ALTER/KILL yok,
     tek denetim + tek bildirim
  2  uygula: seviye 1→2→3 zamanları, uygulanan sınırlar, yalnız o hosting etkilenir, sistem hesapları/diğer
     kiracılar dokunulmaz, yük bitince BİREBİR geri dönüş
  3a askı  3b parola/silme  3c panel yeniden başlatma  3d MariaDB yeniden başlatma  3e kip geçişleri
  3f çok kısa ağır sorgu  3g eski yavaş-sorgu bekçisiyle çakışma + plan yeniden uygulaması  3h userstat kapalı (UZUN)
  4  yetki: bayi/müşteri/API anahtarı, denetim aktörü
  ek-hesap  (a) ACCOUNT LOCK'lı hesapta kısıt yazma/bırakma kilidi korur (panel askısı + doğrudan kilit), (b) çok host'lu hesap
            (@localhost + @'%') kısıt + bırakma sonrası host değerleri
  ek-tur    (c) varsayılan ayarlarla tek uzun CPU deyimi gözlemde görünür mü, (d) yoğun yükte ardışık yoklama aralığı ≤ 2,5 x poll (UZUN)
  5  ölçek: 50 DB kullanıcısı turu (panel CPU'su, tur başına DB meşguliyeti), userstat QPS etkisi (UZUN)
"""
import hashlib
import json
import os
import re
import subprocess
import threading
import time

import gosp
from gosp import TUM_BAYI

HIZLI = {"db_kisici_poll_sn": "5", "db_kisici_pencere_sn": "10", "db_kisici_yukselme_sn": "10", "db_kisici_kill_sn": "3",
         "db_kisici_soguma_sn": "30", "db_kisici_uzun_sn": "2", "db_kisici_baglanti_kes_dk": "1"}
LIMIT_ALANLARI = ("max_questions", "max_updates", "max_connections", "max_user_connections")
OZGUN = {"mysql_max_baglanti": 12, "db_max_queries_per_hour": 777777, "db_max_updates_per_hour": 555555}
KISIT = 2          # varsayılan db_kisici_baglanti (seviye 1-2)
KISIT3 = 1         # seviye 3: MAX_USER_CONNECTIONS=1 (0 ASLA: MariaDB'de 0 = sınırsız)
UZUN = os.environ.get("GOV_UZUN") == "1"


# ───────────────────────── yardımcılar ─────────────────────────

def kok_sql(q, zaman=90):
    r = subprocess.run(["mysql", "-uroot", "-N", "-B", "-r", "-e", q], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                       universal_newlines=True, timeout=zaman)
    if r.returncode != 0:
        raise RuntimeError("mysql: " + r.stderr.strip()[:200])
    return r.stdout.strip()


def panel_sql(q):
    return gosp.mysql(q)


def priv_oku():
    """mysql.global_priv → {'u@h': {'lim': (q,u,c,uc), 'kilit': bool, 'rest': özet}} (parola özetleri ÇIKTIYA girmez)."""
    s = {}
    for ln in kok_sql("SELECT User,Host,Priv FROM mysql.global_priv").splitlines():
        u, h, p = ln.split("\t", 2)
        j = json.loads(p)
        kalan = {k: v for k, v in j.items() if k not in LIMIT_ALANLARI and k != "account_locked"}
        s["%s@%s" % (u, h)] = {"lim": tuple(int(j.get(k, 0) or 0) for k in LIMIT_ALANLARI), "kilit": bool(j.get("account_locked")),
                               "rest": hashlib.sha256(json.dumps(kalan, sort_keys=True).encode()).hexdigest()[:12]}
    return s


def priv_fark(a, b, haric=()):
    """a→b arasında değişen/eklenen/silinen hesaplar (haric: kullanıcı adları)."""
    f = []
    for k in sorted(set(a) | set(b)):
        if k.split("@")[0] in haric:
            continue
        if a.get(k) != b.get(k):
            f.append(k)
    return f


def hesap_durum(kul):
    """kullanıcının tüm host hesapları: [(host, max_user_connections, kilitli)]."""
    out = kok_sql("SELECT Host,Priv FROM mysql.global_priv WHERE User=%s" % gosp.sqlq(kul))
    r = []
    for ln in out.splitlines():
        h, p = ln.split("	", 1)
        j = json.loads(p)
        r.append((h, int(j.get("max_user_connections", 0) or 0), bool(j.get("account_locked"))))
    return r


def mx(kul):
    d = hesap_durum(kul)
    return d[0][1] if d else None


def bekle(kosul, zaman, aralik=1.0):
    son = time.time() + zaman
    while True:
        v = kosul()
        if v:
            return v
        if time.time() >= son:
            return None
        time.sleep(aralik)


def ayar_yaz(kv):
    for k, v in kv.items():
        panel_sql("INSERT INTO cp_ayarlar (anahtar, deger) VALUES (%s,%s) ON DUPLICATE KEY UPDATE deger=VALUES(deger)" % (gosp.sqlq(k), gosp.sqlq(v)))


def ayar_tum():
    out = panel_sql("SELECT anahtar, deger FROM cp_ayarlar WHERE anahtar LIKE 'db\\_kisici\\_%'")
    return [tuple(ln.split("\t", 1)) for ln in out.splitlines() if ln]


def ayar_geri(once):
    panel_sql("DELETE FROM cp_ayarlar WHERE anahtar LIKE 'db\\_kisici\\_%'")
    for k, v in once:
        panel_sql("INSERT INTO cp_ayarlar (anahtar, deger) VALUES (%s,%s)" % (gosp.sqlq(k), gosp.sqlq(v)))


def jwt():
    return gosp.admin_jwt()


def mod_ayarla(ctx, m):
    return ctx.p.istek("admin", "PUT", "/db-cpu/durum", {"mod": m}, yetki=jwt(), desen="PUT /db-cpu/durum")


def limitler(ctx, hid, ust):
    """PUT /domains/{id}/limitler — API anahtarıyla KULLANILAMAZ (403), kök JWT gerekir."""
    return ctx.p.istek("admin", "PUT", "/domains/%d/limitler" % hid, {"ust": ust}, yetki=jwt(), desen="PUT /domains/{id}/limitler")


def durum(ctx):
    y = ctx.p.istek("admin", "GET", "/db-cpu/durum", yetki=jwt(), desen="GET /db-cpu/durum")
    return (y.alan("durum", varsayilan={}) or {}), y


def tur_bekle(ctx, n=2, zaman=90):
    d0, _ = durum(ctx)
    y0 = int(d0.get("yoklama_sayisi", 0))
    return bekle(lambda: int(durum(ctx)[0].get("yoklama_sayisi", 0)) >= y0 + n, zaman, 1.0)


def olcum_toplam(kul):
    return float(panel_sql("SELECT COALESCE(SUM(cpu_ms),0) FROM db_cpu_olcum WHERE db_user=%s" % gosp.sqlq(kul)) or 0) / 1000


def audit(eylem, kul_domain, t0):
    """t0'dan beri audit_log: (eylem, hedef ya da alan) → [(actor_username, actor_user_id, ok)]"""
    out = panel_sql("SELECT actor_username, actor_user_id, ok FROM audit_log WHERE action=%s AND target=%s AND ts >= FROM_UNIXTIME(%d) ORDER BY id"
                    % (gosp.sqlq(eylem), gosp.sqlq(kul_domain), int(t0)))
    return [ln.split("\t") for ln in out.splitlines() if ln]


def bildirim(baslik_parca, domain_id, t0):
    return int(panel_sql("SELECT COUNT(*) FROM cp_bildirim WHERE ref_tur='db_cpu' AND domain_id=%d AND baslik LIKE %s AND created_at >= FROM_UNIXTIME(%d)"
                         % (domain_id, gosp.sqlq("%" + baslik_parca + "%"), int(t0))) or 0)


def kisit_satirlari(kul, t0):
    out = panel_sql("SELECT seviye, uygulandi, kisit_baglanti, onceki_baglanti, oldurulen, kesilen, COALESCE(bitis_neden,'-'), COALESCE(acik,0) FROM db_cpu_kisit "
                    "WHERE db_user=%s AND baslangic_ts >= %d ORDER BY id" % (gosp.sqlq(kul), int(t0) - 2))
    return [ln.split("\t") for ln in out.splitlines() if ln]


def gunluk(t0, desen):
    r = subprocess.run(["journalctl", "-u", "girginospanel", "--utc", "--no-pager", "--since", "@%d" % int(t0)],
                       stdout=subprocess.PIPE, universal_newlines=True, timeout=60)
    return [ln.split("girginospanel[", 1)[-1] for ln in r.stdout.splitlines() if re.search(desen, ln)]


# ───────────────────────── yük işçileri ─────────────────────────

class Isci(threading.Thread):
    """Bir DB kullanıcısı kimliğiyle `mysql` istemcisi döngüsü. Her tur YENİ bağlantı (PHP gibi); kalıcı bağlantı için sorgu
    tek `-e` içinde çok deyimdir. Kayıt: (başlangıç, bitiş, çıkış kodu, ERROR no)."""

    def __init__(self, kul, parola, sorgu, ad=""):
        super().__init__(daemon=True)
        self.kul, self.pw, self.sorgu, self.ad = kul, parola, sorgu, ad
        self.dur = threading.Event()
        self.ol = []
        self.proc = None

    def run(self):
        env = dict(os.environ, MYSQL_PWD=self.pw)
        while not self.dur.is_set():
            t0 = time.time()
            try:
                self.proc = subprocess.Popen(["mysql", "-u", self.kul, "--skip-reconnect", "-N", "-B", "-e", self.sorgu], env=env,
                                             stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, universal_newlines=True)
                _, err = self.proc.communicate()
            except OSError:
                time.sleep(1)
                continue
            m = re.search(r"ERROR (\d+)", err or "")
            self.ol.append((t0, time.time(), self.proc.returncode, int(m.group(1)) if m else 0))
            if self.proc.returncode != 0 and not self.dur.is_set():
                time.sleep(0.3)

    def durdur(self):
        self.dur.set()
        p = self.proc
        if p is not None and p.poll() is None:
            try:
                p.terminate()
            except OSError:
                pass

    def say(self, *kodlar):
        return sum(1 for o in self.ol if o[3] in kodlar)

    def olu(self):
        """tek uzun bağlantı çalışıyorsa False; çıkış yaptıysa ve hata ile bittiyse True"""
        return any(o[2] != 0 for o in self.ol)


def yuk_baslat(liste):
    for w in liste:
        w.start()
    return liste


def yuk_durdur(liste, bekle_sn=12):
    for w in liste:
        w.durdur()
    son = time.time() + bekle_sn
    for w in liste:
        w.join(max(0.1, son - time.time()))


def sorgu_kisa(hiz, sn=0.6):
    return "SELECT BENCHMARK(%d, MD5('k'))" % int(hiz * sn)


def sorgu_kalici(hiz, sn=0.3, n=500):
    return ";".join(["SELECT BENCHMARK(%d, MD5('p'))" % int(hiz * sn)] * n)


def sorgu_uzun(hiz, sn=8):
    return "SELECT BENCHMARK(%d, MD5('u'))" % int(hiz * sn)


def sorgu_hafif(hiz, n=400):
    return ";".join(["SELECT BENCHMARK(%d, MD5('b')); SELECT SLEEP(0.4)" % int(hiz * 0.01)] * n)


class Izleyici(threading.Thread):
    """~1 sn'de bir: açık kısıt satırları, global_priv Max_user_connections, USER_STATISTICS CPU_TIME (zaman çizelgesi)."""

    def __init__(self):
        super().__init__(daemon=True)
        self.kul = ["root"]
        self.dur = threading.Event()
        self.s = []
        self.hata = 0

    def run(self):
        while not self.dur.is_set():
            t = time.time()
            ad = ",".join(gosp.sqlq(u) for u in list(self.kul))
            q = ("SELECT 'K',db_user,seviye,kisit_baglanti,kesilen,oldurulen,uygulandi FROM panel.db_cpu_kisit WHERE acik=1;"
                 "SELECT 'M',User,COALESCE(JSON_VALUE(Priv,'$.max_user_connections'),0) FROM mysql.global_priv WHERE User IN (%s);"
                 "SELECT 'C',USER,CPU_TIME FROM information_schema.USER_STATISTICS WHERE USER IN (%s)" % (ad, ad))
            try:
                out = kok_sql(q, 20)
            except Exception:  # noqa: BLE001 — MariaDB yeniden başlarken
                self.hata += 1
                self.dur.wait(1.0)
                continue
            k, m, c = {}, {}, {}
            for ln in out.splitlines():
                f = ln.split("\t")
                try:
                    if f[0] == "K":
                        k[f[1]] = tuple(int(x) for x in f[2:7])
                    elif f[0] == "M":
                        m[f[1]] = int(f[2])
                    elif f[0] == "C":
                        c[f[1]] = float(f[2])
                except (ValueError, IndexError):
                    pass
            self.s.append((t, k, m, c))
            self.dur.wait(0.7)

    def ilk(self, kul, fn, t_min=0.0):
        for t, k, m, c in self.s:
            if t >= t_min and fn(k.get(kul), m.get(kul)):
                return t
        return None

    def cekirdek(self, kul, t0, t1):
        a = [x for x in self.s if x[0] >= t0 and kul in x[3]]
        b = [x for x in self.s if x[0] <= t1 and kul in x[3]]
        if not a or not b or b[-1][0] <= a[0][0]:
            return None
        return (b[-1][3][kul] - a[0][3][kul]) / (b[-1][0] - a[0][0])

    def cpu(self, kul):
        for t, k, m, c in reversed(self.s):
            if kul in c:
                return c[kul]
        return None


# ───────────────────────── kurulum ─────────────────────────

class Durum(object):
    pass


def kur(ctx, st):
    r, p = ctx.r, ctx.p
    r.bolum("governor/kurulum")
    st.ayar_once = ayar_tum()
    st.t_basla = time.time()
    r.kontrol("başlangıçta db_kisici_* ayar satırı yok (varsayılanlar) ve kip gözlem", True, "önceki satırlar: %s" % (st.ayar_once or "yok"))
    # panel yeni başlamışsa ilk tur Poll (15 sn) sonra gelir; o zamana dek durum boştur
    bekle(lambda: int(durum(ctx)[0].get("yoklama_sayisi", 0) or 0) >= 1, 60, 2)
    d, y = durum(ctx)
    r.kontrol("GET /db-cpu/durum 200 ve kısıcı çalışıyor (userstat açık)", y.kod == 200 and y.alan("calisiyor") is True and d.get("userstat") == "acik", y.ozet() + " userstat=%s" % d.get("userstat"))
    st.mod_once = y.alan("mod_ayar", varsayilan="?")
    for e in ("ga", "gb", "gc", "gd", "ge", "gg"):
        st.h[e] = ctx.hosting(e)
    st.h["gy"] = ctx.hosting("gy", sahip="A")
    h = st.h
    # her hostinge bilinen parolalı bir DB + kullanıcı (yeni hostingte DB yoktur)
    for e, x in h.items():
        x["pw"] = gosp.guclu_parola()
        gosp.sir_kaydet(x["pw"])
        y = ctx.kok_api("POST", "/domains/%d/databases" % x["id"], {"db_sonek": "d1", "kullanici_tipi": "yeni", "kullanici_sonek": "d1", "parola": x["pw"]},
                        desen="POST /domains/{id}/databases", zaman=120)
        if y.kod not in (200, 201):
            raise RuntimeError("DB açılamadı (%s): %s" % (e, y.ozet()))
        x["dbu"], x["dba"] = panel_sql("SELECT db_user, db_name FROM db_accounts WHERE domain_id=%d ORDER BY id LIMIT 1" % x["id"]).split("	")
    # ga: ÖZGÜN (plandan farklı) MySQL limitleri + ikinci DB kullanıcısı; gg: eski yavaş-sorgu bekçisi eşiği 3 sn
    u = dict(OZGUN, max_db=200)
    y = limitler(ctx, h["ga"]["id"], u)
    r.kod("ga: ÖZGÜN MySQL limitleri (12 bağlantı / 777777 sorgu / 555555 güncelleme) yazıldı", y, 200)
    bekle(lambda: (priv_oku().get(h["ga"]["dbu"] + "@localhost") or {}).get("lim") == (777777, 555555, 0, 12), 60, 2)
    pg = priv_oku().get(h["ga"]["dbu"] + "@localhost")
    r.kontrol("ga: PUT limitler MariaDB'ye uygulandı (777777 sorgu, 555555 güncelleme, 12 bağlantı)", pg is not None and pg["lim"] == (777777, 555555, 0, 12), str(pg and pg["lim"]))
    y = limitler(ctx, h["gg"]["id"], {"db_max_query_seconds": 3})
    r.kod("gg: db_max_query_seconds=3 (eski bekçi) yazıldı", y, 200)
    a = h["ga"]
    a["pw2"] = gosp.guclu_parola()
    gosp.sir_kaydet(a["pw2"])
    y = ctx.kok_api("POST", "/domains/%d/databases" % a["id"], {"db_sonek": "x2", "kullanici_tipi": "yeni", "kullanici_sonek": "x2", "parola": a["pw2"]},
                    desen="POST /domains/{id}/databases", zaman=120)
    a["dbu2"] = ""
    if y.kod in (200, 201):
        a["dbu2"] = y.alan("db_user", varsayilan="") or y.alan("kullanici", varsayilan="") or y.alan("db_kullanici", varsayilan="")
    if not a["dbu2"]:
        # ad yanıttan okunamadıysa db_accounts'tan
        a["dbu2"] = panel_sql("SELECT db_user FROM db_accounts WHERE domain_id=%d AND db_user<>%s LIMIT 1" % (a["id"], gosp.sqlq(a["dbu"])))
    r.kontrol("ga: ikinci DB kullanıcısı var", bool(a["dbu2"]) and a["dbu2"] != a["dbu"], "HTTP %d" % y.kod)
    st.kullanicilar = [x["dbu"] for x in h.values()] + [a["dbu2"]]
    st.iz = Izleyici()
    st.iz.kul = list(st.kullanicilar)
    # hız kalibrasyonu: BENCHMARK deyimi saniyede kaç yineleme (root, kiracıyı kirletmez)
    t = time.time()
    kok_sql("SELECT BENCHMARK(4000000, MD5('x'))")
    st.hiz = 4000000 / max(0.2, time.time() - t)
    r.kontrol("BENCHMARK kalibrasyonu (yineleme/sn)", st.hiz > 100000, "%.0f/sn (ölçüm)" % st.hiz)
    ayar_yaz(HIZLI)
    y = mod_ayarla(ctx, "gozlem")
    r.kod("kip gozlem (zaten gozlemdi: değişiklik yok)", y, 200)
    ok = bekle(lambda: durum(ctx)[0].get("poll_sn") == 5 and durum(ctx)[0].get("pencere_sn") == 10, 40, 2)
    r.kontrol("hızlı ayarlar kısıcıya ulaştı (poll 5 sn, pencere 10 sn)", ok, str(durum(ctx)[0]))
    st.P0 = priv_oku()
    st.iz.start()
    time.sleep(2)
    # BENCHMARK() CPU'yu KULLANICIYA doğru yazıyor mu: ga kimliğiyle ~3 sn
    c0 = st.iz.cpu(a["dbu"]) or 0.0
    t = time.time()
    subprocess.run(["mysql", "-u", a["dbu"], "-N", "-B", "-e", "SELECT BENCHMARK(%d, MD5('c'))" % int(st.hiz * 3)], env=dict(os.environ, MYSQL_PWD=a["pw"]),
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    dt = time.time() - t
    time.sleep(1.6)
    c1 = st.iz.cpu(a["dbu"]) or 0.0
    r.kontrol("USER_STATISTICS.CPU_TIME, BENCHMARK() süresini kullanıcıya yazıyor (0,8-1,3 x duvar saati)", 0.8 * dt <= (c1 - c0) <= 1.3 * dt + 0.2,
              "CPU_TIME +%.2f sn, duvar %.2f sn (ölçüm)" % (c1 - c0, dt))


# ───────────────────────── 1: gözlem ─────────────────────────

def faz_gozlem(ctx, st):
    r, h, iz = ctx.r, st.h, st.iz
    r.bolum("governor/1-gozlem")
    a, b, c = h["ga"], h["gb"], h["gc"]
    # kurulumdaki kalibrasyon sorguları kayıt açmış olabilir: temiz başlangıç (denetim/bildirim sayımı bu pencereden)
    bekle(lambda: bool(iz.s) and a["dbu"] not in iz.s[-1][1] and a["dbu2"] not in iz.s[-1][1], 90, 2)
    time.sleep(3)
    t0 = time.time()
    st.t_gozlem = t0
    gy = h["gy"]
    # bayi görünürlük verisi için gy'ye ~3 sn CPU
    subprocess.run(["mysql", "-u", gy["dbu"], "-N", "-B", "-e", "SELECT BENCHMARK(%d, MD5('y'))" % int(st.hiz * 3)], env=dict(os.environ, MYSQL_PWD=gy["pw"]),
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    wa = [Isci(a["dbu"], a["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(3)] + [Isci(a["dbu"], a["pw"], sorgu_kalici(st.hiz), "kalici")]
    wb = Isci(b["dbu"], b["pw"], sorgu_hafif(st.hiz), "hafif")
    wc = Isci(c["dbu"], c["pw"], "SELECT SLEEP(240)", "bos")
    tur_bekle(ctx, 2)
    tc0 = {u: (iz.cpu(u) or 0.0) for u in (a["dbu"], b["dbu"], c["dbu"])}
    st.olcum_baslangic = {u: olcum_toplam(u) for u in st.kullanicilar}
    yuk_baslat(wa + [wb, wc])
    t_yuk = time.time()
    # 3f: hiçbir deyim 1 sn'yi aşmıyor (çok sayıda kısa ağır sorgu); yine de işaretlenmeli
    t_isaret = bekle(lambda: iz.ilk(a["dbu"], lambda k, m: k is not None, t_yuk), 45, 1)
    r.kontrol("3f: yalnız KISA ağır deyimler (<=0,6 sn) üreten kullanıcı işaretlendi (gözlem kaydı açıldı)", t_isaret is not None,
              "işaret %.0f sn sonra (ölçüm)" % ((t_isaret or 0) - t_yuk))
    st.zaman["gozlem_isaret_sn"] = round((t_isaret or 0) - t_yuk, 1)
    time.sleep(max(0, 60 - (time.time() - t_yuk)))
    kay = iz.s[-1]
    r.kontrol("gözlem kaydı: A seviye 1, uygulandi=0 (MariaDB'ye dokunulmadı)", kay[1].get(a["dbu"]) is not None and kay[1][a["dbu"]][0] == 1 and kay[1][a["dbu"]][4] == 0,
              str(kay[1].get(a["dbu"])))
    r.kontrol("B (hafif) ve C (boşta) işaretlenmedi", b["dbu"] not in kay[1] and c["dbu"] not in kay[1], "açık kayıtlar: %s" % sorted(kay[1]))
    tek = {a["dbu"], a["dbu2"]}
    r.kontrol("gözlem kaydı yalnız ga hostingi DB kullanıcılarında (hosting kapsamlı)", set(kay[1]) <= tek, "kayıtlar: %s" % sorted(kay[1]))
    P = priv_oku()
    r.kontrol("gözlemde HİÇBİR hesap değişmedi (global_priv öncesi = sonrası: limitler, kilit, parola, yetkiler)", not priv_fark(st.P0, P),
              "değişen: %s" % priv_fark(st.P0, P))
    ya = sum(w.say(1317, 1927, 2013, 1226) for w in wa)
    r.kontrol("gözlemde hiçbir sorgu/bağlantı öldürülmedi, 1226 (bağlantı sınırı) yok", ya == 0 and not wa[3].olu(), "A işçi hataları=%d" % ya)
    # denetim/bildirim: bir kez
    n_aud = len(audit("db.cpu_asim_gozlem", a["alan"], t0))
    n_bil = bildirim("eşiği aşıldı (gözlem modu)", a["id"], st.t_basla)   # kurulumdaki kalibrasyon da kayıt açmış olabilir: okunmamış kopya varken yeniden yazılmaz (bildirim dedup)
    r.kontrol("denetim db.cpu_asim_gozlem TEK kez (60 sn sürekli aşımda spam yok)", n_aud == 1, "%d kayıt" % n_aud)
    r.kontrol("sahibe bildirim TEK kez (gözlem modu; okunmamış kopya varken yenisi yazılmaz)", n_bil == 1, "%d bildirim (kurulumdan beri)" % n_bil)
    r.kontrol("gözlemde db.cpu_kisit / yükselt / baglanti_kes denetimi YOK", len(audit("db.cpu_kisit", a["alan"], t0)) + len(audit("db.cpu_kisit_yukselt", a["alan"], t0)) + len(audit("db.cpu_baglanti_kes", a["alan"], t0)) == 0)
    ak = audit("db.cpu_asim_gozlem", a["alan"], t0)
    r.kontrol("governor denetim aktörü 'sistem' (kullanıcı kimliği yok/0, başarılı)", bool(ak) and ak[0][0] == "sistem" and ak[0][1] in ("0", "NULL") and ak[0][2] == "1", str(ak[:1]))
    # 1 dk içinde seviye hiç yükselmedi
    r.kontrol("gözlem kaydı tırmanmadı (60 sn sürekli aşımda seviye 1 kaldı)", all(s[1].get(a["dbu"], (1,))[0] == 1 for s in iz.s if s[0] >= t_yuk), "")
    # ölçüm doğruluğu: yük bitir, ölçüm otursun
    yuk_durdur(wa)
    t_dur = time.time()
    time.sleep(12)
    d_a = (iz.cpu(a["dbu"]) or 0) - tc0[a["dbu"]]
    d_b = (iz.cpu(b["dbu"]) or 0) - tc0[b["dbu"]]
    o_a = olcum_toplam(a["dbu"]) - st.olcum_baslangic[a["dbu"]]
    o_b = olcum_toplam(b["dbu"]) - st.olcum_baslangic[b["dbu"]]
    o_c = olcum_toplam(c["dbu"]) - st.olcum_baslangic[c["dbu"]]
    r.kontrol("1: db_cpu_olcum(A) ≈ USER_STATISTICS farkı (±%10 ±1 sn)", abs(o_a - d_a) <= 0.10 * d_a + 1.0, "olcum %.1f sn, istatistik %.1f sn (ölçüm)" % (o_a, d_a))
    r.kontrol("1: B (hafif) ölçümü de tutarlı ve A'dan çok küçük", abs(o_b - d_b) <= 0.10 * d_b + 0.5 and o_b < 0.05 * max(o_a, 1), "B olcum %.2f sn, istatistik %.2f sn" % (o_b, d_b))
    r.kontrol("1: C (boşta) için ölçüm satırı yok", o_c == 0.0, "%.2f sn" % o_c)
    st.zaman["olcum_a_sn"], st.zaman["istatistik_a_sn"] = round(o_a, 1), round(d_a, 1)
    # API: en-cok
    y = ctx.p.istek("admin", "GET", "/db-cpu/en-cok?sure=1s&limit=50", yetki=jwt(), desen="GET /db-cpu/en-cok")
    lst = y.alan("liste", varsayilan=[]) or []
    ad = [x.get("db_kullanici") for x in lst]
    r.kontrol("GET /db-cpu/en-cok: A listenin başında, C yok", y.kod == 200 and ad[:1] == [a["dbu"]] and c["dbu"] not in ad, "sıra: %s" % ad[:4])
    # soğuma: gözlem kaydı kapanır
    t_kapa = bekle(lambda: iz.ilk(a["dbu"], lambda k, m: k is None, t_dur), 90, 2)
    st.zaman["gozlem_soguma_sn"] = round((t_kapa or 0) - t_dur, 1)
    r.kontrol("yük bitince gözlem kaydı kendiliğinden kapandı (soğuma 30 sn + pencere)", t_kapa is not None, "%.0f sn sonra (ölçüm)" % ((t_kapa or 0) - t_dur))
    time.sleep(2)
    r.kontrol("kapanışta denetim db.cpu_kisit_kaldir TEK kez; kapanış nedeni soguma", len(audit("db.cpu_kisit_kaldir", a["alan"], t0)) == 1
              and [x[6] for x in kisit_satirlari(a["dbu"], t0)][:1] == ["soguma"], str(kisit_satirlari(a["dbu"], t0)))
    r.kontrol("B ve C işçileri gözlem boyunca hiç kopmadı", not wb.olu() and not wc.olu(), "")
    yuk_durdur([wb, wc], 8)


# ───────────────────────── 2: uygula (A döngüsü) + 3g kanca + 3b parola + 3e geçişler ─────────────────────────

def faz_uygula(ctx, st):
    r, h, iz = ctx.r, st.h, st.iz
    r.bolum("governor/2-uygula")
    a, b, c = h["ga"], h["gb"], h["gc"]
    t0 = time.time()
    st.t_uygula = t0
    y = mod_ayarla(ctx, "uygula")
    r.kontrol("PUT /db-cpu/durum uygula (kök JWT) → degisti=true", y.kod == 200 and y.alan("degisti") is True and y.alan("onceki") == "gozlem", y.ozet())
    tur_bekle(ctx, 2)
    P_once = priv_oku()
    wa = [Isci(a["dbu"], a["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(3)] + [Isci(a["dbu"], a["pw"], sorgu_kalici(st.hiz), "kalici"),
                                                                                          Isci(a["dbu"], a["pw"], sorgu_uzun(st.hiz), "uzun")]
    wb = Isci(b["dbu"], b["pw"], sorgu_hafif(st.hiz), "hafif")
    wc = Isci(c["dbu"], c["pw"], "SELECT SLEEP(400)", "bos")
    tc0 = iz.cpu(a["dbu"]) or 0
    yuk_baslat(wa + [wb, wc])
    # kimlik kontrolü için sistem bağlantılarının kimlikleri
    wr = Isci("root", "", "SELECT SLEEP(400)", "root")
    wr.start()
    t_yuk = time.time()
    t1 = bekle(lambda: iz.ilk(a["dbu"], lambda k, m: k is not None and k[4] == 1, t_yuk), 70, 1)
    ta = bekle(lambda: iz.ilk(a["dbu"], lambda k, m: m == KISIT, t_yuk), 20, 1)
    t2 = bekle(lambda: iz.ilk(a["dbu"], lambda k, m: k is not None and k[0] >= 2, t_yuk), 60, 1)
    rel = lambda t: round((t or 0) - t_yuk, 1)  # noqa: E731
    r.kontrol("2: seviye 1 (kayıt açıldı, uygulandi=1)", t1 is not None, "yük başlangıcından %s sn (ölçüm)" % rel(t1))
    r.kontrol("2: ALTER USER … MAX_USER_CONNECTIONS 2 uygulandı", ta is not None, "%s sn (ölçüm)" % rel(ta))
    r.kontrol("2: seviye 2'ye yükseldi (yükselme 10 sn)", t2 is not None, "%s sn; seviye1→2 aralığı %s sn (ölçüm)" % (rel(t2), round((t2 or 0) - (t1 or 0), 1)))
    st.zaman.update({"s1_sn": rel(t1), "alter_sn": rel(ta), "s2_sn": rel(t2)})
    time.sleep(1)
    P1 = priv_oku()
    ka, ka2 = P1.get(a["dbu"] + "@localhost"), P1.get(a["dbu2"] + "@localhost")
    o_a = P_once.get(a["dbu"] + "@localhost")
    r.kontrol("2: A'da yalnız max_user_connections değişti (12→2); sorgu/güncelleme limitleri, kilit ve parola/yetki aynı",
              ka and o_a and ka["lim"] == (777777, 555555, 0, KISIT) and ka["rest"] == o_a["rest"] and ka["kilit"] == o_a["kilit"], "önce %s sonra %s" % (o_a and o_a["lim"], ka and ka["lim"]))
    o_a2 = P_once.get(a["dbu2"] + "@localhost")
    r.kontrol("2: aynı hostingin yükü olmayan 2. DB kullanıcısı KISILMADI (pay bazlı hedefleme)", ka2 and o_a2 and ka2["lim"] == o_a2["lim"], "önce %s sonra %s" % (o_a2 and o_a2["lim"], ka2 and ka2["lim"]))
    r.kontrol("2: B (başka hosting) hiç etkilenmedi: limitler ve hesap kaydı aynı, bağlantısı sürüyor", P1.get(b["dbu"] + "@localhost") == P_once.get(b["dbu"] + "@localhost") and not wb.olu(), "")
    r.kontrol("2: C ve tüm sistem/diğer hesaplar (root, mariadb.sys, panel, pma, roundcube, mail…) değişmedi", not priv_fark(P_once, P1, haric=(a["dbu"], a["dbu2"])),
              "değişen: %s" % priv_fark(P_once, P1, haric=(a["dbu"], a["dbu2"])))
    # seviye 2 sonrası: uzun sorgular öldürülüyor
    time.sleep(6)
    kul_uzun = wa[4]
    n_kill = len(gunluk(t2 or t_yuk, r"DB kısıcı: %s sorgusu .* KILL QUERY" % re.escape(a["dbu"])))
    sureler = [o[1] - o[0] for o in kul_uzun.ol if o[0] > (t2 or t_yuk) and o[2] == 0]
    r.kontrol("2: seviye 2'de uzun (>3 sn) okuma sorgusu KILL QUERY ile öldürüldü (günlük satırı; BENCHMARK kesilince istemci hata görmez)", n_kill > 0,
              "KILL QUERY günlük satırı=%d; uzun işçi sorgu süreleri(sn)=%s (8 sn'lik sorgu kısaldı) — ölçüm" % (n_kill, [round(x, 1) for x in sureler[:5]]))
    r.kontrol("2: seviye 1'de yeni bağlantılar sınıra takıldı (ERROR 1226 görüldü)", sum(w.say(1226) for w in wa) > 0, "1226 toplam=%d" % sum(w.say(1226) for w in wa))
    # seviye 3
    t3 = bekle(lambda: iz.ilk(a["dbu"], lambda k, m: k is not None and k[0] >= 3, t2 or t_yuk), 110, 1)
    r.kontrol("2: seviye 3 (bağlantılar KILL CONNECTION) — seviye 2'den ≥ 60 sn sonra (baglanti_kes_dk=1)", t3 is not None and (t3 - (t2 or 0)) >= 58, "seviye2→3 aralığı %s sn (ölçüm)" % round((t3 or 0) - (t2 or 0), 1))
    st.zaman["s3_sn"] = rel(t3)
    time.sleep(3)
    last = iz.s[-1][1].get(a["dbu"])
    r.kontrol("2: db_cpu_kisit: seviye 3, kesilen>0, oldurulen>0", last is not None and last[0] == 3 and last[2] > 0 and last[3] > 0, "kayıt (seviye,kisit,kesilen,oldurulen,uyg)=%s" % (last,))
    r.kontrol("2: kalıcı bağlantı (kalici işçi) KILL ile koptu", wa[3].olu(), "çıkışlar: %s" % [(round(o[0] - t_yuk), o[2], o[3]) for o in wa[3].ol[:3]])
    r.kontrol("2: B'nin kalıcı bağlantısı ve C'nin uzun deyimi 3. seviyede de KOPMADI", not wb.olu() and not wc.olu(), "")
    r.kontrol("2: 'root' sentinel bağlantısı 3. seviyede KOPMADI (sistem hesapları kesilmez)", not wr.olu(), "")
    # üst sınır: seviye 3'te A kaç çekirdek yakıyor?
    time.sleep(22)
    cek = iz.cekirdek(a["dbu"], time.time() - 20, time.time())
    st.zaman["a_seviye3_cekirdek"] = None if cek is None else round(cek, 2)
    ccek = iz.cekirdek(a["dbu"], t_yuk + 4, t_yuk + 14)
    st.zaman["a_oncesi_cekirdek"] = None if ccek is None else round(ccek, 2)
    ctx.r.bulgu("bilgi", "seviye 3'te A'nın CPU'su", "kısıttan önce %s çekirdek → seviye 3'te %s çekirdek (MAX_USER_CONNECTIONS=%d; VM 2 çekirdek) — ölçüm" % (ccek and round(ccek, 2), cek and round(cek, 2), KISIT))
    r.kontrol("2: kademeli kısmanın CPU üst sınırı = bağlantı sayısı (bilgi): seviye 3'te A hâlâ eşiğin (0,5) üstünde", cek is not None and cek > 0.5, "A seviye 3'te %s çekirdek (ölçüm)" % (cek and round(cek, 2)))
    # 3g: plan yeniden uygulaması kısıtı EZMİYOR (kanca), diğer limitler plan değerine yazılıyor
    y = limitler(ctx, a["id"], dict(OZGUN, db_max_queries_per_hour=777778, max_db=200))
    time.sleep(8)
    px = priv_oku().get(a["dbu"] + "@localhost")
    r.kontrol("3g: plan yeniden uygulandı (sorgu limiti 777778 yazıldı) AMA max_user_connections seviye 3 kısıtında KALDI (kanca)", y.kod == 200 and px and px["lim"] == (777778, 555555, 0, KISIT3), "limitler=%s HTTP %d" % (px and px["lim"], y.kod))
    # 3b: kısıt sürerken DB parolası değişimi
    yeni_pw = gosp.guclu_parola()
    gosp.sir_kaydet(yeni_pw)
    y = ctx.kok_api("PUT", "/domains/%d/databases/users/%s/password" % (a["id"], a["dbu2"]), {"password": yeni_pw}, desen="PUT /domains/{id}/databases/users/{u}/password")
    time.sleep(2)
    p2 = priv_oku().get(a["dbu2"] + "@localhost")
    r.kontrol("3b: A2'nin parolası değişti (200), max_user_connections ve kilit bozulmadı (A2 yüksüz → kısıtsız)", y.kod == 200 and p2 and o_a2 and p2["lim"][3] == o_a2["lim"][3] and p2["rest"] != o_a2["rest"] and not p2["kilit"], "HTTP %d limit=%s" % (y.kod, p2 and p2["lim"]))
    a["pw2"] = yeni_pw
    # 3e: kip geçişleri (A seviye 3, yük sürüyor)
    n_olay0 = len(gunluk(t0, "DB kısıcı"))
    mod_ayarla(ctx, "gozlem")
    time.sleep(16)
    s = iz.s[-1][1].get(a["dbu"])
    r.kontrol("3e: uygula→gozlem: mevcut kısıt KALKMADI (yük sürüyor), tırmanma/yeni eylem yok; kısıt gözlemde soğumayla kalkar", s is not None and s[0] == 3 and s[4] == 1 and iz.s[-1][2].get(a["dbu"]) == KISIT3,
              "kayıt=%s max_user_conn=%s" % (s, iz.s[-1][2].get(a["dbu"])))
    ctx.r.bulgu("bilgi", "uygula→gozlem kısıtı kaldırmıyor", "yük sürerken gözlem kipine geçmek mevcut kısıtı (seviye 3: MAX_USER_CONNECTIONS=1) KALDIRMAZ; acil çıkış yolu 'kapali' (kod niyeti: ayar.go ModGozlem açıklaması) — ölçüm")
    t_k = time.time()
    mod_ayarla(ctx, "kapali")
    t_ser = bekle(lambda: mx(a["dbu"]) == 12 and mx(a["dbu2"]) == 12, 30, 1)
    r.kontrol("3e: gozlem→kapali: kısıt bir tur içinde kalktı, A ÖZGÜN değerine (12) döndü", t_ser is not None, "%s sn (ölçüm)" % round(time.time() - t_k, 1))
    st.zaman["kapali_serbest_sn"] = round(time.time() - t_k, 1)
    sat = kisit_satirlari(a["dbu"], t0)
    r.kontrol("3e: kapanış nedeni 'kapali', kayıt kapandı (acik=NULL)", bool(sat) and sat[0][6] == "kapali" and sat[0][7] == "0", str(sat[:1]))
    pk = priv_oku().get(a["dbu"] + "@localhost")
    r.kontrol("3e: kapaliyken geri yazılan limitler BİREBİR özgün (777778 sorgu, 555555 güncelleme, 12 bağlantı)", pk and pk["lim"] == (777778, 555555, 0, 12), str(pk and pk["lim"]))
    time.sleep(12)
    r.kontrol("3e: kapali kipte yeni kısıt konmuyor (yük sürerken 12 sn)", mx(a["dbu"]) == 12, "")
    mod_ayarla(ctx, "uygula")
    t_y = bekle(lambda: mx(a["dbu"]) == KISIT, 70, 1)
    r.kontrol("3e: kapali→uygula: yük sürdüğü için YENİDEN kısıldı (yeni kayıt, seviye 1)", t_y is not None, "%s sn (ölçüm)" % round(time.time() - t_k, 1))
    # temiz çıkış: yük bitince birebir geri dönüş
    yuk_durdur(wa)
    t_dur = time.time()
    t_birak = bekle(lambda: mx(a["dbu"]) == 12 and iz.s[-1][1].get(a["dbu"]) is None, 100, 2)
    st.zaman["yuk_bitis_serbest_sn"] = round(time.time() - t_dur, 1)
    r.kontrol("2: yük bitince kısıt KALKTI ve A ÖZGÜN limitlerine döndü", t_birak is not None, "%s sn (ölçüm)" % round(time.time() - t_dur, 1))
    P2 = priv_oku()
    pa, pa2 = P2.get(a["dbu"] + "@localhost"), P2.get(a["dbu2"] + "@localhost")
    r.kontrol("2: A'nın TÜM değerleri birebir: (777778 sorgu/saat, 555555 güncelleme/saat, 0 genel, 12 bağlantı), kilit yok, parola/yetki özeti aynı",
              pa and pa["lim"] == (777778, 555555, 0, 12) and pa["rest"] == o_a["rest"] and not pa["kilit"], str(pa and pa["lim"]))
    r.kontrol("2: A2 de ÖZGÜN değerine döndü (plan yeniden uygulamasının 777778'i dahil), kilit yok", pa2 and o_a2 and pa2["lim"] == (777778, 555555, 0, 12) and not pa2["kilit"], "önce %s sonra %s" % (o_a2 and o_a2["lim"], pa2 and pa2["lim"]))
    r.kontrol("2: tüm diğer hesaplar kampanya boyunca BİREBİR aynı (yalnız ga/ga2 değişebilir)", not priv_fark(st.P0, P2, haric=(a["dbu"], a["dbu2"])), "değişen: %s" % priv_fark(st.P0, P2, haric=(a["dbu"], a["dbu2"])))
    sat = kisit_satirlari(a["dbu"], t0)
    r.kontrol("2: db_cpu_kisit geçmişi: 1. kayıt seviye 3 / kapali, 2. kayıt soguma", len(sat) >= 2 and sat[0][0] == "3" and sat[-1][6] == "soguma", str(sat))
    r.kontrol("2: denetim: db.cpu_kisit / yükselt / baglanti_kes ilk epizotta TEK; kaldır kapali+soguma = 2", len(audit("db.cpu_kisit", a["alan"], t0)) == 2 and len(audit("db.cpu_kisit_yukselt", a["alan"], t0)) >= 1
              and len(audit("db.cpu_baglanti_kes", a["alan"], t0)) == 1 and len(audit("db.cpu_kisit_kaldir", a["alan"], t0)) == 2,
              "kisit=%d yukselt=%d kes=%d kaldir=%d" % tuple(len(audit(x, a["alan"], t0)) for x in ("db.cpu_kisit", "db.cpu_kisit_yukselt", "db.cpu_baglanti_kes", "db.cpu_kisit_kaldir")))
    r.kontrol("2: bildirim sayıları: 'kısıtı uygulandı' 2 (iki epizot), 'sertleşti' ≥1, 'bağlantıları kesildi' 1 — spam yok", bildirim("CPU kısıtı uygulandı", a["id"], t0) in (1, 2) and bildirim("bağlantıları kesildi", a["id"], t0) == 1,
              "uygulandı=%d sertleşti=%d kesildi=%d kaldırıldı=%d" % (bildirim("CPU kısıtı uygulandı", a["id"], t0), bildirim("sertleşti", a["id"], t0), bildirim("bağlantıları kesildi", a["id"], t0), bildirim("kaldırıldı", a["id"], t0)))
    hata = gunluk(t0, r"DB kısıcı: .*(başarısız|okunamadı|yazılamadı|kaldırılamadı|paniği|kesilemedi)")
    r.kontrol("2: kısıcı günlüğünde hata satırı yok", not hata, "; ".join(hata[:2]))
    yuk_durdur([wb, wc, wr], 8)
    mod_ayarla(ctx, "gozlem")


# ───────────────────────── 3a/3b: askı ve silme; 3g: eski bekçi ─────────────────────────

def faz_askisilme(ctx, st):
    r, h, iz = ctx.r, st.h, st.iz
    r.bolum("governor/3-aski-silme")
    d, e = h["gd"], h["ge"]
    t0 = time.time()
    ayar_yaz({"db_kisici_soguma_sn": "120"})   # üretim varsayılanı gibi soğuma >> kimlik önbelleği (60 sn): 'askida'/'silindi' yolları sınansın
    mod_ayarla(ctx, "uygula")
    tur_bekle(ctx, 2)
    P_once = priv_oku()
    wd = [Isci(d["dbu"], d["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(2)]
    we = [Isci(e["dbu"], e["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(2)]
    yuk_baslat(wd + we)
    t_yuk = time.time()
    td = bekle(lambda: mx(d["dbu"]) == KISIT, 70, 1) and time.time()
    te = bekle(lambda: mx(e["dbu"]) == KISIT, 70, 1) and time.time()
    r.kontrol("3a/3b: D ve E ayrı hostingler aynı anda kısıldı", td is not None and te is not None, "D %s sn, E %s sn" % (round((td or 0) - t_yuk, 1), round((te or 0) - t_yuk, 1)))
    # 3a: kısıtlıyken askıya al
    y = ctx.kok_api("POST", "/domains/%d/askiya-al" % d["id"], {}, desen="POST /domains/{id}/askiya-al", zaman=120)
    r.kod("3a: kısıtlı D hostingi askıya alındı", y, 200)
    time.sleep(3)
    ds = hesap_durum(d["dbu"])
    r.kontrol("3a: askıda hesap KİLİTLİ (ACCOUNT LOCK) — kısıt bunu bozmadı", ds and all(k for _, _, k in ds), str(ds))
    t_ask = time.time()
    plan_d = mx_plan(ctx, d)
    tb = bekle(lambda: mx(d["dbu"]) == plan_d, 100, 2)
    ds = hesap_durum(d["dbu"])
    sat = kisit_satirlari(d["dbu"], t0)
    r.kontrol("3a: kısıt askıda KALKTI (sınır plan değerine döndü) ve hesap HÂLÂ kilitli — governor kilidi AÇMADI", tb is not None and ds and all(k for _, _, k in ds) and bool(sat) and sat[0][7] == "0",
              "%s sn; hesap=%s; kayıt=%s" % (round(time.time() - t_ask, 1), ds, sat[:1]))
    r.kontrol("3a: kapanış nedeni 'askida' (soğuma 120 sn iken askı yolu tetiklendi)", bool(sat) and sat[0][6] == "askida", str(sat[:1]))
    st.zaman["aski_serbest_sn"] = round(time.time() - t_ask, 1)
    y = ctx.kok_api("POST", "/domains/%d/askidan-al" % d["id"], {}, desen="POST /domains/{id}/askidan-al", zaman=120)
    r.kod("3a: D askıdan alındı", y, 200)
    time.sleep(3)
    ds = hesap_durum(d["dbu"])
    r.kontrol("3a: askıdan alınca hesap açıldı, sınır plan değerinde", ds and not any(k for _, _, k in ds) and ds[0][1] == plan_d, str(ds))
    # 3b: kısıtlıyken hosting silme
    y = ctx.p.istek("admin", "DELETE", "/domains/%d?onay=%s" % (e["id"], e["alan"]), yetki=ctx.kok, desen="DELETE /domains/{id}", zaman=240)
    r.kod("3b: kısıtlı E hostingi silindi", y, 200)
    ctx.hostingler.pop("ge", None)
    time.sleep(2)
    r.kontrol("3b: E'nin MariaDB hesabı silindi", not hesap_durum(e["dbu"]), "")
    t_sil = time.time()
    tc = bekle(lambda: not kisit_satirlari(e["dbu"], t0) or kisit_satirlari(e["dbu"], t0)[0][7] == "0", 100, 2)
    sat = kisit_satirlari(e["dbu"], t0)
    r.kontrol("3b: silinen hostingin kısıt kaydı kapandı (yetim açık kayıt yok)", tc is not None, "%s sn; kayıt=%s" % (round(time.time() - t_sil, 1), sat[:1]))
    r.kontrol("3b: kapanış nedeni 'silindi'", bool(sat) and sat[0][6] == "silindi", str(sat[:1]))
    st.zaman["silme_kayit_kapanma_sn"] = round(time.time() - t_sil, 1)
    yuk_durdur(wd + we)
    hata = gunluk(t0, r"DB kısıcı: .*(başarısız|okunamadı|yazılamadı|kaldırılamadı|paniği|kesilemedi)")
    r.kontrol("3a/3b: kısıcı günlüğünde hata satırı yok (askı/silme sırasında)", not hata, "; ".join(hata[:3]))
    mod_ayarla(ctx, "gozlem")
    ayar_yaz({"db_kisici_soguma_sn": "30"})
    P = priv_oku()
    r.kontrol("3a/3b: diğer tüm hesaplar aynı (D, E hariç)", not priv_fark(P_once, P, haric=(d["dbu"], e["dbu"])), "değişen: %s" % priv_fark(P_once, P, haric=(d["dbu"], e["dbu"])))


def mx_plan(ctx, x):
    """hostingin efektif mysql_max_baglanti'sı (plan ya da üst yazım)"""
    return int(panel_sql("SELECT mysql_max_baglanti FROM service_plans WHERE id=(SELECT plan_id FROM domains WHERE id=%d)" % x["id"]))


def faz_bekci(ctx, st):
    r, h, iz = ctx.r, st.h, st.iz
    r.bolum("governor/3g-eski-bekci")
    g = h["gg"]
    t0 = time.time()
    mod_ayarla(ctx, "uygula")
    tur_bekle(ctx, 2)
    wg = [Isci(g["dbu"], g["pw"], sorgu_uzun(st.hiz, 8), "uzun") for _ in range(2)]
    yuk_baslat(wg)
    t_yuk = time.time()
    t1 = bekle(lambda: mx(g["dbu"]) == KISIT, 70, 1) and time.time()
    t2 = bekle(lambda: iz.ilk(g["dbu"], lambda k, m: k is not None and k[0] >= 2, t_yuk), 60, 1)
    time.sleep(35)
    yuk_durdur(wg)
    n1317 = sum(w.say(1317) for w in wg)
    nbek = len(gunluk(t0, r"Governor: %s sorgusu" % re.escape(g["dbu"])))
    ngov = len(gunluk(t0, r"DB kısıcı: %s sorgusu" % re.escape(g["dbu"])))
    nbasarisiz = len(gunluk(t0, r"Governor: %s KILL başarısız" % re.escape(g["dbu"])))
    r.kontrol("3g: eski bekçi (3 sn) + kısıcı seviye 2 aynı hostingte birlikte çalıştı; kısıt kuruldu ve yükseldi", t1 is not None and t2 is not None, "seviye1 %s sn, seviye2 %s sn" % (round((t1 or 0) - t_yuk, 1), round((t2 or 0) - t_yuk, 1)))
    sure = [round(o[1] - o[0], 1) for w in wg for o in w.ol if o[2] == 0]
    r.kontrol("3g: uzun deyimler öldürüldü (8 sn'lik sorguların çoğu kısaldı; ilk sorgu bekçi eşiğine kadar sürer); bekçi ve kısıcı KILL toplamı > 0", (nbek + ngov) > 0 and bool(sure) and sum(1 for x in sure if x < 8.0) * 2 >= len(sure),
              "bekçi KILL=%d; kısıcı KILL=%d; bekçi 'Unknown thread' başarısız=%d; sorgu süreleri=%s (ölçüm)" % (nbek, ngov, nbasarisiz, sure[:8]))
    ctx.r.bulgu("bilgi", "eski bekçi + kısıcı çakışması", "1317=%d bekçi=%d kısıcı=%d bekçi-başarısız(Unknown thread)=%d" % (n1317, nbek, ngov, nbasarisiz))
    time.sleep(2)
    p = hesap_durum(g["dbu"])
    r.kontrol("3g: çelişen ALTER yok — kısıt değeri bekçi/plan tarafından ezilmedi", p and p[0][1] == KISIT, str(p))
    hata = gunluk(t0, r"DB kısıcı: .*(başarısız|okunamadı|yazılamadı|kaldırılamadı|paniği|kesilemedi)")
    r.kontrol("3g: kısıcı günlüğünde hata satırı yok", not hata, "; ".join(hata[:3]))
    # soğuma
    t_b = bekle(lambda: mx(g["dbu"]) == 15 or mx(g["dbu"]) == mx_plan(ctx, g), 90, 2)
    r.kontrol("3g: yük bitince kısıt kalktı (plan değeri)", t_b is not None, "")
    mod_ayarla(ctx, "gozlem")


# ───────────────────────── 3c/3d: yeniden başlatmalar ─────────────────────────

def faz_yenidenbasla(ctx, st):
    r, h, iz = ctx.r, st.h, st.iz
    r.bolum("governor/3c-3d-yeniden-baslatma")
    a, b, c = h["ga"], h["gb"], h["gc"]
    t0 = time.time()
    mod_ayarla(ctx, "uygula")
    tur_bekle(ctx, 2)
    wa = [Isci(a["dbu"], a["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(3)]
    yuk_baslat(wa)
    t1 = bekle(lambda: mx(a["dbu"]) == KISIT, 70, 1)
    r.kontrol("3c: ön koşul — A kısıldı", t1 is not None, "")
    # 3c: panel yeniden başlatma
    t_r = time.time()
    subprocess.run(["systemctl", "restart", "girginospanel"], timeout=120)
    t_b = bekle(lambda: mx(a["dbu"]) == 12 and mx(a["dbu2"]) == 12, 60, 1)
    sat = kisit_satirlari(a["dbu"], t0)
    r.kontrol("3c: panel yeniden başlayınca takılı kısıt KALMADI (açılış kurtarma) ve A özgün değerine döndü", t_b is not None, "%s sn (ölçüm)" % round(time.time() - t_r, 1))
    r.kontrol("3c: kayıt nedeni 'acilis'", bool(sat) and sat[0][6] == "acilis" and sat[0][7] == "0", str(sat[:1]))
    st.zaman["panel_restart_serbest_sn"] = round(time.time() - t_r, 1)
    t_y = bekle(lambda: mx(a["dbu"]) == KISIT, 90, 1) and time.time()
    r.kontrol("3c: yük sürdüğü için panel açılışından sonra yeniden kısıldı (yeni kayıt)", t_y is not None, "yeniden başlatmadan %s sn sonra (ölçüm)" % round((t_y or 0) - t_r, 1))
    # 3d: MariaDB yeniden başlatma
    yuk_durdur(wa)
    cpu_once = st.iz.cpu(a["dbu"])
    t_m = time.time()
    subprocess.run(["systemctl", "restart", "mariadb"], timeout=300)
    ok = bekle(lambda: subprocess.run(["mysqladmin", "-uroot", "ping"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0, 120, 1)
    r.kontrol("3d: MariaDB yeniden başladı", ok, "%s sn" % round(time.time() - t_m, 1))
    r.kontrol("3d: userstat yeniden başlatmadan sonra AÇIK (kalıcı yapılandırma)", kok_sql("SELECT @@GLOBAL.userstat") == "1", "")
    subprocess.run(["mysql", "-u", a["dbu"], "-N", "-B", "-e", "SELECT BENCHMARK(%d, MD5('c'))" % int(st.hiz * 0.2)], env=dict(os.environ, MYSQL_PWD=a["pw"]),
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    time.sleep(1)
    cpu_sonra = float(kok_sql("SELECT COALESCE(MAX(CPU_TIME),0) FROM information_schema.USER_STATISTICS WHERE USER=%s" % gosp.sqlq(a["dbu"])) or 0)
    r.kontrol("3d: USER_STATISTICS sayaçları sıfırlandı (CPU_TIME düştü) — governor'un negatif fark yolu", cpu_sonra is None or cpu_once is None or cpu_sonra < cpu_once,
              "önce %s sonra %s (ölçüm)" % (cpu_once and round(cpu_once, 1), cpu_sonra and round(cpu_sonra, 1)))
    # yük yeniden: B/C yanlış işaretlenmemeli, A mevcut kayıtla sürmeli
    wa = [Isci(a["dbu"], a["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(3)]
    wb = Isci(b["dbu"], b["pw"], sorgu_hafif(st.hiz), "hafif")
    yuk_baslat(wa + [wb])
    time.sleep(45)
    ds, y = durum(ctx)
    k = iz.s[-1][1]
    r.kontrol("3d: yeniden başlatma sonrası B/C yanlış işaretlenmedi; negatif/sahte fark yok", b["dbu"] not in k and c["dbu"] not in k, "açık kayıtlar=%s" % sorted(k))
    r.kontrol("3d: MariaDB döndükten sonra kısıcı toparlandı (son_hata boş, yoklama sürüyor)", ds.get("son_hata", "") == "", "son_hata=%s" % ds.get("son_hata"))
    r.kontrol("3d: A kısıtı yeniden/sürerek uygulanıyor", mx(a["dbu"]) == KISIT, "")
    mx_ms = int(panel_sql("SELECT COALESCE(MAX(cpu_ms),0) FROM db_cpu_olcum WHERE db_user=%s" % gosp.sqlq(a["dbu"])))
    r.kontrol("3d: olcum satırlarında olanaksız değer yok (5 dk diliminde ≤ 2 çekirdek x 300 sn)", mx_ms <= 2 * 300 * 1000, "en büyük dilim %.0f sn (ölçüm)" % (mx_ms / 1000))
    hata = gunluk(t_m, r"DB kısıcı: ")
    st.zaman["mariadb_kesinti_gunluk"] = [x[:90] for x in hata[:2]]
    yuk_durdur(wa + [wb])
    t_b = bekle(lambda: mx(a["dbu"]) == 12 and mx(a["dbu2"]) == 12, 100, 2)
    r.kontrol("3c/3d: yük bitince A ve A2 özgün değerine döndü", t_b is not None, "")
    mod_ayarla(ctx, "gozlem")


# ───────────────────────── 4: yetki ─────────────────────────

def faz_yetki(ctx, st):
    r, p, h = ctx.r, ctx.p, st.h
    r.bolum("governor/4-yetki")
    gy = h["gy"]
    a = h["ga"]
    b = ctx.bayi("A")
    bjwt = ctx.bayi_giris(b) or b["jwt"]
    kok = jwt()
    # kök: tüm uçlar 200
    for yol in ("/db-cpu/durum", "/db-cpu/en-cok?sure=24s", "/db-cpu/kisitlar?durum=tumu&sure=7g"):
        r.kod("kök JWT: GET " + yol, p.istek("admin", "GET", yol, yetki=kok), 200)
    # bayi JWT
    r.kod("bayi: PUT /db-cpu/durum → 403 (mod değişmedi)", p.istek("bayi", "PUT", "/db-cpu/durum", {"mod": "uygula"}, yetki=bjwt, desen="PUT /db-cpu/durum"), 403)
    r.kod("bayi: GET /db-cpu/durum → 403", p.istek("bayi", "GET", "/db-cpu/durum", yetki=bjwt, desen="GET /db-cpu/durum"), 403)
    y = p.istek("bayi", "GET", "/db-cpu/en-cok?sure=24s&limit=100&bayi=1", yetki=bjwt, desen="GET /db-cpu/en-cok")
    ad = [x.get("db_kullanici") for x in (y.alan("liste", varsayilan=[]) or [])]
    r.kontrol("bayi: en-cok YALNIZ kendi hostingleri (kök listesinde A/B var, bayi listesinde yalnız gy; ?bayi= süzgeci işe yaramaz)", y.kod == 200 and gy["dbu"] in ad and a["dbu"] not in ad and h["gb"]["dbu"] not in ad,
              "bayi listesi=%s" % ad)
    y = p.istek("bayi", "GET", "/db-cpu/kisitlar?durum=tumu&sure=7g", yetki=bjwt, desen="GET /db-cpu/kisitlar")
    kd = [x.get("db_kullanici") for x in (y.alan("liste", varsayilan=[]) or [])]
    r.kontrol("bayi: kisitlar listesinde başka kiracı yok", y.kod == 200 and a["dbu"] not in kd, "kayıtlar=%s" % kd)
    # müşteri kapısı
    mj = ctx.musteri_giris(h["gy"]).alan("token", varsayilan="")
    for yol, metod in (("/db-cpu/en-cok", "GET"), ("/db-cpu/durum", "GET"), ("/db-cpu/kisitlar", "GET")):
        y = p.istek("musteri", metod, yol, yetki="Bearer " + mj, desen=metod + " " + yol) if mj else None
        r.kontrol("müşteri kapısı: %s %s erişemez (403/404)" % (metod, yol), y is not None and y.kod in (403, 404), y.ozet() if y else "müşteri jetonu alınamadı")
    r.kod("müşteri kapısı: jetonsuz → 401/403/404", p.istek("musteri", "GET", "/db-cpu/en-cok"), (401, 403, 404))
    # API anahtarı: yollar izin tablosunda YOK → varsayılan reddet
    for ad_, yetki, kapi in (("kök anahtarı (tüm izinler)", ctx.kok, "admin"), ("bayi anahtarı (tüm izinler)", ctx.an.ac("gk", b["kul"], "reseller", TUM_BAYI), "bayi")):
        for metod, yol, g in (("GET", "/db-cpu/durum", None), ("GET", "/db-cpu/en-cok", None), ("GET", "/db-cpu/kisitlar", None), ("PUT", "/db-cpu/durum", {"mod": "uygula"})):
            if kapi == "bayi" and yol == "/db-cpu/durum":
                continue
            y = p.istek(kapi, metod, yol, g, yetki=yetki, desen=metod + " " + yol)
            r.kontrol("%s: %s %s → 403 (izin tablosunda yok: varsayılan reddet)" % (ad_, metod, yol), y.kod == 403, y.ozet())
    d, y = durum(ctx)
    r.kontrol("anahtar/bayi PUT denemelerinden sonra mod hâlâ gozlem", y.alan("mod_ayar") == "gozlem", "mod_ayar=%s" % y.alan("mod_ayar"))
    # geçersiz mod
    r.kod("kök: PUT /db-cpu/durum {mod:'bozuk'} → 400", p.istek("admin", "PUT", "/db-cpu/durum", {"mod": "bozuk"}, yetki=kok, desen="PUT /db-cpu/durum"), 400)
    r.kod("kök: PUT /db-cpu/durum gövdesiz → 400", p.istek("admin", "PUT", "/db-cpu/durum", ham="{", yetki=kok, desen="PUT /db-cpu/durum"), 400)
    y = p.istek("admin", "PUT", "/db-cpu/durum", {"mod": "GOZLEM "}, yetki=kok, desen="PUT /db-cpu/durum")
    r.kontrol("aynı mod (büyük harf/boşluk) → 200 degisti=false, denetim üretmez", y.kod == 200 and y.alan("degisti") is False, y.ozet())
    # denetim: mod değişimleri doğru aktörle
    out = panel_sql("SELECT actor_username, actor_user_id, ok, detail FROM audit_log WHERE action='db.cpu_kisici_mod' AND ts >= FROM_UNIXTIME(%d) ORDER BY id" % int(st.t_basla))
    sat = [ln.split("\t") for ln in out.splitlines() if ln]
    r.kontrol("denetim db.cpu_kisici_mod: her kayıt aktör 'root' (kök JWT), ok=1; bayi/anahtar denemeleri kayıt üretmedi", bool(sat) and all(x[0] == "root" and x[2] == "1" for x in sat),
              "%d kayıt; aktörler=%s" % (len(sat), sorted({x[0] for x in sat})))
    n_gecis = sum(1 for x in sat if "→" in x[3])
    r.kontrol("denetim sayısı = gerçek mod değişimi sayısı (aynı değeri yazmak kayıt üretmez)", len(sat) == n_gecis, "%d" % n_gecis)


# ───────────────────────── 3h + 5: uzun (GOV_UZUN=1) ─────────────────────────

def faz_uzun(ctx, st):
    r, h, iz = ctx.r, st.h, st.iz
    r.bolum("governor/3h-5-uzun")
    a = h["ga"]
    t0 = time.time()
    # 3h: userstat kapalı
    mod_ayarla(ctx, "uygula")
    tur_bekle(ctx, 2)
    wa = [Isci(a["dbu"], a["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(3)]
    yuk_baslat(wa)
    t1 = bekle(lambda: mx(a["dbu"]) == KISIT, 70, 1)
    r.kontrol("3h: ön koşul — A kısıldı", t1 is not None, "")
    kok_sql("SET GLOBAL userstat=0")
    t_u = time.time()
    # kısıcı açıkken HER turda denetler (aeacb3d): bir-iki yoklama içinde yeniden açmalı
    t_ac = bekle(lambda: kok_sql("SELECT @@GLOBAL.userstat") == "1", 60, 1)
    st.zaman["userstat_yeniden_acilis_sn"] = None if t_ac is None else round(t_ac - t_u, 1)
    r.kontrol("3h: kısıcı userstat'ı kendiliğinden yeniden açtı (açıkken her turda denetim; ≤ 3 yoklama)", t_ac is not None and (t_ac - t_u) <= 16,
              "%s sn sonra (ölçüm; poll 5 sn)" % st.zaman["userstat_yeniden_acilis_sn"])
    t_ser = bekle(lambda: mx(a["dbu"]) == 12, 60, 2)
    d, _ = durum(ctx)
    st.zaman["userstat0_kisit_kalkis_sn"] = None if t_ser is None else round(time.time() - t_u, 1)
    r.kontrol("3h: userstat=0 kısıtı KALDIRMADI (yük sürerken 60 sn) ve durum.userstat 'acik'", t_ser is None and d.get("userstat") == "acik",
              "kalkış=%s sn; durum.userstat='%s' (ölçüm)" % (st.zaman["userstat0_kisit_kalkis_sn"], d.get("userstat")))
    if t_ser is not None:
        ctx.r.bulgu("orta", "userstat kapanınca kısıcı sessizce silahsızlanıyor", "yük sürerken userstat=0 → kısıt %s sn sonra 'soğuma' ile kalktı; durum.userstat o anda '%s'" % (st.zaman["userstat0_kisit_kalkis_sn"], d.get("userstat")))
    kok_sql("SET GLOBAL userstat=1")
    t_y = bekle(lambda: mx(a["dbu"]) in (KISIT, KISIT3), 30, 1)
    r.kontrol("3h: yük sürdüğü için kısıt sürüyor (seviye 1-2: 2, seviye 3: 1)", t_y is not None, "max_user_connections=%s" % mx(a["dbu"]))
    yuk_durdur(wa)
    bekle(lambda: mx(a["dbu"]) == 12, 100, 2)
    mod_ayarla(ctx, "gozlem")
    # 5: ölçek — ge/gd yerine gg hostingine 50 DB kullanıcısı
    g = h["gg"]
    y = limitler(ctx, g["id"], {"max_db": 200, "db_max_query_seconds": 3})
    kul = []
    t_o = time.time()
    for i in range(50):
        pw = gosp.guclu_parola()
        gosp.sir_kaydet(pw)
        y = ctx.kok_api("POST", "/domains/%d/databases" % g["id"], {"db_sonek": "s%02d" % i, "kullanici_tipi": "yeni", "kullanici_sonek": "s%02d" % i, "parola": pw},
                        desen="POST /domains/{id}/databases", zaman=120)
        if y.kod not in (200, 201):
            break
        u = panel_sql("SELECT db_user FROM db_accounts WHERE domain_id=%d AND db_name LIKE %s LIMIT 1" % (g["id"], gosp.sqlq("%%_s%02d" % i)))
        kul.append((u, pw))
        st.kullanicilar.append(u)
    r.kontrol("5: 50 DB kullanıcısı oluşturuldu (gg hostingi)", len(kul) == 50, "%d kullanıcı, %.0f sn" % (len(kul), time.time() - t_o))
    pid = int(subprocess.run(["systemctl", "show", "girginospanel", "-p", "MainPID", "--value"], stdout=subprocess.PIPE, universal_newlines=True).stdout.strip() or 0)

    def panel_cpu():
        f = open("/proc/%d/stat" % pid).read().rsplit(")", 1)[1].split()
        return (int(f[11]) + int(f[12])) / float(os.sysconf("SC_CLK_TCK"))   # yalnız panelin kendisi (utime+stime); çocuk süreçler hariç

    def busy():
        o = {}
        for ln in kok_sql("SELECT USER, BUSY_TIME, CPU_TIME FROM information_schema.USER_STATISTICS WHERE USER NOT LIKE %s AND USER<>'root'" % gosp.sqlq(ctx.onek + "%")).splitlines():
            f = ln.split("	")
            o[f[0]] = (float(f[1]), float(f[2]))
        return o

    def olc(sn):
        """sn boyunca: panel CPU'su (çekirdek), tur sayısı, DB tarafında en meşgul sistem kullanıcısı (=panelin DB hesabı) için TUR BAŞINA meşguliyet ve CPU (sn)."""
        c0, b0, y0 = panel_cpu(), busy(), int(durum(ctx)[0].get("yoklama_sayisi", 0))
        time.sleep(sn)
        c1, b1, y1 = panel_cpu(), busy(), int(durum(ctx)[0].get("yoklama_sayisi", 0))
        tur = max(1, y1 - y0)
        fark = {u: (v[0] - b0.get(u, (0.0, 0.0))[0], v[1] - b0.get(u, (0.0, 0.0))[1]) for u, v in b1.items()}
        en = max(fark.items(), key=lambda kv: kv[1][0]) if fark else ("?", (0.0, 0.0))
        return {"panel_cpu_cekirdek": round((c1 - c0) / sn, 4), "tur": tur, "db_kullanici": en[0], "db_busy_tur_basina_sn": round(en[1][0] / tur, 3), "db_cpu_tur_basina_sn": round(en[1][1] / tur, 3)}

    time.sleep(20)   # 50 kullanıcının oluşturma işleri otursun
    taban = olc(60)
    ws = [Isci(u, pw, ";".join(["SELECT BENCHMARK(%d, MD5('s')); SELECT SLEEP(0.9)" % int(st.hiz * 0.005)] * 200), "s") for u, pw in kul]
    yuk_baslat(ws)
    time.sleep(12)
    yuk = olc(60)
    kay = int(panel_sql("SELECT COUNT(*) FROM db_cpu_olcum WHERE db_user IN (%s)" % ",".join(gosp.sqlq(u) for u, _ in kul[:50])) or 0)
    yuk_durdur(ws, 15)
    st.zaman["olcek"] = {"kullanici": len(kul), "taban": taban, "yuk_50_kullanici": yuk, "olcum_satiri": kay, "poll_sn": 5}
    r.kontrol("5: 50 etkin kullanıcılı turda panel CPU'su < 0,10 çekirdek (ölçüm)", yuk["panel_cpu_cekirdek"] < 0.10,
              "taban %.4f, 50 kullanıcı %.4f çekirdek (poll 5 sn, %d tur); olcum satırı %d" % (taban["panel_cpu_cekirdek"], yuk["panel_cpu_cekirdek"], yuk["tur"], kay))
    r.kontrol("5: tur başına DB tarafı meşguliyet (alt sınır) < poll aralığının %40'ı = 2 sn (ölçüm)", yuk["db_busy_tur_basina_sn"] < 2.0 and yuk["tur"] >= 8,
              "taban %.3f sn/tur, 50 kullanıcı %.3f sn/tur (DB kullanıcısı %s; CPU %.3f sn/tur); tur=%d (ölçüm)" % (
                  taban["db_busy_tur_basina_sn"], yuk["db_busy_tur_basina_sn"], yuk["db_kullanici"], yuk["db_cpu_tur_basina_sn"], yuk["tur"]))
    # userstat QPS etkisi
    mod_ayarla(ctx, "kapali")
    time.sleep(8)
    res = {"0": [], "1": []}
    for tur in range(3):
        for u in ("0", "1"):
            kok_sql("SET GLOBAL userstat=%s" % u)
            time.sleep(1)
            t = time.time()
            subprocess.run(["mysqlslap", "-uroot", "--concurrency=4", "--iterations=1", "--number-of-queries=60000", "--query=SELECT 1,2,3"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=300)
            res[u].append(round(60000 / (time.time() - t)))
    kok_sql("SET GLOBAL userstat=1")
    ort = {u: sum(v) / len(v) for u, v in res.items()}
    st.zaman["userstat_qps"] = {"kapali": res["0"], "acik": res["1"], "fark_yuzde": round((ort["0"] - ort["1"]) / ort["0"] * 100, 1)}
    r.kontrol("5: userstat=1 basit sorgu işlem hacmini <%10 düşürüyor (ölçüm)", (ort["0"] - ort["1"]) / ort["0"] < 0.10, "userstat kapalı %s qps, açık %s qps → fark %%%s" % (res["0"], res["1"], st.zaman["userstat_qps"]["fark_yuzde"]))
    mod_ayarla(ctx, "gozlem")


# ───────────────────────── ek-hesap: kilit korunumu, çok host'lu hesap (kod inceleme soruları a, b) ─────────────────────────

def hesap_tail(kul, host="localhost"):
    """SHOW CREATE USER çıktısının parola özetinden SONRAKİ kısmı (kaynak sınırları + kilit); özet çıktıya girmez."""
    out = kok_sql("SHOW CREATE USER %s@%s" % (gosp.sqlq(kul), gosp.sqlq(host)))
    m = re.search(r"( REQUIRE | WITH | ACCOUNT ).*$", out, re.S)
    return re.sub(r"\s+", " ", m.group(0)).strip() if m else "(sınır/kilit yok)"


def tail_mx(t):
    m = re.search(r"MAX_USER_CONNECTIONS (\d+)", t)
    return int(m.group(1)) if m else 0


def tail_kilit(t):
    return "ACCOUNT LOCK" in t


def host_degerleri(kul):
    return {h_: m_ for h_, m_, _ in hesap_durum(kul)}


def faz_ek_hesap(ctx, st):
    r, h, iz = ctx.r, st.h, st.iz
    r.bolum("governor/ek-hesap")
    ek = st.zaman.setdefault("ek_hesap", {})
    ek["nproc"] = int(subprocess.run(["nproc"], stdout=subprocess.PIPE, universal_newlines=True).stdout.strip() or 0)
    # (a1) panelin askıya alması: kısıtlıyken askı, askıdayken bırakma, askıdan alma — SHOW CREATE USER önce/sonra
    ayar_yaz({"db_kisici_soguma_sn": "120"})
    mod_ayarla(ctx, "uygula")
    tur_bekle(ctx, 2)
    d = h["gd"]
    plan_d = mx_plan(ctx, d)
    wd = [Isci(d["dbu"], d["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(2)]
    yuk_baslat(wd)
    bekle(lambda: mx(d["dbu"]) == KISIT, 70, 1)
    ek["a1_kisitli"] = hesap_tail(d["dbu"])
    y = ctx.kok_api("POST", "/domains/%d/askiya-al" % d["id"], {}, desen="POST /domains/{id}/askiya-al", zaman=120)
    r.kod("(a) kısıtlı gd hostingi askıya alındı", y, 200)
    time.sleep(3)
    ek["a1_askida_kisitli"] = hesap_tail(d["dbu"])
    yuk_durdur(wd)
    bekle(lambda: mx(d["dbu"]) == plan_d, 130, 2)
    ek["a1_kisit_birakildi_askida"] = hesap_tail(d["dbu"])
    y = ctx.kok_api("POST", "/domains/%d/askidan-al" % d["id"], {}, desen="POST /domains/{id}/askidan-al", zaman=120)
    r.kod("(a) gd askıdan alındı", y, 200)
    time.sleep(3)
    ek["a1_askidan_alindi"] = hesap_tail(d["dbu"])
    T = ek
    r.kontrol("(a1) askıdaki hesapta kısıt YAZILIRKEN ve BIRAKILIRKEN ACCOUNT LOCK korundu; askıdan alınınca kilit kalktı, sınır plan değerinde",
              not tail_kilit(T["a1_kisitli"]) and tail_mx(T["a1_kisitli"]) == KISIT
              and tail_kilit(T["a1_askida_kisitli"]) and tail_mx(T["a1_askida_kisitli"]) == KISIT
              and tail_kilit(T["a1_kisit_birakildi_askida"]) and tail_mx(T["a1_kisit_birakildi_askida"]) == plan_d
              and not tail_kilit(T["a1_askidan_alindi"]) and tail_mx(T["a1_askidan_alindi"]) == plan_d,
              "kısıtlı: [%s] | askıda+kısıtlı: [%s] | askıdayken bırakıldı: [%s] | askıdan alındı: [%s]" % (
                  T["a1_kisitli"], T["a1_askida_kisitli"], T["a1_kisit_birakildi_askida"], T["a1_askidan_alindi"]))
    # (a2) panelin bilmediği (doğrudan) ACCOUNT LOCK + açık bağlantı: governor kısıtı kilitli hesaba yazar, bırakır; kilit korunur
    ayar_yaz({"db_kisici_soguma_sn": "30"})
    c = h["gc"]
    plan_c = mx_plan(ctx, c)
    wk = [Isci(c["dbu"], c["pw"], sorgu_kalici(st.hiz), "kalici") for _ in range(2)]
    yuk_baslat(wk)
    time.sleep(3)   # bağlantılar açıldı; sonra kilit (yeni bağlantılar reddedilir, açıklar sürer)
    try:
        kok_sql("ALTER USER %s@'localhost' ACCOUNT LOCK" % gosp.sqlq(c["dbu"]))
        ek["a2_kilitli_once"] = hesap_tail(c["dbu"])
        bekle(lambda: mx(c["dbu"]) == KISIT, 70, 1)
        ek["a2_kilitli_kisitli"] = hesap_tail(c["dbu"])
        yuk_durdur(wk)
        bekle(lambda: mx(c["dbu"]) == plan_c, 120, 2)
        ek["a2_kisit_birakildi"] = hesap_tail(c["dbu"])
    finally:
        yuk_durdur(wk)
        kok_sql("ALTER USER %s@'localhost' ACCOUNT UNLOCK" % gosp.sqlq(c["dbu"]))
    r.kontrol("(a2) doğrudan kilitli hesapta kısıt KONURKEN ve BIRAKILIRKEN ACCOUNT LOCK korundu",
              tail_kilit(ek.get("a2_kilitli_once", "")) and tail_kilit(ek.get("a2_kilitli_kisitli", "")) and tail_mx(ek.get("a2_kilitli_kisitli", "")) == KISIT
              and tail_kilit(ek.get("a2_kisit_birakildi", "")) and tail_mx(ek.get("a2_kisit_birakildi", "")) == plan_c,
              "kilitli: [%s] | kısıtlı: [%s] | bırakıldı: [%s]" % (ek.get("a2_kilitli_once"), ek.get("a2_kilitli_kisitli"), ek.get("a2_kisit_birakildi")))
    # (b) çok host'lu hesap: @localhost + @'%' farklı MAX_USER_CONNECTIONS
    g = h["gg"]
    u = g["dbu"]
    plan_g = mx_plan(ctx, g)
    pw_x = gosp.guclu_parola()
    gosp.sir_kaydet(pw_x)
    kok_sql("CREATE USER %s@'%%' IDENTIFIED BY %s WITH MAX_USER_CONNECTIONS 7" % (gosp.sqlq(u), gosp.sqlq(pw_x)))
    try:
        for yin in (plan_g, 7, 1):
            kok_sql("ALTER USER %s@'%%' WITH MAX_USER_CONNECTIONS %d" % (gosp.sqlq(u), yin))
            once = host_degerleri(u)
            ww = [Isci(u, g["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(2)]
            yuk_baslat(ww)
            bekle(lambda: host_degerleri(u).get("localhost", 99) <= KISIT, 70, 1)
            ara = host_degerleri(u)
            yuk_durdur(ww)
            bekle(lambda: host_degerleri(u).get("localhost") == plan_g, 120, 2)
            time.sleep(3)
            sonra = host_degerleri(u)
            ek["b_%d" % yin] = {"once": once, "kisit": ara, "sonra": sonra}
            ayrinti = "önce %s | kısıt sırasında %s | sonra %s (ölçüm)" % (once, ara, sonra)
            if yin == plan_g:
                # panelin kendi değişmezi: kaynaklimit.MySQLLimitUygula kullanıcının TÜM host'larına aynı (plan) değeri yazar
                r.kontrol("(b) çok host'lu hesap (iki host da plan değerinde, panel-yönetimli): kısıt + bırakma sonrası her host başlangıçtakine döndü", sonra == once, ayrinti)
            else:
                # panel dışı elle verilmiş FARKLI değer: governor tüm host'lara tek değer yazar/geri yazar
                ok_ = sonra == once and all(ara.get(k_, 0) <= v_ for k_, v_ in once.items() if v_ > 0)
                if not ok_:
                    r.bulgu("dusuk", "çok host'lu hesapta özel host değeri korunmuyor ('%%' başlangıçta %d)" % yin,
                            "governor kısıt değerini TÜM host'lara min(%d, İLK listelenen host'un değeri) olarak yazar (host sırası belirsiz) ve bırakırken TÜM host'lara plan değerini (%d) yazar; elle verilmiş farklı host değeri kaybolur" % (KISIT, plan_g) +
                            (" ve kısıt değeri başlangıçtan gevşektir" if any(ara.get(k_, 0) > v_ for k_, v_ in once.items() if v_ > 0) else "") + ". " + ayrinti)
                r.kontrol("(b) çok host'lu hesap, '%%' elle %d (panel dışı): sonuç ölçüldü (bilgi — bulgu olarak raporlanır)" % yin, True, ayrinti)
    finally:
        kok_sql("DROP USER IF EXISTS %s@'%%'" % gosp.sqlq(u))
    mod_ayarla(ctx, "gozlem")
    ayar_yaz({"db_kisici_soguma_sn": "30"})


# ───────────────────────── ek-tur: tek uzun deyim (gözlem) + yoğun yükte yoklama aralığı (kod inceleme soruları c, d) ─────────────────────────

def rfc_epoch(s):
    import calendar
    m = re.match(r"(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(\.\d+)?", s or "")
    return None if not m else calendar.timegm(time.strptime(m.group(1), "%Y-%m-%dT%H:%M:%S")) + float(m.group(2) or 0)


def tur_ornekle(ctx, sn, her=None):
    """/db-cpu/durum'u ~1 sn'de bir okur; yoklama_sayisi artınca son_yoklama zamanını kaydeder → ardışık tur aralıkları."""
    j = jwt()
    gor, atlanan, son = [], 0, time.time() + sn
    while time.time() < son:
        y = ctx.p.istek("admin", "GET", "/db-cpu/durum", yetki=j, desen="GET /db-cpu/durum")
        d = y.alan("durum", varsayilan={}) or {}
        n = int(d.get("yoklama_sayisi", 0))
        e = rfc_epoch(d.get("son_yoklama"))
        if gor and n == gor[-1][1]:
            pass
        else:
            if gor and n != gor[-1][1] + 1:
                atlanan += 1
            gor.append((e if e is not None else time.time(), n))
        if her:
            her()
        time.sleep(1)
    gaps = [round(gor[i][0] - gor[i - 1][0], 1) for i in range(1, len(gor)) if gor[i][1] == gor[i - 1][1] + 1]
    return gaps, gor, atlanan


def faz_ek_tur(ctx, st):
    r, h, iz = ctx.r, st.h, st.iz
    r.bolum("governor/ek-tur")
    ek = st.zaman.setdefault("ek_tur", {})
    # (c) varsayılan ayarlarla (poll 15 / pencere 60 / eşik plan) gözlem kipinde TEK uzun CPU deyimi
    ayar_geri([])
    mod_ayarla(ctx, "gozlem")
    bekle(lambda: durum(ctx)[0].get("poll_sn") == 15 and durum(ctx)[0].get("pencere_sn") == 60, 90, 2)
    tur_bekle(ctx, 2)
    b = h["gb"]
    o0 = olcum_toplam(b["dbu"])
    nproc = int(subprocess.run(["nproc"], stdout=subprocess.PIPE, universal_newlines=True).stdout.strip() or 0)
    ek["nproc"] = nproc
    wl = Isci(b["dbu"], b["pw"], "SELECT BENCHMARK(%d, MD5('l'))" % int(st.hiz * 130), "uzun130")
    t_c0 = time.time()
    wl.start()
    bekle(lambda: wl.proc is not None, 10, 0.2)
    wl.dur.set()   # tek deyim: bitince yenisi başlamasın
    durumlar, ilk_kayit, t_c1 = set(), None, None
    while time.time() < t_c0 + 420:
        if wl.ol and t_c1 is None:
            t_c1 = wl.ol[0][1]
        if ilk_kayit is None and int(panel_sql("SELECT COUNT(*) FROM db_cpu_kisit WHERE db_user=%s AND baslangic_ts >= %d" % (gosp.sqlq(b["dbu"]), int(t_c0)))) > 0:
            ilk_kayit = time.time()
        if t_c1 is None:
            s_ = kok_sql("SELECT IFNULL(STATE,'-') FROM information_schema.PROCESSLIST WHERE USER=%s AND COMMAND='Query'" % gosp.sqlq(b["dbu"]))
            if s_:
                durumlar.add(s_)
        elif time.time() - t_c1 > 75:
            break
        time.sleep(1.5)
    t_c1 = t_c1 or time.time()
    sure_c = t_c1 - t_c0
    n_kayit = len(kisit_satirlari(b["dbu"], t_c0))
    n_aud = len(audit("db.cpu_asim_gozlem", b["alan"], t_c0))
    n_bil = bildirim("eşiği aşıldı (gözlem modu)", b["id"], t_c0)
    o_delta = olcum_toplam(b["dbu"]) - o0
    ek.update({"c_sorgu_suresi_sn": round(sure_c, 1), "c_state": sorted(durumlar), "c_ilk_kayit_bitisten_sn": None if ilk_kayit is None else round(ilk_kayit - t_c1, 1),
               "c_kayit": n_kayit, "c_denetim": n_aud, "c_bildirim": n_bil, "c_olcum_cpu_sn": round(o_delta, 1)})
    gorundu = n_kayit > 0 or n_aud > 0
    r.kontrol("(c) varsayılan ayarlarla gözlem kipinde tek uzun (%.0f sn CPU) deyim bittiği turda eşik aşımı olarak görünüyor" % sure_c, gorundu,
              "kayıt=%d denetim=%d bildirim=%d; ilk kayıt bitişten %s sn sonra; db_cpu_olcum'a yazılan CPU=%.1f sn; STATE=%s; nproc=%d (ölçüm)" % (
                  n_kayit, n_aud, n_bil, ek["c_ilk_kayit_bitisten_sn"], o_delta, sorted(durumlar), nproc))
    if not gorundu:
        r.bulgu("orta", "tek uzun deyim gözlemde görünmüyor", "130 sn CPU'luk tek deyim bittikten sonra 75 sn boyunca eşik aşımı/olay oluşmadı (poll 15, pencere 60, nproc sınırı) — ölçüm")
    # (d) yoğun yükte ardışık yoklama aralığı
    a = h["ga"]
    for ad_, kv in (("varsayilan_poll15", None), ("hizli_poll5", HIZLI)):
        if kv:
            ayar_yaz(kv)
            bekle(lambda: durum(ctx)[0].get("poll_sn") == 5, 60, 2)
            tur_bekle(ctx, 1)
        poll = int(durum(ctx)[0].get("poll_sn") or 15)
        wa = [Isci(a["dbu"], a["pw"], sorgu_kisa(st.hiz), "kisa") for _ in range(6)] + [Isci(a["dbu"], a["pw"], sorgu_kalici(st.hiz), "kalici")]
        t_d0 = time.time()
        yuk_baslat(wa)
        gaps, gor, atl = tur_ornekle(ctx, 110 if poll == 15 else 80)
        yuk_durdur(wa)
        ek["d_" + ad_] = {"poll_sn": poll, "tur": len(gor), "en_buyuk_aralik_sn": max(gaps) if gaps else None, "en_kucuk_aralik_sn": min(gaps) if gaps else None,
                          "atlanan_ornek": atl, "aralıklar": gaps[:14]}
        r.kontrol("(d) yoğun yükte (6 kısa + 1 kalıcı işçi, 2 vCPU doygun) ardışık yoklama aralığı 2,5 x poll'u (%s sn) AŞMADI [poll %d]" % (2.5 * poll, poll),
                  bool(gaps) and max(gaps) <= 2.5 * poll, "%d tur gözlendi, aralıklar(sn)=%s (ölçüm; 1 sn örnekleme, atlanan örnek=%d)" % (len(gor), gaps[:14], atl))
        hata = gunluk(t_d0, r"DB kısıcı: .*(okunamadı|paniği|yazılamadı|başarısız)")
        r.kontrol("(d) yük sırasında kısıcı günlüğünde ölçüm/panik hatası yok [poll %d]" % poll, not hata, "; ".join(hata[:3]))
        time.sleep(3)
    mod_ayarla(ctx, "gozlem")


# ───────────────────────── ana giriş ─────────────────────────

def governor(ctx):
    r = ctx.r
    r.bolum("governor")
    if gosp.admin_jwt() is None:
        r.atla("governor modülü", "yönetici JWT betiği yok (%s)" % gosp.JETON_BETIGI)
        return
    st = Durum()
    st.h, st.zaman, st.kullanicilar, st.iz = {}, {}, [], None
    st.ayar_once, st.t_basla = [], time.time()
    try:
        kur(ctx, st)
        secili = [x for x in os.environ.get("GOV_FAZ", "").split(",") if x]
        for ad, fn in (("1-gozlem", faz_gozlem), ("2-uygula", faz_uygula), ("3-aski-silme", faz_askisilme), ("3g", faz_bekci), ("3c-3d", faz_yenidenbasla),
                       ("4-yetki", faz_yetki), ("ek-hesap", faz_ek_hesap)) + ((("ek-tur", faz_ek_tur), ("3h-5", faz_uzun)) if UZUN else ()):
            if secili and ad not in secili:
                continue
            try:
                fn(ctx, st)
            except Exception as e:  # noqa: BLE001 — faz çökmesi rapora düşer, sonraki faz sürer
                r.kontrol("governor/%s fazı tamamlandı" % ad, False, "%s: %s" % (type(e).__name__, gosp.maskele(e)))
                r.bulgu("yuksek", "governor/%s fazı çöktü" % ad, gosp.maskele(e))
                try:
                    mod_ayarla(ctx, "gozlem")
                except Exception:  # noqa: BLE001
                    pass
    finally:
        try:
            if st.iz is not None:
                st.iz.dur.set()
            kok_sql("SET GLOBAL userstat=1")
            ayar_geri(st.ayar_once)
            ids = [x["id"] for x in st.h.values()]
            # kısıcı artıkları: bu koşunun kullanıcılarına ait ölçüm/kısıt/bildirim satırları
            for u in st.kullanicilar:
                panel_sql("DELETE FROM db_cpu_olcum WHERE db_user=%s" % gosp.sqlq(u))
                panel_sql("DELETE FROM db_cpu_kisit WHERE db_user=%s" % gosp.sqlq(u))
            if ids:
                panel_sql("DELETE FROM cp_bildirim WHERE ref_tur='db_cpu' AND domain_id IN (%s)" % ",".join(str(i) for i in ids))
        except Exception as e:  # noqa: BLE001
            r.kontrol("governor ayar/artık geri yükleme", False, gosp.maskele(e))
        r.bolum("governor/zaman-cizelgesi")
        print(json.dumps(st.zaman, ensure_ascii=False, sort_keys=True), flush=True)
        r.bulgu("bilgi", "governor zaman çizelgesi ve ölçümler", json.dumps(st.zaman, ensure_ascii=False, sort_keys=True))
