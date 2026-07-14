-- Şifre korumalı dizinler (.htpasswd / nginx auth_basic)
CREATE TABLE IF NOT EXISTS korumali_dizinler (
  id INT AUTO_INCREMENT PRIMARY KEY,
  domain_id INT NOT NULL,
  yol VARCHAR(255) NOT NULL,
  kullanici VARCHAR(64) NOT NULL,
  htpasswd_dosya VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_kd (domain_id, yol, kullanici),
  KEY ix_kd_domain (domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
