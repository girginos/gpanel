# destek-hub kurulum notu (182, bot.girginos.io) — ELLE; bu repo hiçbir şeyi otomatik kurmaz

> 🔴 KVKK: hukuki onay (md. 9 yurt dışı aktarım, aydınlatma metni) olmadan CANLIYA ALINMAZ.
> Bottaki aydınlatma metni TASLAKTIR (`internal/destek/merkez/metinler.go`, anahtar `kvkk`).

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
