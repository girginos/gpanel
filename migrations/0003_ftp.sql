CREATE TABLE IF NOT EXISTS ftp_accounts (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  domain_id BIGINT UNSIGNED NOT NULL,
  username VARCHAR(64) NOT NULL UNIQUE,
  password_md5 VARCHAR(64) NOT NULL,
  home_dir VARCHAR(255) NOT NULL,
  uid_n INT NOT NULL,
  gid_n INT NOT NULL,
  status ENUM('active','suspended') DEFAULT 'active',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY ix_ftp_domain (domain_id),
  KEY ix_ftp_status (status),
  CONSTRAINT fk_ftp_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB;
