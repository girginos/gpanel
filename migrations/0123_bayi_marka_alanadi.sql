-- 0123: bayinin ozel panel alan adi (ornek panel.bayi.com).
-- Kullanici karari: varsayilan kapali, kok bayi basina en cok 1 acar
-- (UNIQUE reseller_id). Host cozumu yalniz durum aktif satirlari kullanir.
-- dogrulama: TXT _gpanel-dogrulama.<alan> degeri.
-- son_hata_genel bayiye gosterilir, son_hata_ayrinti yalniz koke.
CREATE TABLE IF NOT EXISTS cp_marka_alanadi (
  alan_adi VARCHAR(253) NOT NULL,
  reseller_id BIGINT UNSIGNED NOT NULL,
  dogrulama CHAR(32) NOT NULL,
  durum ENUM('dns_bekliyor','ssl_bekliyor','aktif','hata','kapali') NOT NULL DEFAULT 'dns_bekliyor',
  ssl_bitis DATETIME NULL,
  son_hata_genel VARCHAR(255) NOT NULL DEFAULT '',
  son_hata_ayrinti TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (alan_adi),
  UNIQUE KEY uq_marka_alanadi_bayi (reseller_id),
  CONSTRAINT fk_marka_alanadi_bayi FOREIGN KEY (reseller_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
