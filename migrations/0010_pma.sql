-- 0010 - phpMyAdmin tek-kullanimlik SSO token'lari
CREATE TABLE IF NOT EXISTS pma_tokens (
  token VARCHAR(64) NOT NULL PRIMARY KEY,
  domain_id BIGINT UNSIGNED NOT NULL,
  db_kullanici VARCHAR(80) NOT NULL,
  db_parola VARCHAR(255) NOT NULL,
  db_adi VARCHAR(80) NOT NULL,
  olusturulma TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  son_kullanma TIMESTAMP NOT NULL,
  kullanildi TINYINT(1) NOT NULL DEFAULT 0,
  KEY ix_pma_dom (domain_id),
  CONSTRAINT fk_pma_dom FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB;
