-- 0023_schema_drift_fix.sql
-- Prod'da geliştirme sırasında migration'sız eklenmiş tablo/kolonları fresh install'lara taşır.
-- Idempotent: CREATE TABLE IF NOT EXISTS + ADD COLUMN IF NOT EXISTS (MariaDB).
-- Eksik olmasa no-op (prod'da zaten var).

-- ── domains: web backend + backup ayarları ──
ALTER TABLE domains ADD COLUMN IF NOT EXISTS web_backend      varchar(32)  NOT NULL DEFAULT 'php-fpm';
ALTER TABLE domains ADD COLUMN IF NOT EXISTS backup_freq      varchar(16)  NOT NULL DEFAULT 'none';
ALTER TABLE domains ADD COLUMN IF NOT EXISTS backup_hour      tinyint(4)   NOT NULL DEFAULT 3;
ALTER TABLE domains ADD COLUMN IF NOT EXISTS backup_retention tinyint(4)   NOT NULL DEFAULT 7;
ALTER TABLE domains ADD COLUMN IF NOT EXISTS last_backup_at   timestamp    NULL DEFAULT NULL;

-- ── backup_destinations (uzak yedek hedefleri: sftp/ftp) ──
CREATE TABLE IF NOT EXISTS backup_destinations (
  id          bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  domain_id   bigint(20) unsigned NOT NULL,
  tip         varchar(8)   NOT NULL DEFAULT 'sftp',
  host        varchar(253) NOT NULL,
  port        int(11)      NOT NULL DEFAULT 22,
  kullanici   varchar(128) NOT NULL,
  parola      varchar(255) NOT NULL,
  uzak_dizin  varchar(255) NOT NULL DEFAULT '/',
  aktif       tinyint(4)   NOT NULL DEFAULT 1,
  son_yukleme timestamp    NULL DEFAULT NULL,
  son_durum   varchar(32)  NOT NULL DEFAULT '',
  son_hata    varchar(512) NOT NULL DEFAULT '',
  created_at  timestamp    NULL DEFAULT current_timestamp(),
  updated_at  timestamp    NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  PRIMARY KEY (id),
  UNIQUE KEY uk_domain (domain_id),
  CONSTRAINT fk_bdest_domain FOREIGN KEY (domain_id) REFERENCES domains (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ── github_connections (Git deploy: PAT + repo/branch + webhook) ──
CREATE TABLE IF NOT EXISTS github_connections (
  id            bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  domain_id     bigint(20) unsigned NOT NULL,
  pat           varchar(255) NOT NULL,
  login         varchar(64)  NOT NULL,
  ad_soyad      varchar(128) NOT NULL DEFAULT '',
  avatar_url    varchar(255) NOT NULL DEFAULT '',
  secili_repo   varchar(255) NOT NULL DEFAULT '',
  secili_branch varchar(64)  NOT NULL DEFAULT '',
  webhook_id    bigint(20)   NOT NULL DEFAULT 0,
  webhook_url   varchar(255) NOT NULL DEFAULT '',
  created_at    timestamp    NULL DEFAULT current_timestamp(),
  updated_at    timestamp    NULL DEFAULT current_timestamp() ON UPDATE current_timestamp(),
  PRIMARY KEY (id),
  UNIQUE KEY uk_domain (domain_id),
  CONSTRAINT fk_gh_domain FOREIGN KEY (domain_id) REFERENCES domains (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
