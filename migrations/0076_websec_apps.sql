-- websec UYGULAMA ENVANTERI
--
-- 🔴 NEDEN VAR: Monitör bugüne kadar YALNIZCA zafiyet bulgularını gösteriyordu.
-- Bir domain'de bilinen açık yoksa hiçbir yerde görünmüyordu. Sonuç: ekranda
--   "her şey güvenli"  ile  "tarayıcı hiç çalışmadı"  AYNI görünüyor.
-- Müşteri "panelde 2 domain var ama monitörde listelenmemiş" dedi; ölçtük:
-- tarayıcı çalışıyordu (scanned_apps=2), yalnızca envanter görünmüyordu.
-- Bu tablo "neyin tarandığını" kaydeder, böylece boş bulgu listesi KANITLI
-- iyi haber olur.
CREATE TABLE IF NOT EXISTS cp_websec_apps (
  id           BIGINT       AUTO_INCREMENT PRIMARY KEY,
  -- 🔴 domains.id BIGINT UNSIGNED — FK icin tip BIREBIR ayni olmali (errno 150).
  -- 0074'te bu tuzaga dusulmustu; bastan UNSIGNED tanimliyoruz.
  domain_id    BIGINT UNSIGNED NOT NULL,
  app_type     VARCHAR(24)  NOT NULL,            -- wordpress | nodejs | php-composer
  install_path VARCHAR(500) NOT NULL,
  app_version  VARCHAR(64)  NOT NULL DEFAULT '', -- WP core surumu / node / php
  paket_sayisi INT          NOT NULL DEFAULT 0,  -- eklenti+tema / npm / composer
  bulgu_sayisi INT          NOT NULL DEFAULT 0,  -- bu uygulamada bulunan zafiyet
  son_tarama   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
                            ON UPDATE CURRENT_TIMESTAMP,
  -- Ayni kurulum tekrar tarandiginda YENI satir degil GUNCELLEME olsun.
  UNIQUE KEY uq_websec_app (domain_id, app_type, install_path),
  KEY idx_websec_app_tur (app_type),
  CONSTRAINT fk_websec_apps_domain
    FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
