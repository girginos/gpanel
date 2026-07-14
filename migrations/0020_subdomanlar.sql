-- Subdomainler (alt alan adları) — parent domain'in kullanıcısı altında ayrı docroot + vhost
CREATE TABLE IF NOT EXISTS subdomanlar (
  id INT AUTO_INCREMENT PRIMARY KEY,
  domain_id BIGINT UNSIGNED NOT NULL,
  alt_ad VARCHAR(63) NOT NULL,
  tam_ad VARCHAR(253) NOT NULL,
  php_surum VARCHAR(8) NOT NULL DEFAULT '8.3',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uq_sub_tam (tam_ad),
  KEY ix_sub_dom (domain_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
