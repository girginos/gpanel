-- 0012 - nginx security headers per-domain toggle
CREATE TABLE IF NOT EXISTS nginx_settings (
  domain_id BIGINT UNSIGNED PRIMARY KEY,
  hdr_x_content_type TINYINT(1) NOT NULL DEFAULT 1,
  hdr_x_xss          TINYINT(1) NOT NULL DEFAULT 1,
  hdr_referrer       TINYINT(1) NOT NULL DEFAULT 1,
  hdr_permissions    TINYINT(1) NOT NULL DEFAULT 1,
  hdr_csp_upgrade    TINYINT(1) NOT NULL DEFAULT 1,
  hdr_hsts           TINYINT(1) NOT NULL DEFAULT 1,
  hsts_max_age       INT NOT NULL DEFAULT 31536000,
  hsts_subdomains    TINYINT(1) NOT NULL DEFAULT 1,
  hsts_preload       TINYINT(1) NOT NULL DEFAULT 0,
  ek_direktifler     TEXT NOT NULL,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  CONSTRAINT fk_nginxset_dom FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB;
