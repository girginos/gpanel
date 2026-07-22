-- 0042_laravel_apps.sql — Laravel Toolkit per-domain uygulama meta (Plesk paritesi)
CREATE TABLE IF NOT EXISTS cp_laravel_apps (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  domain_id        BIGINT UNSIGNED NOT NULL,
  app_root         VARCHAR(255) NOT NULL DEFAULT 'public_html',
  deploy_mode      VARCHAR(16)  NOT NULL DEFAULT 'uzak',
  php_surum        VARCHAR(8)   NOT NULL DEFAULT '',
  node_surum       VARCHAR(16)  NOT NULL DEFAULT '',
  schedule_enabled TINYINT(1)   NOT NULL DEFAULT 0,
  queue_enabled    TINYINT(1)   NOT NULL DEFAULT 0,
  queue_timeout    INT          NOT NULL DEFAULT 60,
  queue_max_jobs   INT          NOT NULL DEFAULT 1000,
  queue_connection VARCHAR(32)  NOT NULL DEFAULT 'database',
  maintenance      TINYINT(1)   NOT NULL DEFAULT 0,
  last_commit      VARCHAR(64)  NOT NULL DEFAULT '',
  son_deploy_at    TIMESTAMP    NULL DEFAULT NULL,
  son_deploy_durum VARCHAR(32)  NOT NULL DEFAULT '',
  created_at       TIMESTAMP    NULL DEFAULT current_timestamp(),
  updated_at       TIMESTAMP    NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  PRIMARY KEY (id),
  UNIQUE KEY uk_laravel_domain (domain_id),
  CONSTRAINT fk_laravel_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
