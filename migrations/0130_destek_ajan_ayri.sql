-- 0130: Telegram destek ajanı panel sürecinden AYRILDI (girginospanel-destek-ajan.service).
-- Panel ile ajan artık yalnız bu veritabanı üzerinden haberleşir:
--   * destek_baglantilari.hub_bildirildi_at: 'iptal' bağlantının merkeze (hub) bildirildiği an.
--     Panel UI iptali ve kimlik olayları (parola/2FA/hosting silme…) bağlantıyı DB'de iptal eder
--     ve bu sütunu NULL bırakır; ajan ≤3 sn içinde yoklayıp baglanti_iptal gönderir ve damgalar.
--   * destek_ayar.surum: her ayar yazımında artar → ajan değişikliği fark edip hesap durumlarını yeniden bildirir.
--   * destek_ajan_nabiz: ajanın canlılık satırı (tek satır, id=1) — panel durum sayfası okur.
-- Tamamen idempotent (göçler birden çok kez koşabilir).

ALTER TABLE destek_baglantilari ADD COLUMN IF NOT EXISTS hub_bildirildi_at TIMESTAMP NULL DEFAULT NULL;

-- Bu göçten ÖNCE iptal edilenler eski (gömülü) ajan tarafından ya zaten bildirildi ya da yeniden
-- bağlanınca son-7-gün senkronuyla bildirilir: hepsini "bildirildi" say (yoklama yığılmasın).
-- Yalnız sütun YENİ eklendiyse (hepsi NULL iken) çalışsın diye koşul: iptal_at'i olan ve NULL olanlar.
UPDATE destek_baglantilari SET hub_bildirildi_at = COALESCE(iptal_at, CURRENT_TIMESTAMP)
  WHERE durum = 'iptal' AND hub_bildirildi_at IS NULL AND iptal_at IS NOT NULL AND iptal_at < (CURRENT_TIMESTAMP - INTERVAL 1 MINUTE);

-- destek_ayar.surum: her PUT /destek/ayar yazımında artar (aynı değerler yazılsa da) → ajan ayarı yeniden bildirir.
ALTER TABLE destek_ayar ADD COLUMN IF NOT EXISTS surum BIGINT UNSIGNED NOT NULL DEFAULT 0;

ALTER TABLE destek_baglantilari ADD KEY IF NOT EXISTS ix_destek_bag_hub (durum, hub_bildirildi_at);

CREATE TABLE IF NOT EXISTS destek_ajan_nabiz (
  id             TINYINT UNSIGNED NOT NULL PRIMARY KEY,
  pid            INT UNSIGNED     NOT NULL DEFAULT 0,
  surum          VARCHAR(64)      NOT NULL DEFAULT '',
  basladi_at     TIMESTAMP        NULL DEFAULT NULL,
  nabiz_at       TIMESTAMP        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  durum          VARCHAR(24)      NOT NULL DEFAULT '',
  tasima         VARCHAR(16)      NOT NULL DEFAULT '',
  son_hata       VARCHAR(255)     NOT NULL DEFAULT '',
  son_baglanti   BIGINT           NOT NULL DEFAULT 0,
  sunucu_kimligi VARCHAR(64)      NOT NULL DEFAULT '',
  cihaz_kimligi  VARCHAR(64)      NOT NULL DEFAULT '',
  merkez_bagli   TINYINT(1)       NOT NULL DEFAULT 0,
  kopru_durum    VARCHAR(8)       NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
