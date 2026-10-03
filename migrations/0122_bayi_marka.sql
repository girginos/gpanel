-- 0122: bayiye ozel marka (D2 whitelabel cekirdege tasindi).
-- Kok markasi dosyada kalir (/opt/girginospanel/marka/marka.json), DB dussa da
-- giris sayfasi calisir. Bayi markalari burada; gorseller
-- /opt/girginospanel/marka/bayi/<anahtar>/ altinda.
-- anahtar: [a-z2-7]{20}, tahmin edilemez kisa ad (giris oncesi ?m= ve gorsel
-- adresi). users.id hicbir kimliksiz yanitta gecmez.
-- izin (varsayilan acik) / alanadi_izni (varsayilan KAPALI) / kok_kapatti:
-- kok yonetir. etkin: bayi kendi markasini kaydetti mi.
-- Satir ici yorum YOK: koşucu ifadeyi noktali virgulden boler.
CREATE TABLE IF NOT EXISTS cp_marka (
  reseller_id BIGINT UNSIGNED NOT NULL,
  anahtar CHAR(20) NOT NULL,
  ayar_json LONGTEXT NOT NULL CHECK (json_valid(ayar_json)),
  surum BIGINT UNSIGNED NOT NULL DEFAULT 1,
  etkin TINYINT(1) NOT NULL DEFAULT 0,
  izin TINYINT(1) NOT NULL DEFAULT 1,
  alanadi_izni TINYINT(1) NOT NULL DEFAULT 0,
  kok_kapatti TINYINT(1) NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (reseller_id),
  UNIQUE KEY uq_marka_anahtar (anahtar),
  CONSTRAINT fk_marka_bayi FOREIGN KEY (reseller_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT IGNORE INTO cp_ayarlar (anahtar, deger) VALUES ('marka_bayi_yedek', 'kok');
