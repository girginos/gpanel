# destek-hub kurulum notu (182, bot.girginos.io) — ELLE; bu repo hiçbir şeyi otomatik kurmaz

> 🔴 KVKK: hukuki onay (md. 9 yurt dışı aktarım, aydınlatma metni) olmadan CANLIYA ALINMAZ.
> Bottaki aydınlatma metni TASLAKTIR (`internal/destek/merkez/metinler.go`, anahtar `kvkk`).

## 0. Tek sayfa özet (sıra önemli)
| # | nerede | ne |
|---|---|---|
| 1 | 182 | kullanıcı/dizin + ikili (§1) |
| 2 | 182 | sırlar; `destek-hub anahtar-uret --cikti …/merkez-imza-anahtari` → **açık anahtarı not al** (§2) |
| 3 | 182 | `destek-hub.json` (fiyatlar girilmeden servis başlamaz) (§3) |
| 4 | 182 | systemd + nginx + Cloudflare (§4, §8) |
| 5 | 182 | `destek-hub webhook-kur` (§5) |
| 6 | 181 | `/etc/girginospanel/destek.json`: `merkez_url` + `merkez_anahtarlari` (adım 2'deki açık anahtar) + `bot_kullanici_adi` → panel yeniden başlat (§7) |
| 7 | 181 | ajan bağlı görünmeli (panelde şalter YOK; ajan anahtar varsa kendiliğinden bağlanır, acil kapatma: `/etc/girginospanel/destek-kapali`); eşleştirme **portaldan** (app.girginos.io → sunucu seç → KVKK → kod) → `/start` → bağlantı doğrudan etkin (§9, §10) |

Açık anahtar daha sonra yeniden görülebilir: `destek-hub acik-anahtar` (sırlar LoadCredential ile verilerek,
§5'teki `systemd-run` kalıbıyla; `portal-ic-anahtar` da LoadCredential listesine eklenmeli) → `<açık anahtar> <anahtar kimliği>`.

## 1. Kullanıcı, dizinler
```sh
useradd --system --no-create-home --shell /usr/sbin/nologin destekhub
install -d -m 0750 -o root -g destekhub /etc/girginos-destek
install -m 0755 destek-hub /usr/local/bin/destek-hub      # CGO_ENABLED=0 go build ./cmd/destek-hub
```
Veri dizini `/var/lib/girginos-destek` systemd `StateDirectory=` ile 0700 oluşturulur
(durum.json + konusma/*.jsonl; konuşma kaydı 30 gün sonra silinir).

## 2. Sırlar (root:root 0600; servis bunları YALNIZ LoadCredential ile görür)
| dosya | içerik |
|---|---|
| `/etc/girginos-destek/telegram-token` | BotFather token'ı |
| `/etc/girginos-destek/anthropic-key` | Anthropic API anahtarı |
| `/etc/girginos-destek/telegram-webhook-gizli` | `[A-Za-z0-9_-]{16,256}`, ör. `head -c 32 /dev/urandom \| base64 \| tr '+/' '-_' \| tr -d '='` |
| `/etc/girginos-destek/merkez-imza-anahtari` | Ed25519 tohumu: `destek-hub anahtar-uret --cikti /etc/girginos-destek/merkez-imza-anahtari` |
| `/etc/girginos-destek/portal-ic-anahtar` | **portal iç API Bearer sırrı** `[A-Za-z0-9_-]{32,256}`, ör. `head -c 48 /dev/urandom \| base64 \| tr '+/' '-_' \| tr -d '=\n' > …` (aynı değer portal sunucusunda da, kendi sır deposunda tutulur) |

`anahtar-uret` açık anahtarı ve anahtar kimliğini yazar → bu açık anahtar panele (ajan
doğrulayıcısına) GÖMÜLÜR. Anahtar değişirse tüm panellerin güncellenmesi gerekir.
Sır dosyası grup/diğer erişimine açıksa servis başlamaz (istisna: root grubunda 0440 —
systemd ≥ 256 LoadCredential biçimi). `portal-ic-anahtar` YOKSA servis başlamaz.

## 3. Yapılandırma
`deploy/destek-hub/destek-hub.json.ornek` → `/etc/girginos-destek/destek-hub.json` (root:destekhub 0640).
- `portal_bilet_anahtarlari`: portalın (lisans sunucusu) **bilet imza açık anahtar(lar)ı**, base64url
  (32 bayt Ed25519). Lisans sunucusundaki `LICENSE_SIGNING_KEY` özel anahtarının açık yarısı
  (panelin `biletAcikAnahtarPanel` sabiti hex'tir; burada base64url yazılır:
  `printf %s <hex> \| xxd -r -p \| base64 \| tr '+/' '-_' \| tr -d '='`). Boşsa HİÇBİR sunucu
  doğrulanamaz → portal kod alamaz (kapalı başarısız). Anahtar döndürmede eski+yeni birlikte yazılır.
- `ic_dinle` YALNIZ loopback olabilir (`127.0.0.1:8471`, `[::1]:…`, `localhost:…`); başka adres → servis başlamaz.
- **Fiyatlar koda gömülü değildir.** Sonnet/Opus satırları 0'dır → servis başlamaz; güncel
  Anthropic fiyatlarını (giriş, çıkış, 5 dk / 1 sa cache yazma, cache okuma; $/MTok) girin.
- Model kimliklerini canlıya almadan önce Models API ile doğrulayın (SDK v1.66'da
  `claude-sonnet-5` / `claude-opus-5` sabitleri var; `-5-5` kimlikleri kullanıcı kararıdır).
- `destek_grubu_id`: insan devri Telegram grubu (bot grupta üye olmalı); 0 → yalnız log.
- Acil kesici: `touch /etc/girginos-destek/acil-kesici` → tüm Claude çağrıları anında durur,
  kullanıcılar insan desteğine aktarılır; `rm` ile geri açılır (yeniden başlatma gerekmez).

## 4. systemd + nginx
```sh
cp deploy/destek-hub/destek-hub.service /etc/systemd/system/
systemctl daemon-reload && systemctl enable --now destek-hub
cp deploy/destek-hub/nginx-bot.girginos.io.conf /etc/nginx/conf.d/   # Origin CA sertifikası yollarını düzeltin
nginx -t && systemctl reload nginx
```
Cloudflare: bot.girginos.io turuncu bulut, SSL Full (strict), WebSockets açık.
Cloudflare IP listesini https://www.cloudflare.com/ips/ ile karşılaştırın.

## 5. Webhook
```sh
systemd-run --pipe --wait -p User=destekhub \
  -p LoadCredential=telegram-token:/etc/girginos-destek/telegram-token \
  -p LoadCredential=anthropic-key:/etc/girginos-destek/anthropic-key \
  -p LoadCredential=telegram-webhook-gizli:/etc/girginos-destek/telegram-webhook-gizli \
  -p LoadCredential=merkez-imza-anahtari:/etc/girginos-destek/merkez-imza-anahtari \
  -p LoadCredential=portal-ic-anahtar:/etc/girginos-destek/portal-ic-anahtar \
  /usr/local/bin/destek-hub webhook-kur --url https://bot.girginos.io/v1/telegram/webhook
```
(Yalnız `message` ve `callback_query` güncellemeleri; gizli başlık zorunlu.)

## 6. Gözlem
- `curl -s 127.0.0.1:8471/metrik` → çevrimiçi ajan, etkin oturum, cache isabet oranı (model başına),
  günlük/aylık genel maliyet, sayaçlar (webhook_red, arac_red_merkez, cikis_red, insan_devri, …).
- `journalctl -u destek-hub` → JSON log; "prompt cache isabet oranı düşük" UYARISI önek
  bozulmasını gösterir (ToolsJSON/SistemSabit/model değişimi).

## 7. Panel (ajan) tarafı — 181 ve müşteri sunucuları
Ajan panelden AYRI hizmettir (`girginospanel-destek-ajan.service`, kaynak `cmd/destek-ajan` + `internal/destek/ajan`; ayrıntı: `deploy/destek-ajan/KURULUM.md`). Panel çökse de destek sürer. Modül varsayılan
Panelde şalter YOK (2026-10-04): ajan merkez anahtarı varsa her zaman bağlanır; yerel acil kapatma = root dosyası `/etc/girginospanel/destek-kapali`.

`/etc/girginospanel/destek.json` — **root sahipli, grup/diğer YAZAMAZ** (ör. `root:root 0600`);
aksi halde dosya YOK SAYILIR (panelde "yapılandırma dosyasının sahibi/izni güvensiz"). Bilinmeyen
alan → dosya çözülemez sayılır.
```json
{
  "merkez_url": "https://bot.girginos.io",
  "merkez_anahtarlari": ["<anahtar-uret çıktısındaki açık anahtar, base64url>"],
  "bot_kullanici_adi": "GirginOSDestekBot"
}
```
- `merkez_url`: yalnız `https://` (yol/sorgu yok). Boşsa `https://bot.girginos.io`.
- `merkez_anahtarlari`: meydan okuma ve TÜM imzalı mesajlar (eşleştirme onayı, iş emri) bu
  SABİT anahtarlarla doğrulanır — merkez Cloudflare arkasında olduğundan TLS merkezi kanıtlamaz.
  Yanlış/eksik anahtar → ajan bağlanmaz (panel durum sayfasında "meydan okuma doğrulanamadı"); cihaz imzası
  verilmez. Anahtar döndürmede eski+yeni birlikte yazılır, tüm paneller güncellenince eski çıkarılır.
  Yayın derlemesinde üretim anahtarı `-ldflags "-X girginospanel/internal/destek/ajan.GomuluMerkezAnahtari=<açık anahtar>"`
  ile de gömülebilir (dosyadakilerle BİRLEŞİR).
- `bot_kullanici_adi`: `t.me/<bot>?start=<kod>` bağlantısı ve QR için (deseni `…bot` ile biter).
  Boşsa panel yalnız kodu gösterir.
- Cihaz kimliği `/etc/girginospanel/destek-cihaz.key` ilk bağlantıda üretilir (0600); merkezdeki
  sunucu kimliği bu anahtardan türetilir (`s_<16 hex>`). Silinirse sunucu merkezde YENİ cihaz olur.
- Lisans ispatı: ajan, lisans sunucusunun imzaladığı çalışma biletini (`/var/lib/girginospanel-mail/bilet`;
  yol adı mail kalıntısıdır, bilet her ücretli/lisanslı eklenti aktivasyonu ve nabzında YAZILIR; yoksa
  `bilet-yok`) gönderir. Merkez bileti `portal_bilet_anahtarlari` ile DOĞRULAR (imza, süre, parmak izi,
  durum, askı); geçemeyen sunucu yine bağlanır ama "doğrulanmadı" sayılır ve **eşleştirme kodu alamaz**.
- Değişiklikten sonra `systemctl restart girginospanel-destek-ajan` (ajan ayrı hizmettir).

## 8. Cloudflare notları (bot.girginos.io)
- Turuncu bulut, SSL/TLS **Full (strict)** + Origin CA sertifikası; **WebSockets açık**.
- WAF/Bot Fight Mode/Super Bot Fight Mode `/v1/ajan/*` ve `/v1/telegram/webhook` yollarına
  meydan okuma (challenge) UYGULAMAMALI — ajan ve Telegram tarayıcı değildir. Gerekirse bu iki
  yol için "Skip" kuralı yazın; webhook'u Telegram IP aralıklarıyla (149.154.160.0/20,
  91.108.4.0/22) sınırlamak isteğe bağlıdır (asıl doğrulama gizli başlıktır).
- Zaman aşımları: CF'nin 100 sn sınırı yeterli (WSS kalp atışı 20 sn, `/al` en çok 25 sn).
- Uzun yoklamada ajan her `gonder`'i ayrı POST eder; yeniden bağlanışta senkron (bekleyen kodlar,
  bağlantı durumları, son 7 günün iptalleri) kısa sürede onlarca POST üretebilir. nginx'te
  `gonder` ucu kayıt/yanıt uçlarından ayrı ve daha geniş hız sınırındadır (aksi halde 429 →
  oturum düşer → yeniden senkron döngüsü).
- WSS kurulamazsa (kurumsal vekil, CF kuralı) ajan kendiliğinden uzun yoklamaya geçer; panelde
  bağlantı türü "yoklama" görünür.

## 9. Portal iç API'si (`/ic/v1/*`, yalnız `ic_dinle`)
Hepsi `Authorization: Bearer <portal-ic-anahtar>` ister (yok/yanlış → 401 `{"hata":"yetkisiz"}`).
Portala erişim bu repo dışındaki bir katmandır (SSH tüneli ya da allowlist'li vekil); `ic_dinle`
dış arayüze AÇILMAZ.
| uç | gövde / sorgu | başarı | hata kodları |
|---|---|---|---|
| `GET /ic/v1/sunucu?parmak_izi=<64hex>` | — | `{bulundu,cevrimici,dogrulandi,cakisma,sunucu_kimligi,panel_surumu,son_gorulme}` | 400 `gecersiz_istek` |
| `POST /ic/v1/eslesme-kodu` | `{parmak_izi,duzey:okuma\|onarim\|tam,sure_gun:1..30,portal_kullanici_id,aydinlatma_surum}` | `{kod,gorunen,bitis,bot_url,sunucu_kimligi}` | 400 `gecersiz_istek`, 404 `sunucu_yok`, 403 `dogrulanmadi`, 409 `cevrimdisi`, 409 `parmak_izi_cakisma`, 429 `kota` |
| `GET /ic/v1/baglantilar?parmak_izi=` | — | `{baglantilar:[{baglanti_id,sunucu_kimligi,tg_kisa,durum,rol,duzey,kvkk_kabul,kvkk_surum,kvkk_kaynak,guncelleme}]}` | 400 |
| `POST /ic/v1/baglanti/iptal` | `{parmak_izi,baglanti_id}` | `{ok,ajana_iletildi}` | 400, 404 `baglanti_yok` |
Ortak: 429 `kota` (oran sınırı), 413 `govde_buyuk` (>4 KiB), 503 `ic_api_kapali`.
Kod bellekte tutulur (≤10 dk, tek kullanımlık, sunucu başına 3, toplam 2000); servis yeniden başlarsa düşer.

## 10. Uçtan uca doğrulama
- Repo içi: `go test ./internal/destek/e2e/` (geçici MariaDB ile; `GOSP_TEST_GECICI_DSN`) gerçek
  merkez ↔ gerçek ajan ↔ gerçek araç yürütücüsünü WSS ve uzun yoklama üzerinden koşar
  (sahte Claude + sahte Telegram API; gerçek dış çağrı yok).
- Canlı: 181de ajan çalışıyor → `curl -s 127.0.0.1:8471/metrik` (182) `cevrimici_ajan ≥ 1`;
  portal (ya da `curl` + Bearer) ile `POST /ic/v1/eslesme-kodu` → `bekleyen_kod` artar → Telegram'da
  `/start <kod>` (`ABCD-EFGH-…` biçimi, küçük harf ve boşluk serbest) → bağlantı doğrudan etkin
  (KVKK portalda alındı; düğme yok) → "sitem açılıyor mu?" → panel "İşlemler" listesinde `site_yokla=ok`.

## 11. KVKK davranışları (maskeleme katmanları, silme, saklama)

> Kod: `internal/maske/` (kurallar), `internal/destek/merkez/{dongu,bot,depo}.go`, `internal/destek/ajan/unutma.go`.
> Hukuki metinler TASLAKTIR; bu bölüm yalnız **kodun bugünkü davranışını** anlatır.

### 11.1 Maskeleme katmanları

| Katman | Nerede | Ne maskelenir |
|---|---|---|
| Kullanıcı metni | Hub, **ilk işlem** (`Bot.destekIsle` → `Merkez.maskeliKullaniciMetni`); komutlar (`/start KOD` …) bundan önce ayrılır, maskelenmez | bilinen sırlar, sır desenleri (parola/anahtar/jeton/özel anahtar/DSN/Bearer/JWT…), **T.C. kimlik no** (11 hane + resmi sağlama), **IBAN** (ISO 13616 mod-97; TR için 26 hane), **kart** (13–19 hane, Luhn, boşluk/tire), **telefon** (TR: `+90`/`0090`/`0`+10 hane/`5xx xxx xx xx`; `+` ile başlayan uluslararası 8–15 hane), **"şifrem …/parolam …/password …"** ifadeleri, e-posta (yerel kısım `a***`, **alan adı kalır**), IPv4 /24 ve IPv6 /48 |
| Araç sonucu — ajan | Müşteri sunucusu (`arac`, `ajan`) | sırlar + IP/e-posta/ev dizini (mevcut) + sağlamalı tanımlayıcılar |
| Araç sonucu — hub (2. geçiş) | `Merkez.aracSonucuMaskele`, `GuvenilmezSar`'dan **önce** | bilinen sırlar, sır desenleri, T.C. no/IBAN/kart. **Telefon, IP, e-posta, "şifrem" kuralları UYGULANMAZ** (port/PID/IP teşhisi bozulmasın) |
| Konuşma kaydı, insan devri | Hub | Kullanıcı metni zaten maskeli gelir; ayrıca mevcut satır maskesi |
| Model çıkış süzgeci | Hub | (mevcut, değişmedi) |

Maskeleme belirlenimci ve idempotenttir. Önce maske, **sonra** 4000 rune kesmesi (kesme sırrı bölüp maskeyi kaçıramaz).
**Maskelenmeyenler (bilinen sınır):** ad-soyad, adres, alan adları, 11 haneli ama sağlaması geçmeyen sayılar, boşlukla bölünmüş T.C. no, noktalı telefon (IP/sürümle çakışır).

### 11.2 `/unutbeni` — silme ve anonimleştirme

**Hub'da SİLİNİR** (tek işlemde, `DosyaDepo.KullaniciUnut`): `durum.json` içindeki kullanıcı tercihi ve **tüm** bağlantılar; `konusma/*.jsonl` içinde kullanıcıya ait **tüm satırlar** (her gün dosyası atomik yeniden yazılır); bellek oturumları; bekleyen eşleştirme kayıtları; hız sayaçları. Silme anında uçuşta olan işlem sonradan satır ekleyemesin diye 10 dk'lık **yalnız bellek** mezar taşı tutulur (diske yazılmaz; yeniden eşleştirmede kalkar).

**Ajanda (sunucu DB) ANONİMLEŞTİRİLİR** — silinmez: `destek_baglantilari.tg_user_id → 0`, `tg_kullanici_adi → ''` (şema `tg_user_id NOT NULL`, tekillik yok → **yeni göç gerekmedi**); `audit_log.actor_username` ve `target` içindeki `tg:<kısa>` → `tg:silindi`. Aynı kullanıcının önceden iptal olmuş satırları da temizlenir. Ödünleşim: **güvenlik denetim izi "ne yapıldı"yı korur, "kim (hangi Telegram hesabı)"ı yitirir.** Tetik: `BaglantiIptal` nedeni `unutbeni`, `kullanici` veya `merkez`.

**Çevrimdışı ajan:** hub kaydı hemen silinir; ajan yeniden bağlanınca bağlantıyı bildirir, hub'da karşılığı olmadığı için `BaglantiIptal{merkez}` döner ve anonimleştirme **o anda tamamlanır**. Bot yanıtı iki durumu ayrı söyler (`unut_tamam`, `unut_tamam_bekliyor`) ve silinemeyenleri sayar.

**Silinmeyenler:** Telegram sohbet geçmişi (bot silemez), insan devrinde destek grubuna giden mesaj, Anthropic/Telegram tarafı, nginx/journald günlükleri (tg **özeti** içerebilir), portal onay kayıtları (`destek_kvkk_kabul`, değiştirilemez — ayrı karar), `audit_log` satırlarının kendisi (yalnız Telegram kimliği alanları anonimleşir).

### 11.3 Saklama süreleri (kodun bugünkü değeri)

| Veri | Süre |
|---|---|
| Hub konuşma kaydı (maskeli) | `konusma_saklama_gun` (≤30 gün), gün dosyası bütünüyle budanır |
| Ajan iş kayıtları (`destek_islemleri`) | 180 gün |
| İptal edilmiş bağlantı satırındaki Telegram kimliği | **30 gün** sonra süpürge anonimleştirir (yalnız bağlantı satırı; `audit_log`'a dokunmaz) |
| `audit_log` | Bu modül süre koymaz (panel politikası) |

### 11.4 Bot KVKK metni sürümü

Bot içi metin (`metinler["kvkk"]`) kendi sürüm etiketini taşır (`KvkkMetinSurumu`, şimdi `taslak-1`); `TestKvkkMetinSurumuMetinleEsit` metin ile sabit ayrışırsa düşer. Telegram düğmesiyle alınan kabul (yalnız portal onayı olmayan **eski** kayıtlarda mümkün) artık `kvkk_kaynak=telegram`, `kvkk_surum`, zaman damgası yazar. TODO: portal `taslak-2` kesinleşince bot metni portalla eşitlenir.
