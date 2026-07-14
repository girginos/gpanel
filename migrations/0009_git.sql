-- 0009 - Git entegrasyonu (per-domain repo + deploy key + webhook)
CREATE TABLE IF NOT EXISTS git_repos (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  domain_id BIGINT UNSIGNED NOT NULL,
  repo_url VARCHAR(512) NOT NULL,
  branch VARCHAR(64) NOT NULL DEFAULT 'main',
  target_dir VARCHAR(255) NOT NULL DEFAULT 'public_html',
  deploy_key_pub TEXT NOT NULL,
  webhook_secret VARCHAR(64) NOT NULL,
  son_sync TIMESTAMP NULL,
  son_commit VARCHAR(64) NOT NULL DEFAULT '',
  son_durum VARCHAR(32) NOT NULL DEFAULT 'beklemede',
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  KEY ix_git_domain (domain_id),
  UNIQUE KEY uk_git_secret (webhook_secret),
  CONSTRAINT fk_git_domain FOREIGN KEY (domain_id) REFERENCES domains(id) ON DELETE CASCADE
) ENGINE=InnoDB;
