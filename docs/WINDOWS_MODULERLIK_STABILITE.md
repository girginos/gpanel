# gPanel Windows — Modülerlik ve Stabilite Altyapısı

> Kaynak ilke: *GirginosVM Yazılım Geliştirme & Stabilite Rehberi* (18 madde).
> Amaç: **bir özelliği düzeltirken başkasını bozmamak.** Bu belge rehberin her
> maddesini gPanel Windows tarafındaki gerçek koda eşler (✅ var / 🟡 kısmi /
> ⬜ yok), denetim bulgularını izlenen teknik borç olarak tutar (madde 16) ve
> mimari kararları kaydeder (madde 17).

Kapsam: `internal/platform/*_windows.go` (site/db/servis/kurulum/olay/görev) +
`cmd/girginospanel-agent` (ajan, yerel panel, uçlar). Linux paneli AYRI kanal,
AYRI binary — buradaki hiçbir değişiklik Linux'u etkilemez (bkz. ADR-1).

---

## 1. Rehber maddeleri → kod durumu

| # | İlke | Durum | Nerede / not |
|---|------|-------|--------------|
| 1 | Modüler mimari | ✅ | `platform.Saglayici` sözleşmesi + `Yetenek` bit alanı modül sınırı; build-tag ile OS ayrımı; ajan AYRI binary. Web katmanı appcmd'ye uzanmıyor (`GET /siteler` → `platform.SiteListe`, B-08 ✅). |
| 2 | Regression test sistemi | ✅ | Düzeltilen her zafiyet bir testle kilitli — VM'de 15 test (kimlik/doğrulama/kaçış/parse/indirme/havuz/komut/pg); **injection-defense testleri Linux CI'da da koşar** (B-13, `go test ./internal/platform`). |
| 3 | Test piramidi | 🟡 | Unit (yeni) + E2E (VM: `kendini-sina` + create/delete/DB döngüleri). Integration + API-contract katmanı bekliyor. |
| 4 | State machine | ✅ | Kurulum işi: `kosuyor/basarili/basarisiz/kesildi/kismi` + tek-uçuş **(restart'a dayanıklı disk kilidi ✅ B-05)**; yarım kurulum `kismi` (✅ B-07); site yaşam döngüsü LIFO rollback (`geriAl`); ajan güncelleme rollback (B-01). |
| 5 | Idempotency | ✅ | DB oluştur (IF NOT EXISTS + login parola yakınsama), MSSQL kurulum (motor varsa onarım), site yeniden-oluştur (sahip işareti), servis başlat/durdur — hepsi bu turda idempotent. Bekliyor: uçlarda request_id ile mükerrer-istek engeli. |
| 6 | Asenkron job | ✅ | Kurulum `202 + is_id`, ilerleme `GET /katalog/is` ile yoklanır. |
| 7 | Timeout / retry / breaker | 🟡 | Her komut+indirmede 30 dk timeout, ctx iptali; **indirmede üssel-backoff retry + Range resume ✅ (B-06)**. Bekliyor: kurucu komutlarda circuit-breaker (düşük öncelik). |
| 8 | Observability | 🟡 | `ajan.log` + giriş/site/kurulum logları; **tüm mutasyonlarda `denetimli` audit logu + `istek_id` korelasyonu ✅ (B-09)** (aktör + ad + sonuç + süre; id yanıt başlığında + hata zarfında); **kurulumda GERÇEK ZAMANLI yapısal ilerleme (SSE `/katalog/is-akis`) + canlı ETA çubuğu ✅ (B-16)** (indirmede %+hız+ETA; kurmada dürüst belirsiz). Bekliyor: metrics + tracing (düşük öncelik). |
| 9 | Standart error modeli | ✅ | `hata_windows.go`: `{kod, mesaj, hata(geriye-uyum), istek_id, yeniden}` zarfı + makine kodları (`kodCoz` sentinel→kod+durum+retryable); ops+veri+yerelpanel yerel panel API'si tek `hataYaz`/`yontemHatasi`/`govdeHatasi` kullanır. VM'de doğrulandı (405/400/422 + kod + istek_id; webui `hata` alanı korundu, statüler değişmedi). |
| 10 | Security development lifecycle | 🟡 | Enjeksiyon kapalı (doğrulama + shell'siz exec), allowlist, TLS+TOFU, bcrypt, **indirme SHA-256 ✅ (B-12)**, **havuz adı regex ✅ (B-15)**, **CSRF Origin↔Host ✅ (B-10)**, **pg parolası optionfile ✅ (B-11)**. Denetim güvenlik bulguları kapandı. |
| 11 | Backup/restore doğrulaması | ⬜ | Windows'ta henüz yedek/restore YOK. Eklenince Linux dersi zorunlu: checksum + test-restore + DÜRÜST raporlama (asla "tamam" deyip 0 iş yapma). |
| 12 | Release engineering | ✅ | Ayrı `windows/dev` kanalı + ayrı `Surum`; `kendini-sina` mührü host başına kanıt (sınanmamış hosta site düşmez). **Ajan `guncelle` komutu: on-sınama → atomik takas → health-gate (/saglik + sürüm) → gecmezse OTOMATIK ROLLBACK** (`guncelle_windows.go`, VM'de pozitif+negatif doğrulandı). |
| 13 | Feature flags | 🟡 | `Kademe` (guvenilir/deneysel/karar-bekliyor) kullanıcıya gösterilen kademeli açılım. Bekliyor: yüzdesel rollout. |
| 14 | Chaos testing | ⬜ | Bekliyor (node/servis kapatma, disk dolu, DB kesme senaryoları). |
| 15 | API contract & versioning | 🟡 | Tel sözleşmesi sabit (X-Gosp-Jeton, JSON şekilleri). Bekliyor: contract testleri, sürümleme. |
| 16 | Technical debt | ✅ | Bu belgenin §3 tablosu izlenen borç kaydıdır. |
| 17 | ADR | ✅ | §4. |
| 18 | Definition of Done | ✅ | §5. |

---

## 2. Bu turda kapatılan zafiyetler (Linux dersi + denetim)

> ✅ **VM DOĞRULAMASI (188.40.14.235, WS2019, agent 0.3.0-temel):** 7/7 birim testi
> geçti; `kendini-sina` geçti; E2E site (create→delete→recreate, 19 kr sk, sahip
> isareti yazildi/okundu, IIS'te gercek site); E2E DB (idempotent yeniden-create,
> yetim login silmede 1→0, silme sonrasi yeniden-create bloklanmadi); E2E servis
> (yanit YALNIZ gercek duruma ulasinca ok — Stopped/Running bagimsiz dogrulandi).
> Rollback binary'si VM'de `girginospanel-agent.exe.bak-stab`. Kaynak: 181
> `/root/gpanel-temiz` (gofmt temiz, GOOS=windows GOAMD64=v1 derlendi) — git'e
> COMMIT edilmedi, hicbir yere YAYINLANMADI.

Kullanıcının Linux'ta yaşadığı üç sınıf — **user oluşturmuyordu / siliniyordu /
geri yüklenmiyordu** — ve dört modül denetimi. Kapatılanlar:

- **DB idempotency + yetim login** (`veritabani_windows.go`): `CREATE DATABASE/USER/
  ROLE` artık `IF NOT EXISTS`; login varsa parola YENİDEN uygulanır (eski sürüm
  eski parolayı sessizce tutup siteyi bağlanamaz bırakıyordu). Silme, DROP DATABASE
  sonrası SUNUCU login'ini de düşürür — ama yalnız başka db'de eşlemesi kalmadıysa
  (fail-safe). *(Linux tenant_orphan_race'in MSSQL karşılığı.)*
- **Uç kayıt dikişi** (`yerelpanel_windows.go`): `OpsUclariniKaydet` + `VeriUclariniKaydet`
  çağrıları düşmüştü → servis/kaynak/site-detay/havuz/bağlama/SSL/vt uçlarının
  TAMAMI erişilemezdi. Yeniden bağlandı + başlangıç logu. *(Klasik "bir değişiklik
  başkasını sessizce bozdu" regresyonu.)*
- **ServisIslem sessiz başarısızlığı** (`servis_windows.go`): `sc start/stop`
  asenkron + `Restart-Service` `$LASTEXITCODE` set etmez → başarısız aksiyon
  `{"ok":true}` dönüyordu. Artık hedef duruma ulaşana kadar `Get-Service` ile
  doğrulanır; ulaşılmazsa dürüst hata. *("Başarısızlık güvence gibi görünür" dersi.)*
- **Çapraz-kiracı sızıntısı** (`windows.go`): SAM adı 12-char+16-bit → 8-char+32-bit
  (cakışma ~65536× zorlaştı) + `.gpanel-sahip` işareti (çakışan yeniden-kullanımı
  AÇIK reddet, silinen kurbanın webRoot'unu devralma yok) + silmede ACL sökümü +
  silmede `sk`'yi IIS'ten okuma (algoritma drifti orphan bırakmaz).
- **Kurulum izolasyonu + MSSQL onarımı** (`kurulum_windows.go`): kurucu goroutine'i
  `recover` ile sarıldı (bir kurulum paniği tüm ajanı düşürmez); MSSQL motor
  kuruluyken sqlcmd yarım kalırsa kalem "kurulabilir" kalır ve yeniden koşu
  yalnız sqlcmd'yi onarır (750 MB medya tekrar inmez).

Her biri için `windows_birim_test.go`'da (saf mantık) ve VM E2E'de (yan etkili yol)
doğrulama.

---

## 3. İzlenen teknik borç (öncelik sırası — rehber madde 16)

Öncelik rehberin kendi tablosuna göre: regression → state machine → update/rollback
→ observability → error model → …

| Kod | Öncelik | Bulgu | Kaynak |
|-----|---------|-------|--------|
| ~~B-01~~ | ✅ TAMAM | ~~Ajan güncellemesinde health-gate + rollback YOK.~~ **Yapıldı** — `guncelle_windows.go`: on-sınama + atomik takas + health-gate + otomatik rollback; VM'de pozitif (geçerli binary→commit) ve negatif (bozuk binary→rollback→eski sürüm sağlıklı) doğrulandı. | rehber #4,#12 |
| ~~B-04~~ | ✅ TAMAM | ~~Standart error zarfı yok; 3 farklı status eşlemesi.~~ **Yapıldı** — `hata_windows.go` zarfı; opsHataYaz/vtHataKodu/yerelpanel inline eşlemeleri tek `hataYaz`/`kodCoz`'a indirildi; geriye-uyum `hata` alanı + statüler korundu; VM'de doğrulandı. (Kalan: merkezi 8460 API + opsSiteSSL 200-içinde-başarısızlık + giris/cikis → ayrı, düşük öncelik.) | denetim 04/1,04/3 |
| ~~B-05~~ | ✅ TAMAM | ~~Kurulum tek-uçuş kilidi restart'ta sıfırlanır; öksüz msiexec + eşzamanlı CBS riski.~~ **Yapıldı** — kurulum başlarken diske marker (`kurulum-aktif.json`), bitince silinir; açılışta `KurulumAsiliYukle` marker'ı görürse yeni kurulumlar `409 KURULUM_ASILI` ile reddedilir; iş `kesildi` olarak gösterilir; operatör endpoint (`/kurulum-kilit-temizle`) veya CLI ile temizler. VM'de doğrulandı. | denetim 01/2 |
| ~~B-06~~ | ✅ TAMAM | ~~İndirmede retry/backoff/resume yok.~~ **Yapıldı** — `indir`: `.indiriliyor` geçici dosya + üssel-backoff retry (2/4/8 sn) + HTTP Range **resume** (206) + atomik `rename`; 404/403 kalıcı (retry yok). Birim testi (retry/resume/kalıcı) VM'de geçti. | denetim 01/5 |
| ~~B-07~~ | ✅ TAMAM | ~~`mysql` kurulumu çalışan örnek kurmadan "başarılı" döner.~~ **Yapıldı** — `ErrKismiKurulum` sentinel + `isKismi` durumu; `kurMySQL` (araç kuruldu, örnek yok) ve `kurPhpMyAdmin` (dosyalar açıldı, PHP gerekli) artık işi `kismi` işaretler + dürüst log. VM'de phpmyadmin kurulumu `durum=kismi` + açıklayıcı log ile doğrulandı. | denetim 01/3 |
| ~~B-08~~ | ✅ TAMAM | ~~Yerel panel `GET /siteler` appcmd'ye doğrudan uzanıyor + appcmd yolu kopyası.~~ **Yapıldı** — `platform.SiteListe()` + saf `siteSatirlariCoz` (mevcut `siteDetaySatirRe` yeniden kullanıldı); yerelpanel'den `panelAppcmdYolu`/`siteSatirRe`/regex ayıklama kaldırıldı (regexp/filepath/fmt importları da). Birim testi + gerçek `GET /siteler` (3 site, doğru şekil) VM'de doğrulandı. | denetim 04/4 |
| ~~B-09~~ | ✅ TAMAM | ~~Ayrıcalıklı mutasyonlarda (vt-sil dahil) denetim logu + correlation id yok.~~ **Yapıldı** — `denetimli` middleware: her mutasyona (havuz/bağlama/site-ssl/servis-işlem/vt-oluştur/vt-sil/siteler/katalog-kur/kilit-temizle) `istek_id` (yanıt başlığı + log), yapısal audit log (aktör RemoteAddr + ad + method + path + sonuç durum + süre); id handler↔log↔hata-zarfı boyunca aynı. VM'de header↔log korelasyonu doğrulandı. | denetim 04/5 |
| ~~B-10~~ | ✅ TAMAM | ~~CSRF yalnız SameSite=Strict; 0.0.0.0 bind'de aynı-host-farklı-port boşluğu.~~ **Yapıldı** — `denetimli`'de `csrfKontrol`: mutasyon Origin/Referer taşıyorsa host'u `Host` başlığıyla eşleşmeli, yoksa `403 CSRF_RED` (Origin/Referer yoksa araç kabul). VM: evil.com→403, aynı-origin→200, Origin-yok→200. | denetim 04/6 |
| ~~B-11~~ | ✅ TAMAM | ~~pg superuser parolası cmdline'da + zayıf 64-bit entropi.~~ **Yapıldı** — parola BitRock `--optionfile` (0600 geçici, kullan+sil) ile geçirilir, cmdline'da görünmez; entropi 64→128 bit. Birim test (`TestPgOptionDosyasi`). *(Tam E2E = 350 MB pg kurulumu; orantısız, koşulmadı.)* | denetim 01/7 |
| ~~B-12~~ | ✅ TAMAM | ~~İndirme bütünlük doğrulaması yok.~~ **Yapıldı** — akışta SHA-256 hesabı + `indirmeSha` haritasında pinli (sürüme-kilitli, 6 kalem: go-sqlcmd/win-acme/rewrite/mysql/phpmyadmin/node) beklenen hash'e karşı doğrulama; tutmazsa sil+retry. fwlink/aka.ms (mssql/dotnet) bilinçli pinlenmez (yanlış-pozitif); checksum yoksa açıkça loglanır. Gerçek `rewrite` kurulumunda canlı doğrulandı. | denetim 01/8 |
| ~~B-13~~ | ✅ TAMAM | ~~Saf mantık OS-nötr dosyaya taşınırsa Linux CI'da da unit test koşar.~~ **Yapıldı (çekirdek)** — `ErrGecersizIstek` → `platform.go`; SQL enjeksiyon savunması (`vtAdRe`/`vtKoseKacis`/`vtTirnakKacis`) → `veritabani_ortak.go` (etiketsiz) + `veritabani_ortak_test.go`; **181'de `go test ./internal/platform` (Linux native) TestVtAdRe/TestVtKacis GEÇTI**. Kalan saf fonksiyonlar (parser/exec-bitişik) OS-etiketli kalır (kasıtlı sınır). | rehber #1,#2 |
| ~~B-14~~ | ✅ TAMAM | ~~node/git tespiti çalışan sürecin bayat PATH'ini okur → kurulumdan sonra "kurulu değil" döngüsü.~~ **Yapıldı** — `komutVeyaDosya`: `LookPath`'e ek olarak sabit kurulum yollarını (`C:\Program Files\nodejs\node.exe`, `...\Git\cmd\git.exe`) `os.Stat` ile kontrol eder. Birim testi (14/14) + canlı katalog E2E (placeholder → `Kurulu=true`) doğrulandı. | denetim 01/4 |
| ~~B-15~~ | ✅ TAMAM | ~~`HavuzIslem`/`HavuzDotNet` havuz adı desen doğrulaması yok.~~ **Yapıldı** — `havuzAdiRe = ^gp_[a-z0-9]{1,20}$` + `havuzAdiDogrula`; sistem havuzları (DefaultAppPool) ve enjeksiyon karakterleri reddedilir. Birim testi (13/13) + E2E: DefaultAppPool→400, gp_yok→422 (gate ayrımı), `gp_x;calc`→400. | denetim 03/6 |

---

## 4. Mimari kararlar (ADR — rehber madde 17)

- **ADR-1 — Ayrı binary + ayrı kanal.** Windows ajanı panel binary'sine GÖMÜLMEZ;
  `cmd/girginospanel-agent`, `platform.Kanal="windows/dev"`, `Surum` Linux'tan
  bağımsız. *Gerekçe:* "Linux'ta bir şey bozarsak Windows bozulmasın" — ortak
  artefakt/sürüm bunu ilk gün kırardı. İki sabit BİRBİRİNE BAĞLANMAZ.
- **ADR-2 — Yetenek kapısı + kendini-sina mührü.** `YetSite` koddan açık gelmez;
  yalnız host'ta gerçek IIS yoluyla create/delete kanıtlanınca (mühür = o sürüm)
  açılır. *Gerekçe:* "yetenek ancak doğrulanınca ilan edilir" kağıtta değil makine
  başına mekanik. Yeni ajan sürümü kendini yeniden kanıtlar.
- **ADR-3 — Dürüstlük ilkesi.** Sahte başarı YASAK: yapılamayan işe "yapıldı"
  denmez (mysql/pgsql yönetimi parola saklanmadığı için açık hata verir; ServisIslem
  gerçek durumu doğrular). *Gerekçe:* Linux "başarısızlık güvence gibi görünür" dersi.
- **ADR-4 — Tek-uçuş kurulum.** Aynı anda en çok bir kurulum; ikinci istek kuyruğa
  değil açık redde (409). *Gerekçe:* eşzamanlı MSI/CBS = bozuk sistem.
- **ADR-5 — İki kapı, tek süreç, izole panel.** 8460 merkezi API + 8443 yerel panel
  ayrı goroutine'lerde; panel düşse merkezi API ayakta kalır. Kurulum goroutine'i
  `recover` ile sarılı: bir kalemin kurulumu ajanı düşürmez.

---

## 5. Definition of Done — Windows özelliği (rehber madde 18)

Bir Windows özelliği ancak şunlar tamamlanınca "done":

- [ ] Kod + saf mantık için birim testi (`windows_birim_test.go`)
- [ ] Yan etkili yol için VM E2E (create→delete→**yeniden create** + negatif kontrol)
- [ ] Hata yolu: sessiz başarı YOK — başarısızlık gerçek durumla doğrulanır
- [ ] Idempotent: aynı istek iki kez güvenli (yarım durum kurtarılabilir)
- [ ] Silme simetrik: oluşturmanın yarattığı her şey (kullanıcı/havuz/login/ACL) temizlenir ya da bilinçli korunur
- [ ] Yetki kontrolü (oturumlu/jeton) + enjeksiyon savunması (doğrulama + shell'siz exec)
- [ ] Log: en az operatör-görünür mutasyon kaydı
- [ ] Bu belge güncellendi (durum tablosu + borç)

---

## 6. Doğrulama komutları (VM)

```powershell
# Birim testleri (build host'ta capraz derle, VM'de kosur):
#   GOOS=windows go test -c ./internal/platform -o platform_test.exe
#   scp -> VM ; .\platform_test.exe -test.v

# E2E dongusu (VM, elle ya da betik):
#   site create -> delete -> ayni alan yeniden create (sahip isareti eslesir)
#   farkli alan ayni sk'ye zorlanirsa -> ACIK red
#   vt-olustur -> vt-sil -> ayni kullaniciyla yeniden olustur (parola yakinsar)
#   servis durdur (kapali servis) -> gercek durum dogrulanir, sahte ok yok
```

---

## 7. Kapanış durumu (2026-09-04)

Dört denetimin (kurulum 01/*, site 03/*, servis+uç 04/*, DB) **tüm somut bulguları
kapatıldı** ve backlog tükendi. Tamamlanan + VM'de doğrulanan kalemler:

**B-01** update+rollback engine · **B-04** standart error zarfı · **B-05** kurulum
disk-kilidi · **B-06** indirme retry+resume · **B-07** honest-partial (kismi) ·
**B-08** platform.SiteListe · **B-09** denetim logu+correlation · **B-10** CSRF
Origin↔Host · **B-11** pg parola optionfile · **B-12** indirme SHA-256 (8 kalem
pinli: go-sqlcmd/win-acme/rewrite/mysql/phpmyadmin/node/git/pgsql) · **B-13** OS-nötr
injection-defense testleri Linux CI'da · **B-14** node/git sabit-yol · **B-15** havuz
adı regex. **webui:** kismi rozeti + asılı-kilit-temizle düğmesi.

Test: VM'de 15 birim testi + Linux CI'da injection-defense (`go test ./internal/
platform`); her kalem ayrıca canlı E2E. Ajan güncellemeleri `guncelle` ile (B-01
dogfood). **İzolasyon korundu:** tüm bu değişikliklerden sonra Linux paneli (`go build
./cmd/server`) temiz derlendi.

**Kasıtlı deferrals (kapsam dışı, düşük değer):** B-13'ün parser/exec-bitişik saf
fonksiyonları OS-etiketli kalır (kasıtlı sınır); redis/dotnet checksum pinlenmez
(karar-bekliyor / aka.ms yönlendirme); Windows'ta yedek/restore henüz YOK (madde 11 —
eklenince Linux dersi zorunlu); metrics/tracing (madde 8) ve chaos (madde 14) sonraki
faz; merkezi 8460 API henüz error-zarfını kullanmaz (iç kanal, düşük öncelik).

🔴 **Yayın durumu:** hepsi 181 canonical `/root/gpanel-temiz`'te (untracked, gofmt+vet
temiz, GOOS=windows GOAMD64=v1 derlenir); VM'e `guncelle` ile kuruldu. **Git commit /
kanala yayın YOK** — kullanıcı "yayınla" demeden yapılmaz.

---

## 8. B-16 — Gerçek zamanlı ETA ilerleme çubuğu (2026-09-04)

Kurulum sihirbazı düz metin log ("20 MB indi") yerine **canlı çubuk**la izleniyor:

- **Yapısal ilerleme** (`Ilerleme`: asama/etiket/byte_inen/byte_toplam/yüzde/hız/ETA)
  `KurulumIsi`'ye eklendi. `ilerlemeOkuyucu` Content-Length'i (206'da baslangıç+kalan)
  yakalar, **ortalama** hızdan ETA hesaplar (~250 ms'de bir); 10 MB'lık metin log korundu.
- **Dürüst belirsizlik:** yükleyici koşarken (`kurulumKomutu`) süre gerçekten
  kestirilemez → asama="kuruluyor", Yüzde=-1 → UI belirsiz (sweep) çubuk + geçen/tahmini
  gösterir, **sahte ETA uydurmaz** (denetim 01/3 dersi).
- **SSE** `GET /api/yerel/katalog/is-akis` (text/event-stream, 500 ms tik, bitince kapan);
  `denetimli` ile SARILMADI (mutasyon değil). Yoklama ucu FALLBACK + artık ilerleme döner.
- **webui** (?v=042): EventSource (3.5 sn bekçi + CLOSED→yoklama; geçici kopmada
  oto-reconnect) + segmentli genel çubuk + kalem ETA çubuğu; `prefers-reduced-motion` guard.
- **Doğrulama:** 16 birim testi (yeni `TestIlerlemeYapisal`) + `go vet` temiz;
  `guncelle` ile deploy (on-test + health-gate + `.onceki` yedek); canlı tel doğrulaması
  (is-akis 401 → yeni kod canlı; servis edilen JS/CSS'te yeni semboller). Görsel render
  operatörün oturumunda (self-signed + parola sunucuda değil).

**Yayın durumu değişmedi:** 181 canonical'da, git commit / kanal yayını YOK.
