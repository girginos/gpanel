# API sözleşme test takımı

Panelin dış API davranışını **gerçek bir test panelinde** uçtan uca ölçer: durum kodları, hata iletileri,
yan etkiler (site 503, FTP, müşteri/bayi girişi, oturum düşürme, anahtar iptali), izin matrisi, IDOR,
ham SQL sızıntısı, kalıntı. Kaynak: 2026-10-13 çok ajanlı doğrulama raporlarının test satırları ve o
gün düzeltilen bulgular. **Bir bulgu düzeltildiğinde buraya kalıcı bir kontrol eklenir.**

> Yalnız TEST panelinde (gp-prova VM'leri) root olarak çalıştırın: panel DB'sine geçici API anahtarı
> ekler, `sozl`/`sozk` önekli bayi, paket ve hosting açar, sonunda siler.

| Dosya | Görev |
|---|---|
| `gosp.py` | Ortak kütüphane: kapıya istek, geçici anahtar (yalnız SHA-256 özeti DB'ye), rastgele 127.0.0.x olumsuz giriş kaynağı, site/FTP kontrolü, önekli temizlik + kalıntı, sır maskeleme, rapor |
| `moduller.py` | Modüller: `kapi`, `anahtar`, `hosting`, `parola`, `sso`, `bayi`, `aski`, `idor`, `sizinti` |
| `kos.py` | Koşucu: `python3 kos.py [--modul a,b] [--json çıktı.json]` — çıkış kodu 0 = hepsi geçti |
| `arac.py` | Tek adımlı komut satırı ucu (MCP `vps_*` araçları bunu çağırır); sırlar adla anılır, çıktıya yazılmaz |
| `governor.py` | AĞIR modül `governor` (varsayılan koşuda YOK): MariaDB CPU kısıcısı (`internal/dbgovernor`) için yapay yükle gerçek-VPS sınaması — ölçüm doğruluğu, gözlem/uygula kipleri, seviye 1→2→3 zamanları, askı/silme/parola/yeniden başlatma kenar durumları, eski yavaş-sorgu bekçisiyle çakışma, yetki, denetim/bildirim; `GOV_UZUN=1` ile userstat kapalı davranışı, 50 kullanıcılı ölçek ve userstat QPS ölçümü. Kısıcı ayarlarını kısaltır, MariaDB'yi ve paneli yeniden başlatır, sonunda ayarları geri yükler |
| `belge.py` | Belge ↔ gerçek: gözlenen hata yanıtları belge sayfalarının hata tablolarında var mı |

Örnek (181'den VM-B'ye):

```bash
tar -C tests/sozlesme -cf - . | ssh root@<vm> 'mkdir -p /root/sozlesme && tar -C /root/sozlesme -xf -'
ssh root@<vm> 'cd /root/sozlesme && python3 kos.py --json /root/sozlesme-son.json'
scp root@<vm>:/root/sozlesme-son.json /tmp/ && python3 tests/sozlesme/belge.py --json /tmp/sozlesme-son.json --belge <girginos-docs>/icerik/tr/api
```

Yalnız Python 3 standart kütüphanesi kullanılır.

## governor modülü (ağır)

```bash
python3 kos.py --onek sozg --modul governor                      # ~15 dk
GOV_UZUN=1 python3 kos.py --onek sozg --modul governor           # ~32 dk (+ tek uzun deyim, yoklama aralığı, userstat kapalı, 50 kullanıcı, QPS)
GOV_FAZ=2-uygula,ek-hesap python3 kos.py --onek sozg --modul governor   # yalnız seçili fazlar (kurulum her zaman koşar)
```

Fazlar: `1-gozlem`, `2-uygula`, `3-aski-silme`, `3g`, `3c-3d`, `4-yetki`, `ek-hesap` (ACCOUNT LOCK korunumu + çok host'lu hesap), `ek-tur` (varsayılan ayarlarla tek uzun deyim + yoğun yükte yoklama aralığı; yalnız `GOV_UZUN=1`), `3h-5` (userstat kapalı, 50 kullanıcı ölçeği, userstat QPS; yalnız `GOV_UZUN=1`).
**Yalnız 2 çekirdekli test VPS'inde (gp-prova) koşun:** yük üreticisi CPU'yu doyurur, MariaDB ve `girginospanel` servisi
yeniden başlatılır. Koşu `cp_ayarlar.db_kisici_*` satırlarını kısaltır ve sonunda başlangıç haline (satır yok, kip `gozlem`) döndürür.
