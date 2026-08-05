-- Lisanslı eklenti pazaryeri — yerel lisans deposu + panel ayar anahtarları.
--
-- cp_eklenti_lisans: bu sunucuda etkinleştirilmiş eklenti lisansları.
--   Lisans anahtarı DÜZ saklanır (heartbeat için gerekli) ama LOGA ASLA
--   tam yazılmaz; internal/lisans.Maskele() ile maskelenir.
--   Satırın silinmesi = lisansın kaldırılması (gate kapanır, VERİ SİLİNMEZ).
--
-- cp_ayarlar: panel geneli küçük anahtar/değer ayarları. Şimdilik tek kullanıcı:
--   'lisans_sunucu' (varsayılan https://lic-eu.girginos.io).
--   🔴 app.girginos.io DEĞİL: o vhost Cloudflare arkasındadır ve bot koruması
--   standart HTTP istemcilerini engeller; ayrıca panele bakan uçlar (katalog,
--   activate, heartbeat) orada YAYINLANMAZ. lic-eu CDN dışıdır ve yalnız
--   panel uçlarını sunar.

CREATE TABLE IF NOT EXISTS cp_eklenti_lisans (
  id              BIGINT AUTO_INCREMENT PRIMARY KEY,
  eklenti_ad      VARCHAR(64)  NOT NULL,               -- cp_eklentiler.ad ('mail')
  urun_slug       VARCHAR(64)  NOT NULL,               -- lisans sunucusu ürünü ('mail-server')
  lisans_anahtari VARCHAR(255) NOT NULL,
  durum           VARCHAR(32)  NOT NULL DEFAULT 'aktif', -- aktif|expired|suspended|invalid|...
  expires_at      DATETIME     NULL,
  son_dogrulama   DATETIME     NULL,                   -- son BAŞARILI heartbeat
  son_hata        VARCHAR(255) NOT NULL DEFAULT '',    -- gate neden kapandı
  deneme          TINYINT(1)   NOT NULL DEFAULT 0,     -- ücretsiz deneme lisansı mı
  created_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_eklenti_lisans_ad (eklenti_ad)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS cp_ayarlar (
  anahtar    VARCHAR(64)  NOT NULL PRIMARY KEY,
  deger      TEXT         NOT NULL,
  updated_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO cp_ayarlar (anahtar, deger) VALUES ('lisans_sunucu', 'https://lic-eu.girginos.io');
