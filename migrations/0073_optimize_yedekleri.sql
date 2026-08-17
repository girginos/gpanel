CREATE TABLE IF NOT EXISTS cp_optimize_yedekler (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  servis VARCHAR(32) NOT NULL,
  yedek_yol VARCHAR(500) NOT NULL,
  hedef_yol VARCHAR(500) NOT NULL,
  aciklama VARCHAR(255) NOT NULL DEFAULT "",
  aktor_uid BIGINT UNSIGNED NULL,
  geri_alindi TINYINT(1) NOT NULL DEFAULT 0,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_servis (servis, created_at),
  INDEX idx_aktor (aktor_uid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
