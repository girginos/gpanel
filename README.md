# GirginOSPanel

Boş bir **AlmaLinux 10** sunucuyu tek komutla komple bir hosting kontrol paneline çevirir — nginx + MariaDB + çok sürümlü PHP + Valkey (Redis) + phpMyAdmin + güvenlik duvarı, hepsi otomatik kurulur ve ayarlanır.

## Tek satır kurulum

Temiz bir AlmaLinux 10 (min. 2 GB RAM) sunucuda **root** olarak:

```bash
curl -fsSL https://raw.githubusercontent.com/girginos/gpanel/main/install.sh | bash
```

Kurulum ~5-10 dakika sürer (paket indirmeleri). Bittiğinde panel adresi + giriş bilgileri ekrana yazılır.

## Kurulum sonrası

- **Panel:** `https://SUNUCU_IP:8443` (self-signed sertifika — tarayıcı uyarısını geçin)
- **Giriş:** kullanıcı **`root`** · parola = **sunucunun root parolası**
  (panel yöneticisini işletim sistemi root'u üzerinden PAM ile doğrular; ayrı bir panel parolası yoktur)

## Ne kurar?

| Bileşen | Detay |
|---|---|
| **Web** | nginx (panel :8443 + müşteri siteleri :80/:443) |
| **PHP** | 7.4 / 8.2 / 8.3 / 8.4 / 8.5 (remi) — her domain bağımsız sürüm seçer, per-domain FPM havuzu |
| **Veritabanı** | MariaDB 10.11 (`panel` DB) + phpMyAdmin (`/pma/`) |
| **Cache** | Valkey (Redis) — per-tenant izole object cache (WordPress'e otomatik bağlanır) |
| **Güvenlik** | nftables güvenlik duvarı, SELinux uyumlu, ClamAV |
| **Performans** | MariaDB + nginx + OPcache otomatik tuning (`girginospanel-optimize`) |

## Panel özellikleri

- Domain / subdomain yönetimi, DNS düzenleme, toplu işlemler
- Tek-tık **WordPress** kurulumu + WP-CLI
- Per-tenant **Redis object cache** (tek tıkla aç/kapa, WP'ye otomatik bağlama)
- **Güvenlik duvarı** arayüzü (IP ban / whitelist / port kapatma + hazır şablonlar)
- Backup yöneticisi, izleme/loglar, istatistikler
- Hizmet planları ve kaynak limitleri (domain oluştururken varsayılan **Başlangıç**)

## Sistem gereksinimleri

- **AlmaLinux 10** (RHEL 10 / Rocky 10 de çalışır)
- En az **2 GB RAM**, 2 vCPU (5 PHP sürümü + MariaDB + Valkey için)
- Root erişimi + internet bağlantısı

## Kurulum sonrası yardımcı araçlar

Kurulumla birlikte `/usr/local/bin`'e şu araçlar gelir:

```bash
girginospanel-update        # paneli GitHub'dan güvenli güncelle (aşağıya bak)
girginospanel-optimize      # MariaDB/nginx/PHP'yi sunucu kaynaklarına göre yeniden ayarla
girginospanel-redis-setup   # Valkey (Redis) altyapısını kur/onar
girginospanel-wp-redis <sk> # bir domainin WordPress'ine Redis cache bağla/çöz
girginospanel-repair        # izin / SELinux / sahiplik onarımı (idempotent)
```

## Güncelleme (SSH / CLI)

Kurulu bir panelde, SSH ile root olarak tek komut:

```bash
girginospanel-update            # son sürümü GitHub'dan çek → binary+frontend+migration değiştir → yeniden başlat
girginospanel-update --dry-run  # önce ne yapacağını göster (dokunmadan)
girginospanel-update --force    # binary aynı olsa bile yeniden uygula
girginospanel-update --branch X # farklı dal
```

- **Güvenli & veri-korumalı:** `/etc/girginospanel/env` (JWT/DB/Redis secret), MariaDB `panel` veritabanı ve `/home/c_*` müşteri siteleri **asla silinmez**. `install.sh`'in aksine yeni secret üretmez.
- Yeni migration'lar servis yeniden başlarken **otomatik + idempotent** uygulanır.
- Binary değişmemişse (sha eşleşir) hiçbir şey yapmaz.
- Yeni sürüm sağlıklı başlamazsa **otomatik olarak eski binary'ye geri döner** (rollback).

> Kendi fork'unu deploy ediyorsan: kaynağı derle (`go build` + `npm run build`), `assets/girginospanel-server` + `assets/frontend-dist.tar.gz`'i güncelle, repona push et — sunucularda `girginospanel-update` yeni sürümü çeker.

## Notlar

- Kurulum **idempotent** değildir; her çalıştırma yeni secret (JWT/DB parola) üretir. Yeniden çalıştırma yerine `girginospanel-repair` / `girginospanel-optimize` kullanın.
- Panel HTTP/2 + self-signed SSL ile :8443'te yayınlanır; gerçek alan adı için Let's Encrypt panel üzerinden eklenebilir.

---

## Kaynaktan derleme ve geliştirme

Bu proje **tamamen açık kaynaktır** (MIT). İstersen hazır binary'yi kurmak yerine kaynağı kendin derleyip geliştirebilirsin — katkılar açıktır.

### Gereksinimler

- **Go 1.23+** (backend)
- **Node.js 20+** ve **npm** (frontend)
- Çalıştırma için: MariaDB/MySQL erişimi (backend başlarken migration + admin seed uygular)

### Backend (Go)

```bash
# tek statik binary derle
go build -o girginospanel-server ./cmd/server

# çalıştır (ortam değişkenleriyle)
PANEL_JWT_SECRET="$(openssl rand -hex 32)" \
PANEL_DB_DSN="root@unix(/var/lib/mysql/mysql.sock)/panel" \
./girginospanel-server
```

Backend API `/api/v1` altında; sağlık kontrolü `/healthz`. Admin girişi işletim sistemi root'u üzerinden PAM ile doğrulanır (üretimde); geliştirmede `scripts/seed_admin.go` ile ayrı bir admin tohumlayabilirsin:

```bash
go run scripts/seed_admin.go -dsn '<DSN>' -kullanici admin -parola 'SECELECEGIN_PAROLA'
# ya da: PANEL_SEED_PAROLA env değişkeni
```

### Frontend (React + Vite + TypeScript)

```bash
cd frontend
npm install
npm run dev        # geliştirme sunucusu :5185 (proxy /api → VITE_API_PROXY)
npm run build      # üretim derlemesi → frontend/dist/
```

Dev sunucusunun backend'i nereye proxy'leyeceğini `VITE_API_PROXY` ile ayarla (varsayılan `http://localhost:8080`):

```bash
VITE_API_PROXY=http://localhost:8080 npm run dev
```

### Depo yapısı

```
cmd/server/       Go giriş noktası (main)
internal/         Backend paketleri (domains, wordpress, dns, redis, guvenlikduvari, github, backups, ...)
frontend/src/     React arayüzü (pages/, components/, lib/)
migrations/       SQL şema migration'ları (başlangıçta uygulanır)
scripts/          Ops yardımcıları (optimize, repair, redis-setup, seed_admin, ...)
assets/           Kurulum için hazır (prebuilt) release çıktıları — installer bunları kullanır
install.sh        Tek satır bootstrap (repoyu indirir → girginospanel-install.sh)
```

> `assets/` içindeki hazır binary + `frontend-dist.tar.gz`, `curl | bash` kurulumunun kaynağı derlemeden çalışması içindir. Kendi değişikliklerini yayınlarken bunları yukarıdaki `go build` / `npm run build` çıktısıyla güncelle.

## Katkı & lisans

- Katkılar (issue / PR) açıktır.
- Lisans: **MIT** — bkz. [LICENSE](LICENSE). Kullanabilir, değiştirebilir, dağıtabilir ve kendi ürününde kullanabilirsin.
