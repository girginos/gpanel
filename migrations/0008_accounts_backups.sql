-- 0008 - Customers + domains.customer_id + backups
-- NOT: resellers/bayiler yapısı 0011'de kaldırıldı (bkz. sprint notu).
CREATE TABLE IF NOT EXISTS customers (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ad VARCHAR(128) NOT NULL,
  eposta VARCHAR(255) NOT NULL,
  plan_id BIGINT UNSIGNED NULL,
  durum ENUM('aktif','pasif') NOT NULL DEFAULT 'aktif',
  notlar VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  KEY ix_customer_plan (plan_id)
) ENGINE=InnoDB;

ALTER TABLE domains
  ADD COLUMN IF NOT EXISTS customer_id BIGINT UNSIGNED NULL,
  ADD KEY IF NOT EXISTS ix_domains_customer (customer_id);

CREATE TABLE IF NOT EXISTS backups (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  domain_id BIGINT UNSIGNED NOT NULL,
  tip VARCHAR(16) NOT NULL DEFAULT 'tam',
  dosya VARCHAR(255) NOT NULL,
  boyut_b BIGINT NOT NULL DEFAULT 0,
  notlar VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  KEY ix_backup_domain (domain_id),
  CONSTRAINT fk_backup_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB;
