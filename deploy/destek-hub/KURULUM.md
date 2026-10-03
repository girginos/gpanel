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
| 7 | 181 | panel → Telegram Destek: kök şalteri aç → "bağlı" görünmeli; eşleştir → `/start` → panelde onay → KVKK (§9) |

Açık anahtar daha sonra yeniden görülebilir: `destek-hub acik-anahtar` (sırlar LoadCredential ile verilerek,
§5'teki `systemd-run` kalıbıyla) → `<açık anahtar> <anahtar kimliği>`.

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

`anahtar-uret` açık anahtarı ve anahtar kimliğini yazar → bu açık anahtar panele (ajan
doğrulayıcısına) GÖMÜLÜR. Anahtar değişirse tüm panellerin güncellenmesi gerekir.
Sır dosyası grup/diğer erişimine açıksa servis başlamaz.

## 3. Yapılandırma
`deploy/destek-hub/destek-hub.json.ornek` → `/etc/girginos-destek/destek-hub.json` (root:destekhub 0640).
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
  /usr/local/bin/destek-hub webhook-kur --url https://bot.girginos.io/v1/telegram/webhook
```
(Yalnız `message` ve `callback_query` güncellemeleri; gizli başlık zorunlu.)

## 6. Gözlem
- `curl -s 127.0.0.1:8471/metrik` → çevrimiçi ajan, etkin oturum, cache isabet oranı (model başına),
  günlük/aylık genel maliyet, sayaçlar (webhook_red, arac_red_merkez, cikis_red, insan_devri, …).
- `journalctl -u destek-hub` → JSON log; "prompt cache isabet oranı düşük" UYARISI önek
  bozulmasını gösterir (ToolsJSON/SistemSabit/model değişimi).

## 7. Panel (ajan) tarafı — 181 ve müşteri sunucuları
Ajan panel sürecinin içindedir (`internal/destek/ajan`); ayrı servis yoktur. Modül varsayılan
KAPALI: kök "Telegram Destek" sayfasından şalter açılmadıkça merkeze hiç bağlanmaz.

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
  Yanlış/eksik anahtar → ajan bağlanmaz (panelde "meydan okuma doğrulanamadı"); cihaz imzası
  verilmez. Anahtar döndürmede eski+yeni birlikte yazılır, tüm paneller güncellenince eski çıkarılır.
  Yayın derlemesinde üretim anahtarı `-ldflags "-X girginospanel/internal/destek/ajan.GomuluMerkezAnahtari=<açık anahtar>"`
  ile de gömülebilir (dosyadakilerle BİRLEŞİR).
- `bot_kullanici_adi`: `t.me/<bot>?start=<kod>` bağlantısı ve QR için (deseni `…bot` ile biter).
  Boşsa panel yalnız kodu gösterir.
- Cihaz kimliği `/etc/girginospanel/destek-cihaz.key` ilk bağlantıda üretilir (0600); merkezdeki
  sunucu kimliği bu anahtardan türetilir (`s_<16 hex>`). Silinirse sunucu merkezde YENİ cihaz olur.
- Lisans ispatı: Faz 1'de ajan lisans biletini (yoksa `bilet-yok`) gönderir; merkez Faz 1'de
  içeriği DOĞRULAMAZ (log'da bir kez uyarır). Faz 2: lic-eu doğrulaması.
- Değişiklikten sonra `systemctl restart girginospanel`.

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

## 9. Uçtan uca doğrulama
- Repo içi: `go test ./internal/destek/e2e/` (geçici MariaDB ile; `GOSP_TEST_GECICI_DSN`) gerçek
  merkez ↔ gerçek ajan ↔ gerçek araç yürütücüsünü WSS ve uzun yoklama üzerinden koşar
  (sahte Claude + sahte Telegram API; gerçek dış çağrı yok).
- Canlı: 181'de kök şalteri açık → `curl -s 127.0.0.1:8471/metrik` (182) `cevrimici_ajan ≥ 1`;
  panelde kod üret → `bekleyen_kod` artar → Telegram'da `/start <kod>` (paneldeki
  `ABCD-EFGH-…` biçimi, küçük harf ve boşluk serbest) → panelde onay → KVKK düğmesi →
  "sitem açılıyor mu?" → panel "İşlemler" listesinde `site_yokla=ok`.
