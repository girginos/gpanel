-- Antivirüs platform ayarları — TEK SATIR (id=1).
--
-- 🔴 NEDEN MODÜLER: tarama motorunun her katmanı ve her eylemi ayrı ayrı
-- kapatılabilir olmalı. Bir müşteri sunucusunda gerçek zamanlı izleme fazla
-- yük getiriyorsa yalnız zamanlı tarama açık kalabilir; yanlış pozitif
-- şüphesi varsa otomatik karantina kapatılıp yalnız raporlama bırakılabilir.
-- Tek bir "antivirüs açık/kapalı" anahtarı bu kararları operatörden alır.
--
-- 🔴 NEDEN KAYNAK LİMİTİ ŞART: tarama G/Ç ve CPU yoğundur. Sınırsız çalışan
-- bir tarayıcı, korumaya çalıştığı siteleri yavaşlatarak kendi başına bir
-- kesinti sebebi olur. Limitler cgroup (systemd slice) ile UYGULANIR —
-- uygulama içi "yavaş git" mantığı çekirdek tarafından zorlanmaz.
CREATE TABLE IF NOT EXISTS av_ayarlar (
  id                TINYINT UNSIGNED NOT NULL PRIMARY KEY DEFAULT 1,

  -- ── Modüller (her biri bağımsız kapatılabilir) ──
  gercek_zamanli    TINYINT(1)   NOT NULL DEFAULT 0,  -- fanotify izleyici
  zamanli_tarama    TINYINT(1)   NOT NULL DEFAULT 1,  -- periyodik tam tarama
  wp_butunluk       TINYINT(1)   NOT NULL DEFAULT 1,  -- WP çekirdek sağlama denetimi
  kural_motoru      TINYINT(1)   NOT NULL DEFAULT 1,  -- desen/puan motoru
  konum_sezgileri   TINYINT(1)   NOT NULL DEFAULT 1,  -- uploads/*.php vb.

  -- ── Eylem ──
  -- 🔴 Varsayılan 0: otomatik karantina, operatör motorun yanlış pozitif
  -- davranışını KENDİ sunucusunda görmeden açılmamalı. Önce raporla, güven
  -- oluşunca aç.
  oto_karantina     TINYINT(1)   NOT NULL DEFAULT 0,
  esik_kritik       INT          NOT NULL DEFAULT 100, -- puan eşiği (motorla aynı)

  -- ── Kapsam ──
  -- 'host'   = yalnız /home (kiracı dizinleri) — VARSAYILAN
  -- 'sunucu' = tüm dosya sistemi (hariç listesiyle)
  kapsam            VARCHAR(16)  NOT NULL DEFAULT 'host',
  haric_yollar      TEXT         NOT NULL,

  -- ── Kaynak limitleri (systemd slice ile UYGULANIR) ──
  -- 0 = otomatik (sunucu kapasitesine göre hesapla)
  cpu_yuzde         INT          NOT NULL DEFAULT 0,  -- CPUQuota %
  ram_mb            INT          NOT NULL DEFAULT 0,  -- MemoryMax MB
  io_agirlik        INT          NOT NULL DEFAULT 50, -- IOWeight (1-10000, varsayılan 100)
  is_parcacigi      INT          NOT NULL DEFAULT 0,  -- eşzamanlı tarayıcı (0=oto)
  dosya_hiz_sn      INT          NOT NULL DEFAULT 0,  -- dosya/sn tavanı (0=sınırsız)

  -- ── Zamanlama ──
  zamanli_saat      VARCHAR(8)   NOT NULL DEFAULT '04:00',

  guncelleme        TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 🔴 Tek satır GARANTİ: uygulama "kayıt yoksa varsayılan" yoluna düşmesin
-- diye satır burada oluşturulur. Aksi halde panel bir şey gösterir, ajan
-- başka bir şey okur (bu oturumda PHP varsayılanlarında birebir yaşandı).
INSERT INTO av_ayarlar (id, haric_yollar) VALUES (1,
  '/proc\n/sys\n/dev\n/run\n/var/lib/mysql\n/var/lib/containers\n/var/cache\n/var/backups\n/opt/girginospanel/frontend-dist\n.git/\nnode_modules/\n/wp-content/cache/\n/wp-content/uploads/cache/'
) ON DUPLICATE KEY UPDATE id = id;
