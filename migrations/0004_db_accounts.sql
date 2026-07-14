-- 0004 - db_accounts tablosu (her domain icin MySQL veritabani + kullanici metadata)
CREATE TABLE IF NOT EXISTS db_accounts (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  domain_id BIGINT UNSIGNED NOT NULL,
  db_name VARCHAR(64) NOT NULL UNIQUE,
  db_user VARCHAR(64) NOT NULL UNIQUE,
  db_pass_plain VARCHAR(255) NOT NULL,
  db_host VARCHAR(64) NOT NULL DEFAULT 'localhost',
  notlar VARCHAR(255) NOT NULL DEFAULT '',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  KEY ix_db_domain (domain_id),
  CONSTRAINT fk_db_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB;
